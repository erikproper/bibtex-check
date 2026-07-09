/*
 *
 * Module: bibtex_library
 *
 * This module is concerned with the storage of BibTeX libraties
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 24.04.2024
 *
 */

package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// bibParseCount is incremented atomically for each entry completed by the
// bib-file parser. Reset at parse start; read by the ticker loop to show
// live progress without requiring the goroutine to send explicit messages.
var bibParseCount int64

/*
 *
 * Definition of the Library type
 *
 */

// TContributor holds the persistent data for one person (contributor).
type TContributor struct {
	Name    string // preferred display name (from contributors.name)
	ORCID   string // ORCID identifier, may be empty
	DblpKey string // DBLP homepages key (e.g. "homepages/93/4573"), may be empty
	Garbled bool   // true when the contributor represents a raw garbled name value
}

type (
	// The type for BibTeXLibraries
	TBibTeXLibrary struct {
		FilesRoot    string   // Path to folder with library related files
		BaseName     string   // BaseName of the library related files
		FilesFolder  string   // Path to the PDF files folder, relative to FilesRoot
		Comments     []string // The Comments included in a BibTeX library. These are not always "just" Comments. BiBDesk uses this to store (as XML) information on e.g. static groups.
		GroupEntries TStringSetMap
		TitleIndex   TStringSetMap //
		//		BookTitleIndex                   TStringSetMap             //
		ISBNIndex                         TStringSetMap             //
		DOIIndex                          TStringSetMap             //
		NonDoubleEntries                   TStringSetMap             //
		NonDoubleContributorNames         map[[2]string]bool        //
		HintToKey   *TCachedTable[string, string] // persistent hint→key mappings (DBLP-derived hints are transient)
		newKeyHints TStringMap                   // counts persistent hints added in the current run (for harvest reporting)
		KeyOldies   *TKeyAliasTable              // alias→canonical key mappings; always flat, eagerly updated
		FieldMappings                     TStringStringStringMap    // field/value to field/value mapping
		KeyIsTemporary                    TStringSet                // Keys that are generated for temporary reasons
		NameAliasToName                   TStringMap                // Mapping from name aliases to the actual name.
		NameToAliases                     TStringSetMap             // The inverted version of NameAliasToName
		NameToContributorID               map[string]string         // unambiguous: exactly one contributor for this name form
		AmbiguousNameToContributorIDs     map[string][]string       // globally ambiguous: 2+ contributors share this name form
		ContributorByID                   map[string]*TContributor  // contributor ID → contributor data
		ContributorIDOldies               map[string]string         // absorbed contributor ID → current canonical contributor ID
		ORCIDToContributorID              map[string]string         // reverse: ORCID → contributor ID (all ORCIDs, not just canonical)
		DblpKeyToContributorID            map[string]string         // reverse: DBLP homepages key → contributor ID
		StateAliasToCanonical             TStringMap                // Mapping from state name aliases to canonical state names.
		StateToCountry                    TStringMap                // Mapping from canonical state names to canonical country names.
		CountryAliasToCanonical           TStringMap                // Mapping from country name aliases to canonical country names.
		BooktitleCountryAliasToCanonical  TStringMap                // English-only subset of country aliases for booktitle/title normalisation.
		illegalFields                     TStringSet                // Collect the unknown fields we encounter. We can warn about these when e.g. parsing has been finished.
		foundDoubles                      bool                      // If set, we found double entries. In this case, we may not want to e.g. write this file.
		EntryFieldSourceToTarget          TStringStringStringMap    // A key and field specific mapping from challenged value to winner values
		EntryFieldTargetToSource          TStringStringStringSetMap // DO WE NEED THE INVERSES??
		GenericFieldSourceToTarget        TStringStringMap          // A field specific mapping from challenged value to winner values
		GenericFieldTargetToSource        TStringStringSetMap       //
		NoDBUpdating                  bool                      // If set, the parser encountered errors; do not write bib file or update the database.
		DblpParent *TCachedTable[string, string] // child DBLP key → resolved parent DBLP key
		DblpWaived *TCachedTable[string, bool] // library keys exempt from WarningNoDblpKeyForChild
		Metadata              TEntryMetadata // per-entry metadata (see bibtex_library_metadata.go)
		EntryFlags map[string]TStringSet // canonical key → set of flag strings
		harvestNameAliases                bool
		harvestCapturePDFFields           bool         // when true: file/local-url pass through for harvest PDF copy
		harvestSourceDir                  string       // directory of the source bib file; used for relative PDF paths
		harvestSyncGroups                 TStringSet    // groups to sync to main DB during harvest (from config)
		subsetLocalGroups                 TStringSetMap // local groups loaded for current subset write pass
		jabrefGroupingBlock               string   // verbatim @Comment{jabref-meta: grouping:...} from source bib
		jabrefMetaBlocks                  []string // other @Comment{jabref-meta: ...} blocks carried verbatim
		bibdeskMetaBlocks                 []string // @Comment{BibDesk ...} blocks (not Static Groups) carried verbatim
		PDFFiles                          map[string]bool // keys with a <key>.pdf in FilesFolder; populated by LoadPDFFiles
		capturedDBLPEntry                 *TBibTeXEntry
		capturedHarvestEntries            *[]TBibTeXEntry // when non-nil, parsed entries collected here instead of DB
		URLsIgnore                        TStringSet
		IgnoredTitleIndexes               TStringSet // indexed forms of titles to skip in double-title detection
		ignoreIllegalFields               bool
		PreMergeCheck                     func(source, target string) // called before proposing a merge; may associate DBLP keys

		TBibTeXTeX
		TInteraction  // Error reporting channel
		TBibTeXStream // BibTeX parser
	}
)

// Initialise a library
func (l *TBibTeXLibrary) Initialise(reporting TInteraction, filesRoot, baseName string) {
	l.TInteraction = reporting
	l.Progress(ProgressInitialiseLibrary)

	l.TBibTeXStream = TBibTeXStream{}
	l.TBibTeXStream.Initialise(reporting, l)

	l.TBibTeXTeX = TBibTeXTeX{}

	l.TBibTeXTeX.library = l

	l.FilesRoot = filesRoot
	l.BaseName = baseName
	l.FilesFolder = baseName + ".files/"
	l.PDFFiles = map[string]bool{}

	l.Comments = []string{}
	l.FieldMappings = TStringStringStringMap{}
	l.GroupEntries = TStringSetMap{}
	l.TitleIndex = TStringSetMap{}
	//	l.BookTitleIndex = TStringSetMap{}
	l.ISBNIndex = TStringSetMap{}
	l.DOIIndex = TStringSetMap{}
	l.NonDoubleEntries = TStringSetMap{}
	l.NonDoubleContributorNames = map[[2]string]bool{}
	l.KeyOldies = newKeyOldiesTable()
	l.HintToKey = newKeyHintsTable()
	l.KeyIsTemporary = TStringSetNew()
	l.NameAliasToName = TStringMap{}
	l.NameToContributorID = map[string]string{}
	l.AmbiguousNameToContributorIDs = map[string][]string{}
	l.ContributorByID = map[string]*TContributor{}
	l.ContributorIDOldies = map[string]string{}
	l.ORCIDToContributorID = map[string]string{}
	l.DblpKeyToContributorID = map[string]string{}
	l.StateAliasToCanonical = TStringMap{}
	l.StateToCountry = TStringMap{}
	l.CountryAliasToCanonical = TStringMap{}
	l.BooktitleCountryAliasToCanonical = TStringMap{}

	l.EntryFieldTargetToSource = TStringStringStringSetMap{}
	l.GenericFieldTargetToSource = TStringStringSetMap{}

	l.foundDoubles = false
	l.EntryFieldSourceToTarget = TStringStringStringMap{}
	l.EntryFieldTargetToSource = TStringStringStringSetMap{}
	l.GenericFieldSourceToTarget = TStringStringMap{}
	l.GenericFieldTargetToSource = TStringStringSetMap{}

	l.NoDBUpdating = false
	l.newKeyHints = TStringMap{}
	l.Metadata = TEntryMetadata{}
	l.URLsIgnore = TStringSetNew()
	l.IgnoredTitleIndexes = TStringSetNew()
	l.DblpParent = newDblpParentTable()
	l.DblpWaived = newDblpWaivedTable()
	l.EntryFlags = map[string]TStringSet{}
	l.ignoreIllegalFields = false
}

/*
 *
 * Set/add functions
 * These are safe in the sense of not causing problems when dealing with partially empty nested maps.
 *
 */

// ReportEntryWarning prints a warning about a specific entry and records it in
// entry_warnings so it can be queried by the "warnings;" select operator and emitted
// as a % WARNING: comment in bib output.
func (l *TBibTeXLibrary) ReportEntryWarning(key, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	l.Warning("Entry %s: %s", key, msg)
	insertEntryWarning(key, msg)
}

// EntryInvolvedInWarning marks key as a secondary participant in a warning (e.g. the
// "other" entry in a conflict). An empty-text row is inserted so the entry appears in
// "warnings;" select results and in the repair bib, but no % WARNING: comment is emitted.
func (l *TBibTeXLibrary) EntryInvolvedInWarning(key string) {
	insertEntryWarning(key, "")
}

// SetEntryFieldValue writes a field value to the DB for the given entry.
func (l *TBibTeXLibrary) SetEntryFieldValue(entry, field, value string) {
	upsertBibEntryField(entry, field, value)
}

// SetEntryType writes the entry type to the DB, or to the captured DBLP entry when active.
func (l *TBibTeXLibrary) SetEntryType(entry, value string) {
	if l.capturedDBLPEntry != nil {
		l.capturedDBLPEntry.Key = entry
		l.capturedDBLPEntry.Fields[EntryTypeField] = value
		return
	}
	upsertBibEntryField(entry, EntryTypeField, value)
}

// applyJabRefGroupBlock parses the body of a jabref-meta: grouping: or
// jabref-meta: groupstree: comment and records group memberships.
//
// In harvest-capture mode: back-fills each referenced entry's in-memory groups
// field, exactly mirroring what BibDeskStaticGroupDefinition does for BibDesk.
// In normal mode: populates GroupEntries.
//
// Modern StaticGroup: lines carry no member keys (membership lives in per-entry
// groups fields); we register the group name so GroupEntries knows it exists.
// Legacy ExplicitGroup: lines carry member keys after the first two \;-fields.
func (l *TBibTeXLibrary) applyJabRefGroupBlock(content string) {
	var keyToIdx map[string]int
	if l.capturedHarvestEntries != nil {
		keyToIdx = make(map[string]int, len(*l.capturedHarvestEntries))
		for i, e := range *l.capturedHarvestEntries {
			keyToIdx[e.Key] = i
		}
	}

	addMember := func(groupName, key string) {
		if keyToIdx != nil {
			if idx, ok := keyToIdx[key]; ok {
				e := &(*l.capturedHarvestEntries)[idx]
				if e.Fields["groups"] == "" {
					e.Fields["groups"] = groupName
				} else {
					e.Fields["groups"] += ", " + groupName
				}
			}
		} else {
			l.GroupEntries.AddValueToStringSetMap(groupName, key)
		}
	}

	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)

		var isExplicit bool
		var rest string
		if i := strings.Index(line, " StaticGroup:"); i >= 0 {
			rest = line[i+len(" StaticGroup:"):]
		} else if i := strings.Index(line, " ExplicitGroup:"); i >= 0 {
			rest = line[i+len(" ExplicitGroup:"):]
			isExplicit = true
		} else {
			continue
		}

		parts := strings.Split(rest, `\;`)
		if len(parts) == 0 {
			continue
		}
		groupName := strings.TrimSpace(parts[0])
		if groupName == "" {
			continue
		}

		if isExplicit {
			// parts[0]=name, parts[1]=hierarchy, parts[2:]=member keys
			for _, key := range parts[2:] {
				key = strings.TrimSpace(strings.TrimSuffix(key, ";"))
				if key != "" {
					addMember(groupName, key)
				}
			}
		} else {
			// StaticGroup: members are in per-entry groups fields; just ensure the
			// group name is registered so GroupEntries knows it exists.
			if keyToIdx == nil {
				l.GroupEntries.AddValueToStringSetMap(groupName, "")
				l.GroupEntries.DeleteValueFromStringSetMap(groupName, "")
			}
		}
	}
}

// Add a comment to the current library.
func (l *TBibTeXLibrary) ProcessComment(comment string) bool {
	// When parsing a harvest/subset source bib: route each comment block to the
	// appropriate bucket for verbatim replay in the output. Never add source-bib
	// comments to the main library's Comments list.
	if l.capturedHarvestEntries != nil {
		trimmed := strings.TrimSpace(comment)
		block := "@" + CommentEntryType + "{" + comment + "}"
		switch {
		case strings.HasPrefix(trimmed, "jabref-meta: grouping:"):
			l.jabrefGroupingBlock = block
			l.applyJabRefGroupBlock(trimmed[len("jabref-meta: grouping:"):])
		case strings.HasPrefix(trimmed, "jabref-meta: groupstree:"):
			// Legacy format: parse for memberships; do NOT store verbatim.
			// The write side regenerates a modern grouping: block from GroupEntries.
			l.applyJabRefGroupBlock(trimmed[len("jabref-meta: groupstree:"):])
		case strings.HasPrefix(trimmed, "jabref-meta: databaseType:"):
			// always emitted by us; drop
		case strings.HasPrefix(trimmed, "jabref-meta: "):
			l.jabrefMetaBlocks = append(l.jabrefMetaBlocks, block)
		case strings.HasPrefix(trimmed, "BibDesk Static Groups{"):
			// handled via GroupEntries; drop
		case strings.HasPrefix(trimmed, "BibDesk"):
			l.bibdeskMetaBlocks = append(l.bibdeskMetaBlocks, block)
		}
		return true
	}
	l.Comments = append(l.Comments, comment)
	return true
}

// Initial registration of a target over a alias for a given entry and its field.
func (l *TBibTeXLibrary) AddEntryFieldAlias(entry, field, alias, target string, check bool) {
	if alias == "" {
		return
	}

	if alias == target {
		return
	}

	if l.GenericFieldSourceToTarget[field][alias] == target {
		return
	}

	if l.EntryFieldSourceToTarget[entry][field][alias] == target {
		return
	}

	// Check for ambiguity of aliases — warn and skip, but do not block other writes.
	if check {
		if currentTarget, aliasIsAlreadyAliased := l.EntryFieldSourceToTarget[entry][field][alias]; aliasIsAlreadyAliased {
			if currentTarget != target {
				l.Warning(WarningAmbiguousAlias, alias, currentTarget, target)
				return
			}
		}
	}

	// Set the actual mapping
	l.EntryFieldSourceToTarget.SetValueForStringTripleMap(entry, field, alias, target)

	// And inverse mapping
	l.EntryFieldTargetToSource.AddValueToStringTrippleSetMap(entry, field, target, alias)

	if field != PreferredAliasField && !entryFieldMappingsLoading {
		if l.MapFieldValue(field, alias) != l.MapEntryFieldValue(entry, field, target) {
			bibExec(`INSERT OR IGNORE INTO losing_field_values (entry_key, field, value) VALUES (?, ?, ?)`, //nolint:errcheck
				entry, field, alias)
		}
	}
}

// Update the registration of a target over an alias for a given entry and its field.
// As we have a new target, we also need to update any other existing challenges for this field.
func (l *TBibTeXLibrary) UpdateGenericFieldAlias(field, alias, target string) {
	l.AddGenericFieldAlias(field, alias, target, true)

	for otherAlias, otherTarget := range l.GenericFieldSourceToTarget[field] {
		if otherTarget == alias {
			l.AddGenericFieldAlias(field, otherAlias, target, false)
		}
	}
}

// Initial registration of a target over a alias for a given field.
func (l *TBibTeXLibrary) AddGenericFieldAlias(field, alias, target string, check bool) {
	if alias == "" {
		return
	}

	if field == "author" || field == "editor" {
		l.Warning(WarningGenericFieldMappingAuthorEditor, field, target, alias)
		return
	}

	if alias == target {
		return
	}

	if l.GenericFieldSourceToTarget[field][alias] == target {
		return
	}

	// Check for ambiguity of aliases
	if check {
		if currentTarget, aliasIsAlreadyAliased := l.GenericFieldSourceToTarget[field][alias]; aliasIsAlreadyAliased {
			if currentTarget != target {
				l.Warning("line: 206"+WarningAmbiguousAlias, alias, currentTarget, target)
				return
			}
		}
	}

	// Set the actual mapping
	l.GenericFieldSourceToTarget.SetValueForStringPairMap(field, alias, target)

	// And inverse mapping
	l.GenericFieldTargetToSource.AddValueToStringPairSetMap(field, target, alias)

	if field != PreferredAliasField && !fieldMappingsLoading {
		bibExec( //nolint:errcheck
			`INSERT INTO field_mappings (source_field, source_value, target_field, target_value) VALUES (?, ?, ?, ?)
			  ON CONFLICT(source_field, source_value, target_field) DO UPDATE SET target_value = excluded.target_value`,
			field, alias, field, l.MapFieldValue(field, target))
	}
}

// Update the registration of a target over a alias for a given entry and its field.
// As we have a new target, we also need to update any other existing challenges for this field.
func (l *TBibTeXLibrary) UpdateEntryFieldAlias(entry, field, alias, target string) {
	l.AddEntryFieldAlias(entry, field, alias, target, true)

	for otherAlias, otherTarget := range l.EntryFieldSourceToTarget[entry][field] {
		if otherTarget == alias {
			l.AddEntryFieldAlias(entry, field, otherAlias, target, false)
			// If AddEntryFieldAlias was blocked (e.g. a generic mapping already covers
			// otherAlias→target), the stale entry-specific mapping still points to the
			// old alias.  Remove it directly so the generic mapping takes over cleanly.
			// Guard: only remove when alias != target, otherwise the mapping is already
			// correct (otherAlias → alias = target) and must not be deleted.
			if l.EntryFieldSourceToTarget[entry][field][otherAlias] == alias && alias != target {
				delete(l.EntryFieldSourceToTarget[entry][field], otherAlias)
				if entryMap, ok := l.EntryFieldTargetToSource[entry]; ok {
					if fieldMap, ok := entryMap[field]; ok {
						if aliasSet, exists := fieldMap[alias]; exists {
							aliasSet.Delete(otherAlias)
							fieldMap[alias] = aliasSet
						}
					}
				}
				bibExec(`DELETE FROM losing_field_values WHERE entry_key = ? AND field = ? AND value = ?`, //nolint:errcheck
					entry, field, otherAlias)
			}
		}
	}
}

func (l *TBibTeXLibrary) ReassignEntryFieldMappings(source, target string) {
	for field, AliasAssignments := range l.EntryFieldSourceToTarget[source] {
		for alias, winner := range AliasAssignments {
			if dealiasedWinner := l.MapNormalisedEntryFieldValue(target, field, winner); dealiasedWinner != "" {
				l.AddEntryFieldAlias(target, field, alias, dealiasedWinner, false)
			}
		}
	}
}

// Add an implied alias ... order of key/alias ... seems not consistent.
//func (l *TBibTeXLibrary) AddImpliedKeyAlias(key, alias string) {
//	knownKey, keyExists := l.KeyToKey[alias]
//	if keyExists && knownKey != key {
//		l.Warning("Ambiguous alias assignment of %s to %s, while we already have %s", alias, key, knownKey)
//	} else {
//		l.AddKeyAlias(alias, key)
//	}
//}

// Add a new alias (not just for Keys!!)
// Is the check still needed???
// General cleanup needed.
func (l *TBibTeXLibrary) AddAlias(alias, original string, aliasMap *TStringMap, inverseMap *TStringSetMap, check bool) {
	// Neither alias, nor target should be empty
	if alias == "" || original == "" {
		return
	}

	// No need to alias oneself
	if alias == original {
		return
	}

	// Check for ambiguity of aliases
	if check {
		if currentOriginal, aliasIsAlreadyAliased := (*aliasMap)[alias]; aliasIsAlreadyAliased {
			if currentOriginal != original {
				l.Warning(WarningAmbiguousAlias, alias, currentOriginal, original)

				return
			}
		}
	}

	// Set the actual mapping
	aliasMap.SetValueForStringMap(alias, original)

	// Also create update the inverse mapping
	inverseMap.AddValueToStringSetMap(original, alias)

	if aliasMap == &l.NameAliasToName {
		upsertNameMapping(alias, original)
	}
}

// Help function
func (l *TBibTeXLibrary) MaybeAddReorderedName(alias, name string, aliasMap *TStringMap, inverseMap *TStringSetMap) {
	aliasSplit := strings.Split(alias, ",")

	if len(aliasSplit) == 3 {
		reorderedAlias := strings.TrimSpace(aliasSplit[2] + " " + aliasSplit[0] + strings.TrimRight(aliasSplit[1], " .") + ".")
		l.AddAlias(reorderedAlias, name, aliasMap, inverseMap, true)
	} else if len(aliasSplit) == 2 {
		reorderedAlias := strings.TrimSpace(aliasSplit[1] + " " + aliasSplit[0])
		l.AddAlias(reorderedAlias, name, aliasMap, inverseMap, true)
	}
}

// compressedInitialsForm returns the form of name with spaces between consecutive
// initials removed, e.g. "Doe, J. A. B." → "Doe, J.A.B.".
// Returns "" when no compression is possible.
var initialsSpacer = regexp.MustCompile(`\. ([A-Z]\.)`)

func compressedInitialsForm(name string) string {
	commaIdx := strings.Index(name, ", ")
	if commaIdx < 0 {
		return ""
	}
	surname := name[:commaIdx]
	rest := name[commaIdx+2:]
	compressed := initialsSpacer.ReplaceAllString(rest, ".$1")
	if compressed == rest {
		return ""
	}
	for {
		next := initialsSpacer.ReplaceAllString(compressed, ".$1")
		if next == compressed {
			break
		}
		compressed = next
	}
	return surname + ", " + compressed
}

// invertedNameForm returns the "Firstname Lastname" form of a "Lastname, Firstname"
// name, or "" if name has no comma (i.e. is already in non-inverted form).
func invertedNameForm(name string) string {
	parts := strings.Split(name, ",")
	switch len(parts) {
	case 2:
		return strings.TrimSpace(parts[1] + " " + parts[0])
	case 3:
		return strings.TrimSpace(parts[2] + " " + parts[0] + strings.TrimRight(parts[1], " .") + ".")
	}
	return ""
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

// AddNameMapping makes alias an alias of canonical, absorbing the alias's
// existing canonical group (if any) into canonical.
// If canonical is itself an alias of another name the call is normally redirected
// to that name's canonical to keep the graph acyclic. Exception: when canonical is
// currently an alias of alias (a direct inversion), we detach canonical first so
// the intended direction takes effect.
func (l *TBibTeXLibrary) AddNameMapping(canonical, alias string) {
	if existingCanonical, isMapped := l.NameAliasToName[canonical]; isMapped {
		if existingCanonical == alias {
			// Inversion: canonical is currently an alias of alias; detach it so
			// we can re-canonicalise canonical and absorb alias's group into it.
			delete(l.NameAliasToName, canonical)
			l.NameToAliases.DeleteValueFromStringSetMap(alias, canonical)
			deleteNameMapping(canonical)
			// fall through to normal processing
		} else {
			l.AddNameMapping(existingCanonical, alias)
			return
		}
	}

	// If alias is currently a canonical, absorb all its aliases into canonical.
	if aliasSet, isCanonical := l.NameToAliases[alias]; isCanonical {
		for a := range aliasSet.Elements() {
			l.NameAliasToName[a] = canonical
			l.NameToAliases.AddValueToStringSetMap(canonical, a)
			upsertNameMapping(a, canonical)
		}
		delete(l.NameToAliases, alias)
	}

	l.AddAlias(alias, canonical, &l.NameAliasToName, &l.NameToAliases, false)
	l.FindAliases(canonical, alias)
	l.FindAliases(canonical, canonical)
}

// CheckNameMappingConsistency silently flattens any (alias -> X -> canonical) chains
// left by automatic alias derivation. These arise when FindAliases maps a derived
// form to an intermediate alias instead of the ultimate canonical; the result is
// correct but the chain needs one extra hop at lookup time. Flattening removes that
// extra hop and ensures NameAliasToName is a single-level map.
func (l *TBibTeXLibrary) CheckNameMappingConsistency() {
	l.Progress(ProgressCheckingNameMappings)

	// Phase 1: detect and break cycles. Collect the edge to remove for each
	// cycle (the edge that closes the loop), then delete them all at once so
	// we don't modify the map while ranging over it.
	type removal struct{ from, to string }
	var removals []removal
	seenCycle := map[string]bool{}

	for start := range l.NameAliasToName {
		visited := map[string]bool{start: true}
		path := []string{start}
		cur := start
		for {
			next, ok := l.NameAliasToName[cur]
			if !ok {
				break
			}
			path = append(path, next)
			if visited[next] {
				edge := cur + "→" + next
				if !seenCycle[edge] {
					seenCycle[edge] = true
					l.Warning("Cycle in name mappings (auto-fixed): %s", strings.Join(path, " → "))
					removals = append(removals, removal{cur, next})
				}
				break
			}
			visited[next] = true
			cur = next
		}
	}

	for _, r := range removals {
		l.NameToAliases.DeleteValueFromStringSetMap(r.to, r.from)
		delete(l.NameAliasToName, r.from)
		// Only delete from the DB when r.from is a non-canonical alias. If r.from IS a
		// contributor's canonical name, deleteNameMapping would remove the canonical from
		// contributor_names and corrupt that contributor. The spurious reverse edge exists
		// only because loading saw r.from as a non-canonical name for another contributor;
		// that entry is cleaned up by subsequent merges and re-loads.
		if id, ok := l.NameToContributorID[r.from]; ok {
			if c, isC := l.ContributorByID[id]; isC && c.Name == r.from {
				// r.from is a canonical name — skip the DB delete.
			} else {
				deleteNameMapping(r.from)
			}
		} else {
			deleteNameMapping(r.from)
		}
	}

	// Phase 2: flatten multi-hop chains (A → B → C becomes A → C).
	type redirect struct{ alias, trueCanonical string }
	var redirects []redirect

	for alias, x := range l.NameAliasToName {
		if _, xIsAlso := l.NameAliasToName[x]; xIsAlso {
			ultimate := x
			for {
				next, ok := l.NameAliasToName[ultimate]
				if !ok {
					break
				}
				ultimate = next
			}
			redirects = append(redirects, redirect{alias, ultimate})
		}
	}

	for _, r := range redirects {
		oldIntermediate := l.NameAliasToName[r.alias]
		l.NameAliasToName[r.alias] = r.trueCanonical
		l.NameToAliases.DeleteValueFromStringSetMap(oldIntermediate, r.alias)
		l.NameToAliases.AddValueToStringSetMap(r.trueCanonical, r.alias)
		upsertNameMapping(r.alias, r.trueCanonical)
	}
}

func (l *TBibTeXLibrary) FileReferencety(key string) string {
	if l.PDFFiles[key] {
		return l.FilesFolder + key + ".pdf"
	}
	return ""
}

// /// SPLIT??
// /// fix source/target orders ...
// /// MergeEntryFromTo(source, target)
// ///   ResolveFieldValueOfAgainst(current, challenge)
func (l *TBibTeXLibrary) MergeEntries(sourceRAW, targetRAW string) string {
	if sourceRAW != "" && targetRAW != "" {
		// Fix names
		source := sourceRAW //l.MapEntryKey(sourceRAW)
		target := l.MapEntryKey(targetRAW)

		if source != target && l.EntryExists(source) {
			for _, field := range []string{DBLPField, "doi"} {
				sv := l.EntryFieldValueity(source, field)
				tv := l.EntryFieldValueity(target, field)
				if EvidencedUnequal(sv, tv) && !(field == "doi" && strings.EqualFold(sv, tv)) {
					l.Warning(WarningMergeConflictingField, source, target, field, sv, tv)
				}
			}
			if l.EvidenceForBeingDifferentEntries(source, target) {
				if !l.WarningYesNoQuestion(QuestionMergeAnyway, "Entries appear to be different publications") {
					return target
				}
			}
			l.Progress("Merging %s to %s", source, target)

			sourceEntry := loadEntryFromDb(source)
			targetEntry := loadEntryFromDb(target)

			targetType := l.MaybeResolveFieldValue(target, source, EntryTypeField, sourceEntry.EntryType(), targetEntry.EntryType())
			l.setEntryField(targetEntry, EntryTypeField, targetType)

			regularFields := TStringSet{}
			regularFields.Initialise().Unite(BibTeXAllowedEntryFields[targetType])
			for regularField := range regularFields.Elements() {
				merged := l.MaybeResolveFieldValue(target, source, regularField, sourceEntry.FieldValue(regularField), targetEntry.FieldValue(regularField))
				l.setEntryField(targetEntry, regularField, merged)
			}

			// Inherit lineage records from source for fields where target has none.
			// MaybeResolveFieldValue only writes lineage when challengeKey is a
			// known-source key (e.g. "DBLP:…"); library-key merges don't set it,
			// so DBLP provenance would be silently lost on the surviving entry.
			for regularField := range regularFields.Elements() {
				if l.getLineage(target, regularField).Source == "" {
					if srcLin := l.getLineage(source, regularField); srcLin.Source != "" {
						l.setLineage(target, regularField, srcLin.Source, srcLin.Edited)
					}
				}
			}

			if !l.KeyIsTemporary.Contains(source) {
				l.AddKeyAlias(source, target)
				l.AddNonDoubleEntries(source, target)
				// Rename source PDF to target name so the file stays associated.
				l.mergePDFFile(source, target)
			}
			l.ReassignEntryFieldMappings(source, target)
			l.transferMetadata(source, target)

			deleteBibEntry(source)

			l.CheckIfFieldsAreAllowed(targetEntry, func(key, field, value string) {
				l.deleteEntryField(targetEntry, field)
			})

			l.TitleIndex.AddValueToStringSetMap(targetEntry.FieldValue(TitleField), target)

			l.CheckEntry(l.buildEntry(target))
		}

		return target
	}

	return ""
}

// MergeInMemoryDBLPEntry merges an in-memory DBLP entry (never written to DB) into the
// target entry identified by targetRAW. Unlike MergeEntries, the source is not deleted
// from the DB and no key aliases or non-double records are created.
// Opens the target entry for the merge so that only fields that actually changed are
// written to the DB on close. Returns true if any field changed.
// The caller is responsible for wrapping this in a transaction and calling CheckEntry
// when the return value is true.
func (l *TBibTeXLibrary) MergeInMemoryDBLPEntry(sourceEntry *TBibTeXEntry, targetRAW string) bool {
	target := l.MapEntryKey(targetRAW)
	if target == "" {
		target = targetRAW
	}

	targetEntry := loadEntryFromDb(target)
	openEntry(targetEntry)

	targetType := l.MaybeResolveFieldValue(target, sourceEntry.Key, EntryTypeField, sourceEntry.EntryType(), targetEntry.EntryType())
	l.setEntryField(targetEntry, EntryTypeField, targetType)

	regularFields := TStringSet{}
	regularFields.Initialise().Unite(BibTeXAllowedEntryFields[targetType])
	for regularField := range regularFields.Elements() {
		merged := l.MaybeResolveFieldValue(target, sourceEntry.Key, regularField, sourceEntry.FieldValue(regularField), targetEntry.FieldValue(regularField))
		l.setEntryField(targetEntry, regularField, merged)
	}

	l.CheckIfFieldsAreAllowed(targetEntry, func(key, field, value string) {
		l.deleteEntryField(targetEntry, field)
	})

	// When the target has a crossref, push MustInherit fields (e.g. booktitle,
	// publisher, year) up to the parent and strip them from the child, mirroring
	// what the normal check flow does via CheckNeedToSplitBookishEntry.
	crossrefChanged := false
	if crossref := targetEntry.FieldValue("crossref"); crossref != "" {
		crossrefEntry := loadEntryFromDb(crossref)
		openEntry(crossrefEntry)
		for field := range BibTeXInheritableFields.Elements() {
			l.CheckCrossrefInheritableField(crossrefEntry, targetEntry, field)
		}
		crossrefChanged = closeEntry(crossrefEntry)
	}

	l.TitleIndex.AddValueToStringSetMap(targetEntry.FieldValue(TitleField), target)

	return closeEntry(targetEntry) || crossrefChanged
}

func EvidencedUnequal(a, b string) bool {
	return a != "" && b != "" && a != b
}

func (l *TBibTeXLibrary) EvidencedUnequalEntryFields(source, target, field string) bool {
	return EvidencedUnequal(l.EntryFieldValueity(source, field), l.EntryFieldValueity(target, field))
}

func (l *TBibTeXLibrary) EvidenceForBeingDifferentEntries(source, target string) bool {
	if l.EvidencedUnequalEntryFields(source, target, DBLPField) {
		return true
	}
	sv, tv := l.EntryFieldValueity(source, "doi"), l.EntryFieldValueity(target, "doi")
	return sv != "" && tv != "" && !strings.EqualFold(sv, tv)
	// || l.EvidencedUnequalEntryFields(source, target, "crossref")
}

// entryDisplayLines renders one entry for human display with aligned field names.
// All stored fields are shown (sorted), field names padded to 16 chars.
// local-url is suppressed: the DB stores a relative path that cannot be compared
// fairly to the absolute path shown in the bib entry.
func (l *TBibTeXLibrary) entryDisplayLines(key string) string {
	entry := loadEntryFromDb(key)
	if !entry.Exists() {
		return ""
	}
	sorted := make([]string, 0, len(entry.Fields))
	for f := range entry.Fields {
		if f != EntryTypeField && f != LocalURLField {
			sorted = append(sorted, f)
		}
	}
	sort.Strings(sorted)
	result := "  @" + entry.EntryType() + "{" + key + ",\n"
	for _, field := range sorted {
		if value := entry.Fields[field]; value != "" {
			mapped := l.MapEntryFieldValue(key, field, value)
			result += fmt.Sprintf("    %-*s = {%s},\n", BibTeXFieldColumnWidth, field, mapped)
		}
	}
	result += "  }\n"
	return result
}

// entryDisplayString returns the entry's BibTeX string with aligned field names,
// appending the parent entry when a crossref is present.
func (l *TBibTeXLibrary) entryDisplayString(key string) string {
	s := l.entryDisplayLines(key)
	if crossref := l.EntryFieldValueity(key, "crossref"); crossref != "" {
		if parentStr := l.entryDisplayLines(crossref); parentStr != "" {
			s += "  Parent entry:\n" + parentStr
		}
	}
	return s
}

func (l *TBibTeXLibrary) MaybeMergeEntries(sourceRAW, targetRAW string) {
	// Fix names
	source := l.MapEntryKey(sourceRAW)
	target := l.MapEntryKey(targetRAW)

	if l.EntryExists(source) && l.EntryExists(target) {
		if source != target && !l.NonDoubleEntries[source].Set().Contains(target) {
			if l.PreMergeCheck != nil {
				l.PreMergeCheck(source, target)
				source = l.MapEntryKey(source)
				target = l.MapEntryKey(target)
			}
		}
		if source != target && !l.NonDoubleEntries[source].Set().Contains(target) && !l.EvidenceForBeingDifferentEntries(source, target) {
			l.Warning("Found potential double entries")

			sourceEntry := l.entryDisplayString(source)
			targetEntry := l.entryDisplayString(target)

			if sourceEntry == "" {
				l.Warning("Empty source entry: %s", source)
			}

			if targetEntry == "" {
				l.Warning("Empty target entry: %s", target)
			}

			if l.WarningYesNoQuestion("Merge these entries", "First entry:\n%s\nSecond entry:\n%s", sourceEntry, targetEntry) {
				l.MergeEntries(source, target)
			} else {
				l.AddNonDoubleEntries(source, target)
			}
		}
	}
}

func (l *TBibTeXLibrary) MaybeMergeEntrySet(keys TStringSet) {
	if keys.Size() > 1 {
		sortedkeys := keys.ElementsSorted()
		for _, a := range sortedkeys {
			aMap := l.MapEntryKey(a)
			if a == aMap {
				for _, b := range sortedkeys {
					bMap := l.MapEntryKey(b)
					if b == bMap {
						l.MaybeMergeEntries(aMap, bMap)
					}
				}
			}
		}
	}
}

// AddKeyAlias records a persistent alias→canonical key mapping in key_oldies.
// Always persists: an alias being registered here (explicit -add_key_mapping,
// a demoted preferred alias, a migrated key hint, ...) is by definition an old
// or alternate identifier that should keep redirecting to canonical across
// runs, regardless of what format the alias string happens to be in — it is
// not expected to match the current EP-YYYY-MM-DD-HH-MM-SS key format. Callers
// that need transient, regenerated-every-run aliases (e.g. DBLP-derived ones,
// rebuilt fresh from the dblp field each run) should call
// l.KeyOldies.SetTransient directly instead of going through this function.
func (l *TBibTeXLibrary) AddKeyAlias(alias, key string) {
	canonical := l.MapEntryKey(key)
	if alias == canonical {
		return
	}
	if existing := l.KeyOldies.Get(alias); existing != "" {
		if existing == canonical {
			return
		}
		l.Warning(WarningAmbiguousKeyOldie, alias, existing, canonical)
		return
	}
	l.KeyOldies.Set(alias, canonical)
}

func (l *TBibTeXLibrary) AddKeyHint(hint, key string) {
	// DBLP keys belong in KeyOldies via AssociateDblpKey / the dblp field, not in HintToKey.
	if strings.HasPrefix(hint, "DBLP:") {
		return
	}
	resolvedKey := l.MapEntryKey(key)
	if hint == resolvedKey {
		return
	}
	if existing := l.HintToKey.GetValue(hint); existing != "" {
		if l.MapEntryKey(existing) == resolvedKey {
			return
		}
		l.Warning(WarningAmbiguousKeyHint, hint, existing, resolvedKey)
		return
	}
	l.HintToKey.Set(hint, resolvedKey)
	l.newKeyHints[hint] = resolvedKey
}

func (l *TBibTeXLibrary) AddFieldMapping(sourceField, sourceValue, targetField, targetValue string) {
	l.FieldMappings.SetValueForStringTripleMap(sourceField, sourceValue, targetField, targetValue)
	if !fieldMappingsLoading {
		bibExec( //nolint:errcheck
			`INSERT INTO field_mappings (source_field, source_value, target_field, target_value) VALUES (?, ?, ?, ?)
			  ON CONFLICT(source_field, source_value, target_field) DO UPDATE SET target_value = excluded.target_value`,
			sourceField, sourceValue, targetField, targetValue)
	}
}

/*
 *
 * Retrieval & lookup functions
 *
 */

// EntryFieldValueity returns the stored value for the given entry field, or "" if absent.
func (l *TBibTeXLibrary) EntryFieldValueity(entry, field string) string {
	if entryCache != nil {
		if e, ok := entryCache[entry]; ok {
			return e.Fields[field]
		}
		return ""
	}
	row := bibQueryRow(`SELECT value FROM bib_entries WHERE entry_key = ? AND field = ?`, entry, field)
	var value string
	row.Scan(&value)
	return value
}

func (l *TBibTeXLibrary) EntryType(entry string) string {
	return l.EntryFieldValueity(entry, EntryTypeField)
}

func (l *TBibTeXLibrary) PreferredKey(entry string) string {
	return l.EntryFieldValueity(entry, PreferredAliasField)
}

// LibrarySize returns the number of entries stored in the DB.
func (l *TBibTeXLibrary) LibrarySize() int {
	return countBibEntries()
}

// Reports the size of this library.
func (l *TBibTeXLibrary) ReportLibrarySize() {
	l.Progress(ProgressLibrarySize, l.LibrarySize())
}

// ONLY needed for migration???
// Lookup the entry key and type for a given key/alias
func (l *TBibTeXLibrary) MapEntryKeyWithType(key string) (string, string, bool) {
	deAliasedKey := l.MapEntryKey(key)

	if entryType := l.EntryType(deAliasedKey); entryType != "" {
		return deAliasedKey, entryType, true
	}

	return "", "", false
}

func (l *TBibTeXLibrary) MapEntryKey(key string) string {
	if canonical := l.KeyOldies.Get(key); canonical != "" {
		return canonical
	}
	return key
}

func (l *TBibTeXLibrary) LookupDBLPKey(DBLPkey string) string {
	return l.KeyOldies.Get(KeyForDBLP(DBLPkey))
}

// Create a string (with newlines) with a BibTeX based representation of the provided key, while using an optional prefix for each line.
func FormatBibTeXFieldAssignment(prefix, field, value string) string {
	return fmt.Sprintf("%s   %-*s = {%s},\n", prefix, BibTeXFieldColumnWidth, field, value)
}

func (l *TBibTeXLibrary) EntryString(key, groups string, prefixes ...string) string {
	entry := loadEntryFromDb(key)
	if !entry.Exists() {
		return ""
	}

	linePrefix := ""
	for _, prefix := range prefixes {
		linePrefix += prefix
	}

	result := linePrefix + "@" + entry.EntryType() + "{" + key + ",\n"

	if groups != "" {
		result += FormatBibTeXFieldAssignment(linePrefix, GroupsField, groups)
	}

	for _, field := range BibTeXAllowedEntryFields[entry.EntryType()].Set().ElementsSorted() {
		if field == EntryTypeField {
			continue
		}
		if field == LocalURLField {
			if l.PDFFiles[key] {
				result += FormatBibTeXFieldAssignment(linePrefix, field, l.FilesRoot+l.FilesFolder+key+".pdf")
			}
			continue
		}
		if value := entry.FieldValue(field); value != "" {
			mapped := l.MapEntryFieldValue(key, field, value)
			result += FormatBibTeXFieldAssignment(linePrefix, field, mapped)
		}
	}

	result += linePrefix + "}\n"
	return result
}

func CleanJRDate(s string) string {
	var trimJRDate = regexp.MustCompile(` \+.*`)

	return strings.Replace(trimJRDate.ReplaceAllString(s, ""), " ", "T", -1)
}

/*
 *
 * Checking functions
 *
 */

func (l *TBibTeXLibrary) EntryExists(entry string) bool {
	return bibEntryExists(entry)
}

// EntryHasFlag reports whether key has the given flag in EntryFlags.
func (l *TBibTeXLibrary) EntryHasFlag(key, flag string) bool {
	if flags, ok := l.EntryFlags[key]; ok {
		return flags.Set().Contains(flag)
	}
	return false
}

// SetEntryFlag adds flag to key's flag set and writes through to entry_metadata immediately.
func (l *TBibTeXLibrary) SetEntryFlag(key, flag string) {
	canon := l.MapEntryKey(key)
	if _, ok := l.EntryFlags[canon]; !ok {
		l.EntryFlags[canon] = TStringSetNew()
	}
	if !l.EntryFlags[canon].Set().Contains(flag) {
		l.EntryFlags[canon].Set().Add(flag)
		db.Exec(`INSERT OR IGNORE INTO entry_metadata (entry_key, property, value) VALUES (?, ?, 'true')`, canon, flag) //nolint:errcheck
	}
}

// buildEntry loads a TBibTeXEntry snapshot for key from the DB.
func (l *TBibTeXLibrary) buildEntry(key string) *TBibTeXEntry {
	return loadEntryFromDb(key)
}

// normPersonNameField applies NameAliasToName to each " and "-separated name in
// the value. Lightweight (map lookup only, no TeX tokenizer) and idempotent;
// keeps aliases resolved at the point of write for author/editor fields.
func (l *TBibTeXLibrary) normPersonNameField(names string) string {
	if len(l.NameAliasToName) == 0 {
		return names
	}
	parts := strings.Split(names, " and ")
	changed := false
	for i, p := range parts {
		n := NormalisePersonNameValue(l, strings.TrimSpace(p))
		if n != parts[i] {
			parts[i] = n
			changed = true
		}
	}
	if !changed {
		return names
	}
	return strings.Join(parts, " and ")
}

// setEntryField writes a field value to the entry. When the entry is open
// (openEntry was called), only entry.Fields is updated; the DB write is
// deferred to closeEntry. Otherwise the value is written to the DB immediately
// (and the cache when active).
func (l *TBibTeXLibrary) setEntryField(entry *TBibTeXEntry, field, value string) {
	if value != "" && (field == "author" || field == "editor") {
		value = l.normPersonNameField(value)
	}
	if _, open := entrySnapshots[entry.Key]; open {
		if value == "" {
			delete(entry.Fields, field)
		} else {
			entry.Fields[field] = value
		}
		return
	}
	upsertBibEntryField(entry.Key, field, value)
	if entryCache == nil {
		entry.Fields[field] = value
	}
}

// deleteEntryField removes a field from the entry. When the entry is open,
// only entry.Fields is updated; the DB delete is deferred to closeEntry.
// Otherwise the field is deleted from the DB immediately (and the cache).
func (l *TBibTeXLibrary) deleteEntryField(entry *TBibTeXEntry, field string) {
	if _, open := entrySnapshots[entry.Key]; open {
		delete(entry.Fields, field)
		return
	}
	deleteBibEntryField(entry.Key, field)
	if entryCache == nil {
		delete(entry.Fields, field)
	}
}

// DeleteEntry removes a canonical entry and all associated index data:
// key hints, key oldies, bib_entries rows, title index, and PDF file (→ library trash).
// The canonical key and all current aliases are recorded in deleted_entries so that
// subsequent subset syncs do not offer to re-add stale bib entries.
// The caller is responsible for resolving aliases to the canonical key beforehand.
func (l *TBibTeXLibrary) DeleteEntry(key string) {
	// Record the canonical key and every alias so sync bibs can be silently skipped.
	recordDeletedKey(key)
	l.HintToKey.DeleteWhere(func(hint, target string) bool {
		if l.MapEntryKey(target) == key {
			recordDeletedKey(hint)
			return true
		}
		return false
	})
	l.KeyOldies.EachAlias(key, func(alias string) {
		recordDeletedKey(alias)
	})
	l.KeyOldies.DeleteByTarget(key)
	l.TitleIndex.DeleteValueFromStringSetMap(TeXStringIndexer(l.EntryFieldValueity(key, TitleField)), key)
	// Move PDF to library trash before removing the DB entry.
	if l.PDFFiles[key] {
		l.moveToLibraryTrash(l.FilesRoot + l.FilesFolder + key + ".pdf") //nolint:errcheck
		delete(l.PDFFiles, key)
	}
	// Clean up entry_metadata so no orphan rows accumulate after deletion.
	if props, ok := l.Metadata[key]; ok {
		for prop := range props {
			db.Exec(`DELETE FROM entry_metadata WHERE entry_key = ? AND property = ?`, key, prop) //nolint:errcheck
		}
		delete(l.Metadata, key)
	}
	deleteBibEntry(key)
}

func (l *TBibTeXLibrary) AliasExists(alias string) bool {
	return l.KeyOldies.Has(alias)
}

// Checks if the provided winner is, indeed, the winner of the challenge by the challenger for the provided field of the provided entry.
func (l *TBibTeXLibrary) EntryFieldAliasHasTarget(entry, field, challenger, winner string) bool {
	return l.EntryFieldSourceToTarget.GetValueityFromStringTripleMap(entry, field, challenger) == winner
}

/*
 *
 * Support functions
 *
 */

// As the bibtex keys we generate are day and time based (down to seconds only), we need to have enough "room" to quickly generate new keys.
// By taking the time of starting the programme as the base, we can at least generate keys from that point in time on.
// However, we should never go beyond the present time, of course.
var (
	ForwardKeyTime  time.Time // The time used for the latest generated key. Is set (see init() below) at the start of the programme.
	BackwardKeyTime time.Time // XXXXXXXXXXXXXXXXXXXXXX. Is set (see init() below) at the start of the programme.
)

// ////// Place me somehwere
func KeyFromTime(KeyTime time.Time) string {
	return fmt.Sprintf(
		"%s-%04d-%02d-%02d-%02d-%02d-%02d",
		keyPrefix,
		KeyTime.Year(),
		int(KeyTime.Month()),
		KeyTime.Day(),
		KeyTime.Hour(),
		KeyTime.Minute(),
		KeyTime.Second())
}

// ////// Place me somehwere
func (l *TBibTeXLibrary) IsKnownKey(key string) bool {
	_, isContributor := l.ContributorByID[key]
	return bibEntryExists(key) || l.KeyOldies.Has(key) || isContributor
}

// Generate a new key based on the ForwardKeyTime.
func (l *TBibTeXLibrary) NewKey() string {
	var key string

	// We're not allowed to move into the future.
	if ForwardKeyTime.After(time.Now()) {
		// If we can't move forward, then look for a free key in the past
		for key = KeyFromTime(BackwardKeyTime); l.IsKnownKey(key); {
			// Move backward in time
			BackwardKeyTime = BackwardKeyTime.Add(time.Duration(-1) * time.Second)

			// Create the actual new key
			key = KeyFromTime(BackwardKeyTime)
		}
	} else {
		// Create the actual new key
		key = KeyFromTime(ForwardKeyTime)

		// Move to the next time for which we can generate a key.
		ForwardKeyTime = ForwardKeyTime.Add(time.Second)
	}

	return key
}

/*
 *
 * Recording entries by the parser
 *
 */

// Start recording to the library
func (l *TBibTeXLibrary) StartRecordingToLibrary() bool {
	l.illegalFields = TStringSetNew()
	atomic.StoreInt64(&bibParseCount, 0)
	return true
}

// Finish recording to the library
func (l *TBibTeXLibrary) FinishRecordingToLibrary() bool {
	// If we did encounter illegal fields we need to issue a warning.
	if !l.ignoreIllegalFields && l.illegalFields.Size() > 0 {
		l.Warning(WarningUnknownFields, l.illegalFields.String())
	}

	return true
}

// Report back if doubles were found
func (l *TBibTeXLibrary) FoundDoubles() bool {
	return l.foundDoubles
}

// Here for legacy purposes.
// Across the legacy files, we can have double occurrences.
// So, we need to add a unique prefix while parsing these entries.
var uniqueID int

// StartRecordingLibraryEntry begins recording a parsed BibTeX entry.
// When capturedHarvestEntries is active, each entry is captured in memory via
// capturedDBLPEntry instead of the DB. When capturedDBLPEntry is active alone,
// a single entry is captured (DBLP URL fetch mode).
func (l *TBibTeXLibrary) StartRecordingLibraryEntry(key, entryType string) bool {
	if l.capturedHarvestEntries != nil {
		l.capturedDBLPEntry = &TBibTeXEntry{Key: key, Fields: map[string]string{EntryTypeField: entryType}}
		return true
	}
	if l.capturedDBLPEntry != nil {
		l.capturedDBLPEntry.Key = key
		l.capturedDBLPEntry.Fields[EntryTypeField] = entryType
		return true
	}
	if !BibTeXAllowedEntries.Contains(entryType) {
		l.Warning(WarningUnknownEntryType, key, entryType)
		/////// MAYBE UPDATE THIS
		l.NoDBUpdating = true
	}

	// Check if an entry with the given key already exists
	if l.EntryExists(key) {
		// When the entry already exists, we need to report that we found doubles, as well as possibly resolve the entry type.

		l.Warning(WarningEntryAlreadyExists, key)
		l.foundDoubles = true

		// Resolve the double entry issue
		l.SetEntryType(key, l.ResolveFieldValue(key, key, EntryTypeField, entryType, l.EntryType(key)))
	} else {
		l.SetEntryType(key, entryType)
	}

	return true
}

// Assign a value to a field
func (l *TBibTeXLibrary) AssignField(key, field, value string) bool {
	// Note: The parser for BibTeX streams is responsible for the mapping of field name aliases, such as editors to editor, etc.
	// Here we only need to take care of the normalisation and processing of field values.
	// This includes the checking if e.g. files exist, and adding dblp keys as aliases.

	// In capture mode (harvest/DBLP parse), ProcessRawEntryFieldValue writes to the
	// in-memory capturedDBLPEntry and never touches the DB, so the live-DB check would
	// produce false "already has a value" warnings for every existing entry.
	if l.capturedDBLPEntry == nil {
		currentValue := l.EntryFieldValueity(key, field)
		if currentValue != "" {
			l.Warning("Entry %s already has a value for field %s of \"%s\"", key, field, currentValue)
		}
	}

	l.ProcessRawEntryFieldValue(key, field, value)

	// If the field is not allowed, we need to report this
	if !BibTeXAllowedFields.Contains(field) {
		l.illegalFields.Add(field)
	}

	return true
}

// applyMappingsForKey applies all target rules in l.FieldMappings[key][sourceValue]
// to entry, updating fieldIsChanged and returning whether any field was changed.
func (l *TBibTeXLibrary) applyMappingsForKey(entry *TBibTeXEntry, key, sourceValue string, fieldIsChanged map[string]bool, writeToDb bool) bool {
	changed := false
	for targetField, targetValue := range l.FieldMappings[key][sourceValue] {
		if entry.Fields[targetField] == "" && !fieldIsChanged[targetField] {
			if writeToDb {
				l.setEntryField(entry, targetField, targetValue)
			}
			entry.Fields[targetField] = targetValue
			fieldIsChanged[targetField] = true
			changed = true
		}
	}
	return changed
}

// MaybeApplyFieldMappings applies cross-field mappings to entry until no new fields
// are derived (saturation). Each target field is set only when currently empty, and
// at most once per iteration, guaranteeing termination. Conflicting rules for an
// already-assigned field are silently skipped.
//
// Mappings stored with a plain source_field (e.g. "author") apply to every entry
// type. Mappings stored with an entrytype-qualified source_field (e.g.
// "techreport:author") apply only when the entry's type matches.
//
// Pass writeToDb=true for library entries (field value persisted via setEntryField);
// false for temporary in-memory entries such as DBLP file-store entries, to avoid
// writing under a foreign key inside an outer transaction.
func (l *TBibTeXLibrary) MaybeApplyFieldMappings(entry *TBibTeXEntry, writeToDb bool) {
	entryType := entry.Fields[EntryTypeField]
	fieldIsChanged := map[string]bool{}
	for {
		changed := false
		for sourceField, sourceValue := range entry.Fields {
			if l.applyMappingsForKey(entry, sourceField, sourceValue, fieldIsChanged, writeToDb) {
				changed = true
			}
			if entryType != "" {
				if l.applyMappingsForKey(entry, sourceField+":"+entryType, sourceValue, fieldIsChanged, writeToDb) {
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
}

func (l *TBibTeXLibrary) CheckIfFieldsAreAllowed(entry *TBibTeXEntry, violationHandler func(string, string, string)) {
	for field, value := range entry.Fields {
		if !l.EntryAllowsForField(entry.Key, field) {
			violationHandler(entry.Key, field, value)
		}
	}
}

// FinishRecordingLibraryEntry completes parsing of a BibTeX entry.
// In harvest mode (capturedHarvestEntries non-nil), appends the finished entry to
// the slice and clears the per-entry scratch. In DBLP URL fetch mode
// (capturedDBLPEntry set alone), the entry is already in memory; skip DB steps.
func (l *TBibTeXLibrary) FinishRecordingLibraryEntry(key string) bool {
	if l.capturedHarvestEntries != nil {
		if l.capturedDBLPEntry != nil {
			*l.capturedHarvestEntries = append(*l.capturedHarvestEntries, *l.capturedDBLPEntry)
			l.capturedDBLPEntry = nil
		}
		return true
	}
	if l.capturedDBLPEntry != nil {
		return true
	}
	entry := loadEntryFromDb(key)

	if title := entry.FieldValue(TitleField); title != "" {
		l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(title), key)
	}

	if !l.InteractionIsOff() {
		l.CheckIfFieldsAreAllowed(entry, func(key, field, value string) {
			if l.ignoreIllegalFields || l.WarningYesNoQuestion(QuestionIgnore, WarningIllegalField, field, value, key, entry.EntryType()) {
				l.deleteEntryField(entry, field)
			} else {
				l.Warning("Stopping programme. Please fix this manually.")
				os.Exit(0)
			}
		})
	}

	l.MaybeApplyFieldMappings(entry, true)
	atomic.AddInt64(&bibParseCount, 1)
	return true
}

// resolveNonDoubleKey maps a raw key from the non-doubles file to the form that
// should be stored in memory and written back to disk:
//   - library entry (canonical, not an alias) → return its canonical key
//   - DBLP: key not yet imported into the library → return as-is (preserve for future match)
//   - anything else (stale alias, unknown key) → return "" (drop)
func (l *TBibTeXLibrary) resolveNonDoubleKey(rawKey string) string {
	canon := l.MapEntryKey(rawKey)
	if l.EntryExists(canon) {
		return canon
	}
	if strings.HasPrefix(rawKey, "DBLP:") {
		return rawKey
	}
	return ""
}

// nonDoublesLoadingFromDb suppresses write-through in AddNonDoubleEntries while
// loadKeyNonDoublesFromDb is populating in-memory state from an already-current DB.
var nonDoublesLoadingFromDb bool

func (l *TBibTeXLibrary) AddNonDoubleEntries(a, b string) {
	a = l.MapEntryKey(a)
	b = l.MapEntryKey(b)
	if a == b {
		return
	}
	existing := l.NonDoubleEntries[a]
	if existing.Contains(b) {
		return
	}

	s := TStringSet{}
	s.Initialise().Add(a, b).Unite(existing).Unite(l.NonDoubleEntries[b])

	for c := range s.Elements() {
		l.NonDoubleEntries[c] = s
	}
	// Write-through so Ctrl-C or step-limit exits cannot lose a dismissal decision.
	// Writes all pairs in the transitive set (not just the directly-added pair) so
	// the DB stays consistent with the in-memory union. Suppressed during DB load.
	if !nonDoublesLoadingFromDb {
		upsert := `INSERT INTO non_double_entries (key1, key2) VALUES (?, ?) ON CONFLICT DO NOTHING`
		for k1 := range s.Elements() {
			for k2 := range s.Elements() {
				if k1 != k2 {
					if err := bibExec(upsert, k1, k2); err != nil {
						dbInteraction.Warning("non_double_entries upsert failed: %s", err)
						dbWriteFailed = true
					}
				}
			}
		}
	}
}

// latexVisibleFields are the entry fields that affect how an entry renders in LaTeX.
// Used by ContentEqual to determine whether two entries represent the same publication.
// Internal-only fields (dblp, repositum, preferredalias, groups, etc.) are excluded.
var latexVisibleFields = []string{
	"author", "editor", "title", "booktitle", "journal",
	"year", "month", "volume", "number", "series", "pages", "chapter",
	"edition", "publisher", "address", "school", "institution", "type",
	"howpublished", "doi", "url", "note", "isbn", "issn", "eprint", "crossref",
}

// latexNormFieldValue returns the fully-normalised value of field for the given
// DB entry key. Applying both NormaliseFieldValue and MapFieldValue gives the
// same representation used during merge challenges, so comparisons stay consistent
// even as new losers are recorded during the same resolution pass (lazy normalisation).
func (l *TBibTeXLibrary) latexNormFieldValue(key, field string) string {
	return l.MapFieldValue(field, l.NormaliseFieldValue(field, l.EntryFieldValueity(key, field)))
}

// ContentEqual reports whether two DB entries are content-wise identical: every
// LaTeX-visible field normalises to the same value in both entries.
func (l *TBibTeXLibrary) ContentEqual(a, b string) bool {
	for _, field := range latexVisibleFields {
		if l.latexNormFieldValue(a, field) != l.latexNormFieldValue(b, field) {
			return false
		}
	}
	return true
}

// ResolveVariationSet runs a fixed-point loop over a list of entry IDs sharing
// the same title index. Content-equal pairs that show no evidence of being
// different publications are auto-merged silently. Pairs that differ but show
// no contradicting evidence are offered to the user; declining records a
// non-double so the pair is skipped on future passes. The loop repeats until
// a complete pass produces no merges (merge can expose new content-equal pairs
// because field challenges during MergeEntries may update the mapping tables).
// Returns the list of distinct live canonical keys that remain after resolution.
func (l *TBibTeXLibrary) ResolveVariationSet(list []string) []string {
	changed := true
	for changed {
		changed = false
		for i := range list {
			list[i] = l.MapEntryKey(list[i])
		}
		for i, e := range list {
			if !l.EntryExists(e) {
				continue
			}
			for j := i + 1; j < len(list); j++ {
				f := l.MapEntryKey(list[j])
				list[j] = f
				if !l.EntryExists(f) || e == f {
					continue
				}
				if l.NonDoubleEntries[e].Set().Contains(f) {
					continue
				}
				if l.ContentEqual(e, f) && !l.EvidenceForBeingDifferentEntries(e, f) {
					l.Progress("Auto-merging content-equal entries: %s ← %s", e, f)
					l.MergeEntries(f, e)
					e = l.MapEntryKey(e)
					list[i] = e
					list[j] = l.MapEntryKey(f)
					changed = true
				} else if !l.EvidenceForBeingDifferentEntries(e, f) {
					l.MaybeMergeEntries(e, f)
					newE := l.MapEntryKey(e)
					newF := l.MapEntryKey(f)
					list[i] = newE
					list[j] = newF
					if newE == newF {
						e = newE
						changed = true
					}
				}
			}
		}
	}
	seen := map[string]bool{}
	var result []string
	for _, e := range list {
		c := l.MapEntryKey(e)
		if c != "" && l.EntryExists(c) && !seen[c] {
			seen[c] = true
			result = append(result, c)
		}
	}
	return result
}

func init() {
	// Set this at the start so we have enough keys to be generated by the time we need them.
	ForwardKeyTime = time.Now()
	BackwardKeyTime = ForwardKeyTime
}
