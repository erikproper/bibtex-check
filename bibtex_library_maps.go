/*
 *
 * Module:    bibtex_check
 * Component:
 * - bibtex_library
 *   - bibtex_library_maps
 *
 * The in-memory map/inverse-map layer for the BibTeX library: the alias, key, and
 * contributor lookup tables in TBibTeXLibraryMappings, and the functions that read
 * and update them while keeping a map and its inverse consistent with each other.
 *
 * This sits between bibtex_library_db.go (what's actually stored, and how) and
 * bibtex_library.go (the business rules that decide what a mapping should be):
 * a function belongs here when its job is managing the redundancy between a map
 * and its inverse — e.g. setNameAlias/deleteNameAlias keep NameAliasToName/
 * NameToAliases in sync with each other and, via bibtex_library_db.go's
 * upsertNameMapping/deleteNameMapping, with the DB. Pure business-logic reads that
 * only ever touch one side (e.g. MapEntryKey's KeyOldies.Get()) live here too,
 * since they're still "access on top of the maps", even though they don't have
 * an inverse to manage.
 *
 * Invariant: bibtex_library.go never mutates NameAliasToName/NameToAliases
 * directly — AddNameMapping's absorb-group logic and CheckNameMappingConsistency's
 * cycle-removal/chain-flattening phases are graph-decision code (what should map to
 * what) that call setNameAlias/deleteNameAlias for every actual change, same as
 * everything else. bibtex_library.go may still read the maps directly to make
 * those decisions — reads don't need to preserve an invariant the way writes do.
 *
 * TBibTeXLibraryGeoMappings — the state/country reference tables used by
 * bibtex_library_address.go — is a separate, simpler struct: those are flat,
 * one-directional lookup tables loaded once from config, with no inverse map and
 * no redundancy to manage, so they don't belong in TBibTeXLibraryMappings and
 * don't need functions here.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 30.07.2026
 *
 */

package main

import "strings"

// TBibTeXLibraryMappings holds the library's alias/key/contributor lookup tables —
// each either a DB-backed write-through cache (KeyOldies, HintToKey) or an
// in-memory map kept consistent with the DB by the functions in this file
// (NameAliasToName/NameToAliases via setNameAlias). Embedded anonymously in
// TBibTeXLibrary, the same way TBibTeXTeX and TInteraction are — field access
// (l.NameAliasToName, l.KeyOldies, …) is unchanged for every existing caller.
type TBibTeXLibraryMappings struct {
	KeyOldies *TKeyAliasTable               // alias→canonical key mappings; always flat, eagerly updated
	HintToKey *TCachedTable[string, string] // persistent hint→key mappings (DBLP-derived hints are transient)

	NameAliasToName               TStringMap               // Mapping from name aliases to the actual name.
	NameToAliases                 TStringSetMap            // The inverted version of NameAliasToName
	NameToContributorID           map[string]string        // unambiguous: exactly one contributor for this name form
	AmbiguousNameToContributorIDs map[string][]string      // globally ambiguous: 2+ contributors share this name form
	ContributorByID               map[string]*TContributor // contributor ID → contributor data
	ContributorIDOldies           map[string]string        // absorbed contributor ID → current canonical contributor ID
	ORCIDToContributorID          map[string]string        // reverse: ORCID → contributor ID (all ORCIDs, not just canonical)
	DblpKeyToContributorID        map[string]string        // reverse: DBLP homepages key → contributor ID
	NonDoubleContributorNames     map[[2]string]bool       // pairs explicitly recorded as different people
}

// setNameAlias records alias as a name variant of canonical: it validates the
// pair, updates the in-memory NameAliasToName/NameToAliases maps, and persists
// the mapping via upsertNameMapping — the one place where a name alias's memory
// cache and its DB-backed source of truth are kept in sync. When check is true,
// an alias already mapped to a different canonical is rejected (with a warning)
// instead of being silently overwritten. Returns false when the mapping was
// rejected: empty alias/canonical, alias == canonical, or an ambiguity hit.
func (l *TBibTeXLibrary) setNameAlias(alias, canonical string, check bool) bool {
	if alias == "" || canonical == "" || alias == canonical {
		return false
	}

	if oldCanonical, aliasIsAlreadyAliased := l.NameAliasToName[alias]; aliasIsAlreadyAliased && oldCanonical != canonical {
		if check {
			l.Warning(WarningAmbiguousAlias, alias, oldCanonical, canonical)
			return false
		}
		// Retargeting (check=false): alias was pointing at a different canonical —
		// e.g. an intermediate hop being flattened away, or a cycle edge being
		// redirected. Drop it from that canonical's inverse set first, or
		// NameToAliases accumulates a stale membership that never gets cleaned up.
		l.NameToAliases.DeleteValueFromStringSetMap(oldCanonical, alias)
	}

	l.NameAliasToName[alias] = canonical
	l.NameToAliases.AddValueToStringSetMap(canonical, alias)
	upsertNameMapping(alias, canonical)

	return true
}

// deleteNameAlias removes alias's forward mapping and its membership in the old
// canonical's inverse set, and persists the removal. Used by
// CheckNameMappingConsistency's cycle-removal phase — the counterpart to
// setNameAlias for the "this mapping should no longer exist" case. No-op if
// alias is not currently mapped.
func (l *TBibTeXLibrary) deleteNameAlias(alias string) {
	canonical, isMapped := l.NameAliasToName[alias]
	if !isMapped {
		return
	}
	l.NameToAliases.DeleteValueFromStringSetMap(canonical, alias)
	delete(l.NameAliasToName, alias)
	if id, ok := l.NameToContributorID[alias]; ok {
		if c, isC := l.ContributorByID[id]; isC && c.Name == alias {
			// alias is itself a canonical contributor's own name — the edge being
			// removed was a stale claim from a DIFFERENT contributor's records
			// (FindAliases-derived, or a stale contributor_names row), not alias's
			// own identity. deleteNameMapping would incorrectly target alias's own
			// record; clear only the stale cross-claim instead.
			dbExecSave("deleteNameAlias: remove stale alias claim",
				`DELETE FROM contributor_names WHERE name = ? AND id != ?`, alias, id)
			return
		}
	}
	deleteNameMapping(alias)
}

// MapEntryKey resolves key through KeyOldies to its current canonical form, or
// returns key unchanged when it is not a known alias.
func (l *TBibTeXLibrary) MapEntryKey(key string) string {
	if canonical := l.KeyOldies.Get(key); canonical != "" {
		return canonical
	}
	return key
}

// LookupDBLPKey resolves a DBLP key to the library key that absorbed it, if any.
func (l *TBibTeXLibrary) LookupDBLPKey(DBLPkey string) string {
	return l.KeyOldies.Get(KeyForDBLP(DBLPkey))
}

// maybeAddFoundAlias adds alias → canonical silently if alias is not yet mapped.
// Returns true when a new mapping was added, false when alias already existed
// (regardless of whether the existing mapping agrees or conflicts).
func (l *TBibTeXLibrary) maybeAddFoundAlias(canonical, alias string) bool {
	if alias == "" || alias == canonical {
		return false
	}
	if strings.ContainsAny(alias, "()") || hasStrayBrace(alias) {
		return false // parentheticals or brace-wrapped/stray-brace tokens are not name variants
	}
	if _, exists := l.NameAliasToName[alias]; exists {
		// Already mapped in-memory; ensure contributor ID is propagated too.
		if id, ok := l.NameToContributorID[canonical]; ok {
			l.NameToContributorID[alias] = id
		}
		return false
	}
	// Don't alias a name that is already a canonical contributor — that would
	// redirect its lookups to another contributor and create alias cycles.
	if id, ok := l.NameToContributorID[alias]; ok {
		if c, isC := l.ContributorByID[id]; isC && c.Name == alias {
			return false
		}
	}
	l.NameAliasToName[alias] = canonical
	l.NameToAliases.AddValueToStringSetMap(canonical, alias)
	// Derived aliases are not persisted — only in-memory.
	if id, ok := l.NameToContributorID[canonical]; ok {
		l.NameToContributorID[alias] = id
	}
	return true
}

// FindAliases derives all non-ambiguous aliases reachable from currentAlias
// (via name inversion and compressed-initials rules) and maps them to canonical.
// It stops silently when it hits an alias that is already mapped.
// Returns true if any new alias was added.
func (l *TBibTeXLibrary) FindAliases(canonical, currentAlias string) bool {
	added := false

	if inverted := invertedNameForm(currentAlias); inverted != "" {
		if l.maybeAddFoundAlias(canonical, inverted) {
			added = true
			l.FindAliases(canonical, inverted)
		}
	}

	if compressed := compressedInitialsForm(currentAlias); compressed != "" {
		if l.maybeAddFoundAlias(canonical, compressed) {
			added = true
			l.FindAliases(canonical, compressed)
		}
	}

	return added
}
