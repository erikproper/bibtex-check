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
	"strings"
	"time"
)

/*
 *
 * Definition of the Library type
 *
 */

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
		NonDoubles                        TStringSetMap             //
		HintToKey                         TStringMap                // Mapping from key hints to the actual entry key.
		newKeyHints                       TStringMap                // Key hints added in the current run (for append-only write).
		KeyToKey                          TStringMap                // Mapping from key aliases to the actual entry key.
		FieldMappings                     TStringStringStringMap    // field/value to field/value mapping
		KeyIsTemporary                    TStringSet                // Keys that are generated for temporary reasons
		NameAliasToName                   TStringMap                // Mapping from name aliases to the actual name.
		NameToAliases                     TStringSetMap             // The inverted version of NameAliasToName
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
		NoEntryFieldMappingsFileWriting   bool                      // If set, we should not write out a entry mappings file as entries might have been lost.
		NoGenericFieldMappingsFileWriting bool                      // If set, we should not write out a generic mappings file as entries might have been lost.
		NoKeyOldiesFileWriting            bool
		NoKeyHintsFileWriting             bool
		NoNameMappingsFileWriting         bool
		NoKeyNonDoublesFileWriting        bool
		keyNonDoublesModified             bool
		DblpParentOverrides               TStringMap // child DBLP key → resolved parent DBLP key
		NoDblpParentFileWriting           bool
		dblpParentModified                bool
		DblpWaived                        TStringSet // library keys exempt from WarningNoDblpKeyForChild
		NoDblpWaivedFileWriting           bool
		dblpWaivedModified                bool
		Metadata              TEntryMetadata // per-entry metadata (see bibtex_library_metadata.go)
		metadataModified      bool
		NoMetadataFileWriting bool
		EntryFlags                        map[string]TStringSet // canonical key → set of flag strings
		NoEntryFlagsFileWriting           bool
		entryFlagsModified               bool
		nameMappingsModified              bool
		keyHintsModified                  bool
		keyOldiesModified                 bool
		crossFieldMappingsModified        bool
		genericFieldMappingsModified      bool
		entryFieldMappingsModified        bool
		harvestNameAliases                bool
		localURLBase                      string // when non-empty, prepended to local-url values in EntryString
		capturedDBLPEntry                 *TBibTeXEntry
		capturedHarvestEntries            *[]TBibTeXEntry // when non-nil, parsed entries collected here instead of DB
		NoCrossFieldMappingsFileWriting   bool
		URLsIgnore                        TStringSet
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

	l.Comments = []string{}
	l.FieldMappings = TStringStringStringMap{}
	l.GroupEntries = TStringSetMap{}
	l.TitleIndex = TStringSetMap{}
	//	l.BookTitleIndex = TStringSetMap{}
	l.ISBNIndex = TStringSetMap{}
	l.DOIIndex = TStringSetMap{}
	l.NonDoubles = TStringSetMap{}
	l.KeyToKey = TStringMap{}
	l.KeyIsTemporary = TStringSetNew()
	l.NameAliasToName = TStringMap{}
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
	l.NoEntryFieldMappingsFileWriting = false
	l.NoGenericFieldMappingsFileWriting = false
	l.NoKeyOldiesFileWriting = false
	l.NoKeyHintsFileWriting = false
	l.newKeyHints = TStringMap{}
	l.NoNameMappingsFileWriting = false
	l.NoCrossFieldMappingsFileWriting = false
	l.Metadata = TEntryMetadata{}
	l.NoMetadataFileWriting = false
	l.URLsIgnore = TStringSetNew()
	l.DblpParentOverrides = TStringMap{}
	l.NoDblpParentFileWriting = false
	l.DblpWaived = TStringSetNew()
	l.NoDblpWaivedFileWriting = false
	l.EntryFlags = map[string]TStringSet{}
	l.ignoreIllegalFields = false
}

/*
 *
 * Set/add functions
 * These are safe in the sense of not causing problems when dealing with partially empty nested maps.
 *
 */

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

// Add a comment to the current library.
func (l *TBibTeXLibrary) ProcessComment(comment string) bool {
	// if ! l.BibDeskStaticGroupDefinition(comment) {
	//   l.Comments = append(l.Comments, comment)
	// } else {
	// Should go, once we can write such group fields
	l.Comments = append(l.Comments, comment)
	// }

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
	l.entryFieldMappingsModified = true
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
				l.NoGenericFieldMappingsFileWriting = true

				return
			}
		}
	}

	// Set the actual mapping
	l.GenericFieldSourceToTarget.SetValueForStringPairMap(field, alias, target)

	// And inverse mapping
	l.GenericFieldTargetToSource.AddValueToStringPairSetMap(field, target, alias)
	l.genericFieldMappingsModified = true
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
				l.entryFieldMappingsModified = true
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
				l.Warning("Line 290:"+WarningAmbiguousAlias, alias, currentOriginal, original)

				return
			}
		}
	}

	// Set the actual mapping
	aliasMap.SetValueForStringMap(alias, original)

	// Also create update the inverse mapping
	inverseMap.AddValueToStringSetMap(original, alias)
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
		return false
	}
	l.NameAliasToName[alias] = canonical
	l.NameToAliases.AddValueToStringSetMap(canonical, alias)
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
// If canonical is itself an alias of another name, the call is redirected to
// that name's canonical so the mapping graph stays acyclic.
func (l *TBibTeXLibrary) AddNameMapping(canonical, alias string) {
	if existingCanonical, isMapped := l.NameAliasToName[canonical]; isMapped {
		l.AddNameMapping(existingCanonical, alias)
		return
	}

	// If alias is currently a canonical, absorb all its aliases into canonical.
	if aliasSet, isCanonical := l.NameToAliases[alias]; isCanonical {
		for a := range aliasSet.Elements() {
			l.NameAliasToName[a] = canonical
			l.NameToAliases.AddValueToStringSetMap(canonical, a)
		}
		delete(l.NameToAliases, alias)
	}

	l.AddAlias(alias, canonical, &l.NameAliasToName, &l.NameToAliases, false)
	l.FindAliases(canonical, alias)
	l.FindAliases(canonical, canonical)
	l.nameMappingsModified = true
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
		l.nameMappingsModified = true
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
		l.nameMappingsModified = true
	}
}

// Really needed?
func (l *TBibTeXLibrary) FileReferencety(key string) string {
	return l.EntryFieldValueity(key, LocalURLField)
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
				if EvidencedUnequal(sv, tv) {
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

			if !l.KeyIsTemporary.Contains(source) {
				l.AddKeyAlias(source, target)
				l.AddNonDoubles(source, target)
			}
			l.ReassignEntryFieldMappings(source, target)

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
	return l.EvidencedUnequalEntryFields(source, target, DBLPField) ||
		l.EvidencedUnequalEntryFields(source, target, "doi")
	// || l.EvidencedUnequalEntryFields(source, target, "crossref")
}

// entryDisplayString returns the entry's BibTeX string, appending the parent
// entry when a crossref is present to give full context for merge decisions.
func (l *TBibTeXLibrary) entryDisplayString(key string) string {
	s := l.EntryString(key, "", "  ")
	if crossref := l.EntryFieldValueity(key, "crossref"); crossref != "" {
		if parentStr := l.EntryString(crossref, "", "  "); parentStr != "" {
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
		if source != target && !l.NonDoubles[source].Set().Contains(target) {
			if l.PreMergeCheck != nil {
				l.PreMergeCheck(source, target)
				source = l.MapEntryKey(source)
				target = l.MapEntryKey(target)
			}
		}
		if source != target && !l.NonDoubles[source].Set().Contains(target) && !l.EvidenceForBeingDifferentEntries(source, target) {
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
				l.AddNonDoubles(source, target)
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

func (l *TBibTeXLibrary) addAliasForKey(aliasMap *TStringMap, alias, key, warning string) bool {
	key = l.MapEntryKey(key)

	if alias == key {
		return true
	} else {
		if currentKey, aliasIsUsedAsKeyForSomeAlias := (*aliasMap)[alias]; aliasIsUsedAsKeyForSomeAlias {
			currentKey = l.MapEntryKey(currentKey)

			if currentKey == key {
				return true
			} else {
				l.Warning(warning, alias, currentKey, key)

				return false
			}
		} else {
			(*aliasMap).SetValueForStringMap(alias, key)

			return true
		}
	}
}

// Add a new key alias ... addAliasForKey would be the more consistent name
func (l *TBibTeXLibrary) AddKeyAlias(alias, key string) {
	if l.addAliasForKey(&l.KeyToKey, alias, key, WarningAmbiguousKeyOldie) {
		l.keyOldiesModified = true
	} else {
		l.NoKeyOldiesFileWriting = true
	}
}

// Add a new key hint
func (l *TBibTeXLibrary) AddKeyHint(hint, key string) {
	resolvedKey := l.MapEntryKey(key)
	if hint == resolvedKey {
		return
	}
	if existing, ok := l.HintToKey[hint]; ok && l.MapEntryKey(existing) == resolvedKey {
		return
	}
	if l.addAliasForKey(&l.HintToKey, hint, key, WarningAmbiguousKeyHint) {
		l.keyHintsModified = true
		l.newKeyHints[hint] = key
	} else {
		l.NoKeyHintsFileWriting = true
	}
}

func (l *TBibTeXLibrary) AddFieldMapping(sourceField, sourceValue, targetField, targetValue string) {
	l.FieldMappings.SetValueForStringTripleMap(sourceField, sourceValue, targetField, targetValue)
	l.crossFieldMappingsModified = true
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

// Lookup the entry key for a given key/alias
func (l *TBibTeXLibrary) EntryKeyAliasTraverser(originalKey, currentKey string, visited TStringSet) string {
	lookupKey, isAlias := l.KeyToKey[currentKey]

	if isAlias && currentKey != lookupKey {
		if visited.Contains(lookupKey) {
			l.Warning("Cycle in key alias assignments %s.", lookupKey)

			return lookupKey
		}

		visited.Add(currentKey)
		return l.EntryKeyAliasTraverser(originalKey, lookupKey, visited)
	}

	return currentKey
}

func (l *TBibTeXLibrary) MapEntryKey(key string) string {
	visited := TStringSetNew()

	return l.EntryKeyAliasTraverser(key, key, visited)
}

func (l *TBibTeXLibrary) LookupDBLPKey(DBLPkey string) string {
	lookupKey, isAlias := l.KeyToKey[KeyForDBLP(DBLPkey)]

	if isAlias {
		return lookupKey
	} else {
		return ""
	}
}

// Create a string (with newlines) with a BibTeX based representation of the provided key, while using an optional prefix for each line.
func FormatBibTeXFieldAssignment(prefix, field, value string) string {
	return prefix + "   " + field + " = {" + value + "},\n"
}

// resolvedLocalURL prepends localURLBase to a relative local-url value when the base is set.
// Used by both EntryString and entryGetString so both renderers produce identical paths.
func (l *TBibTeXLibrary) resolvedLocalURL(value string) string {
	if l.localURLBase != "" && !strings.HasPrefix(value, "/") {
		return l.localURLBase + value
	}
	return value
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
		if field != EntryTypeField {
			if field == LocalURLField && l.localURLBase == "" {
				continue // omit local-url in non-export contexts (display, field challenges)
			}
			if value := entry.FieldValue(field); value != "" {
				mapped := l.MapEntryFieldValue(key, field, value)
				if field == LocalURLField {
					mapped = l.resolvedLocalURL(mapped)
				}
				result += FormatBibTeXFieldAssignment(linePrefix, field, mapped)
			}
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

// SetEntryFlag adds flag to key's flag set and marks the table modified.
func (l *TBibTeXLibrary) SetEntryFlag(key, flag string) {
	canon := l.MapEntryKey(key)
	if _, ok := l.EntryFlags[canon]; !ok {
		l.EntryFlags[canon] = TStringSetNew()
	}
	if !l.EntryFlags[canon].Set().Contains(flag) {
		l.EntryFlags[canon].Set().Add(flag)
		l.entryFlagsModified = true
	}
}

// buildEntry loads a TBibTeXEntry snapshot for key from the DB.
func (l *TBibTeXLibrary) buildEntry(key string) *TBibTeXEntry {
	return loadEntryFromDb(key)
}

// setEntryField writes a field value to the entry. When the entry is open
// (openEntry was called), only entry.Fields is updated; the DB write is
// deferred to closeEntry. Otherwise the value is written to the DB immediately
// (and the cache when active).
func (l *TBibTeXLibrary) setEntryField(entry *TBibTeXEntry, field, value string) {
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

func (l *TBibTeXLibrary) AliasExists(alias string) bool {
	return l.KeyToKey.IsMappedString(alias)
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
	_, KnownAliasKey := l.KeyToKey[key]
	return bibEntryExists(key) || KnownAliasKey
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
	// Reset the set of the illegal fields we may have encountered.
	l.illegalFields = TStringSetNew()

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

// MaybeApplyFieldMappings applies cross-field mappings to entry until no new fields
// are derived (saturation). Each target field is assigned at most once across all
// iterations, which allows mappings to override non-empty fields while still
// guaranteeing termination. Conflicting rules for an already-assigned field are
// silently skipped.
// Pass writeToDb=true for library entries (field value persisted via setEntryField);
// false for temporary in-memory entries such as DBLP file-store entries, to avoid
// writing under a foreign key inside an outer transaction.
func (l *TBibTeXLibrary) MaybeApplyFieldMappings(entry *TBibTeXEntry, writeToDb bool) {
	fieldIsChanged := map[string]bool{}
	for {
		changed := false
		for sourceField, sourceValue := range entry.Fields {
			for targetField, targetValue := range l.FieldMappings[sourceField][sourceValue] {
				if entry.Fields[targetField] != targetValue {
					if !fieldIsChanged[targetField] {
						if writeToDb {
							l.setEntryField(entry, targetField, targetValue)
						}
						entry.Fields[targetField] = targetValue
						fieldIsChanged[targetField] = true
						changed = true
					}
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

// nonDoublesLoadingFromDb suppresses write-through in AddNonDoubles while
// loadKeyNonDoublesFromDb is populating in-memory state from an already-current DB.
var nonDoublesLoadingFromDb bool

func (l *TBibTeXLibrary) AddNonDoubles(a, b string) {
	existing := l.NonDoubles[a]
	if existing.Contains(b) {
		return
	}

	s := TStringSet{}
	s.Initialise().Add(a, b).Unite(existing).Unite(l.NonDoubles[b])

	for c := range s.Elements() {
		l.NonDoubles[c] = s
	}
	l.keyNonDoublesModified = true

	// Write-through so Ctrl-C or step-limit exits cannot lose a dismissal decision.
	// Writes all pairs in the transitive set (not just the directly-added pair) so
	// the DB stays consistent with the in-memory union. Suppressed during DB load.
	if !nonDoublesLoadingFromDb {
		upsert := `INSERT INTO key_non_doubles (key1, key2) VALUES (?, ?) ON CONFLICT DO NOTHING`
		for k1 := range s.Elements() {
			for k2 := range s.Elements() {
				if k1 != k2 {
					db.Exec(upsert, k1, k2)
				}
			}
		}
	}
}

func init() {
	// Set this at the start so we have enough keys to be generated by the time we need them.
	ForwardKeyTime = time.Now()
	BackwardKeyTime = ForwardKeyTime
}
