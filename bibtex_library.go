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
		name                             string                    // Name of the library
		FilesRoot                        string                    // Path to folder with library related files
		BaseName                         string                    // BaseName of the library related files
		Comments                         []string                  // The Comments included in a BibTeX library. These are not always "just" Comments. BiBDesk uses this to store (as XML) information on e.g. static groups.
		EntryFields                      TStringStringMap          // Per entry key, the fields associated to the actual entries.
		TitleIndex                       TStringSetMap             //
		BookTitleIndex                   TStringSetMap             //
		ISBNIndex                        TStringSetMap             //
		FileMD5Index                     TStringSetMap             //
		DOIIndex                         TStringSetMap             //
		NonDoubles                       TStringSetMap             //
		KeyHintToKey                     TStringMap                // Mapping from key hints to the actual entry key.
		KeyAliasToKey                    TStringMap                // Mapping from key aliases to the actual entry key.
		FieldMappings                    TStringStringStringMap    // field/value to field/value mapping
		KeyIsTemporary                   TStringSet                // Keys that are generated for temporary reasons
		NameAliasToName                  TStringMap                // Mapping from name aliases to the actual name.
		NameToAliases                    TStringSetMap             // The inverted version of NameAliasToName
		illegalFields                    TStringSet                // Collect the unknown fields we encounter. We can warn about these when e.g. parsing has been finished.
		foundDoubles                     bool                      // If set, we found double entries. In this case, we may not want to e.g. write this file.
		EntryFieldAliasToTarget          TStringStringStringMap    // A key and field specific mapping from challenged value to winner values
		EntryFieldTargetToAliases        TStringStringStringSetMap // DO WE NEED THE INVERSES??
		GenericFieldAliasToTarget        TStringStringMap          // A field specific mapping from challenged value to winner values
		GenericFieldTargetToAliases      TStringStringSetMap       //
		NoBibFileWriting                 bool                      // If set, we should not write out a Bib file (and cache file) for this library as entries might have been lost.
		NoEntryFieldAliasesFileWriting   bool                      // If set, we should not write out a entry mappings file as entries might have been lost.
		NoGenericFieldAliasesFileWriting bool                      // If set, we should not write out a generic mappings file as entries might have been lost.
		NoKeyAliasesFileWriting          bool
		NoKeyHintsFileWriting            bool
		NoNameAliasesFileWriting         bool
		NoNonDoublesFileWriting          bool
		NoFieldMappingsFileWriting       bool
		IgnoreIllegalFields              bool
		TBibTeXTeX
		TInteraction  // Error reporting channel
		TBibTeXStream // BibTeX parser
	}
)

// Initialise a library
func (l *TBibTeXLibrary) Initialise(reporting TInteraction, name, filesRoot, baseName string) {
	l.TInteraction = reporting
	l.name = name
	l.Progress(ProgressInitialiseLibrary, l.name)

	l.TBibTeXStream = TBibTeXStream{}
	l.TBibTeXStream.Initialise(reporting, l)

	l.TBibTeXTeX = TBibTeXTeX{}

	l.TBibTeXTeX.library = l

	l.FilesRoot = filesRoot
	l.BaseName = baseName

	l.Comments = []string{}
	l.FieldMappings = TStringStringStringMap{}
	l.EntryFields = TStringStringMap{}
	l.TitleIndex = TStringSetMap{}
	l.BookTitleIndex = TStringSetMap{}
	l.ISBNIndex = TStringSetMap{}
	l.DOIIndex = TStringSetMap{}
	l.FileMD5Index = TStringSetMap{}
	l.NonDoubles = TStringSetMap{}
	l.KeyAliasToKey = TStringMap{}
	l.KeyIsTemporary = TStringSetNew()
	l.NameAliasToName = TStringMap{}

	l.EntryFieldTargetToAliases = TStringStringStringSetMap{}
	l.GenericFieldTargetToAliases = TStringStringSetMap{}

	l.foundDoubles = false
	l.EntryFieldAliasToTarget = TStringStringStringMap{}
	l.EntryFieldTargetToAliases = TStringStringStringSetMap{}
	l.GenericFieldAliasToTarget = TStringStringMap{}
	l.GenericFieldTargetToAliases = TStringStringSetMap{}

	l.NoBibFileWriting = false
	l.NoEntryFieldAliasesFileWriting = false
	l.NoGenericFieldAliasesFileWriting = false
	l.NoKeyAliasesFileWriting = false
	l.NoKeyHintsFileWriting = false
	l.NoNameAliasesFileWriting = false
	l.NoFieldMappingsFileWriting = false
	l.IgnoreIllegalFields = false
}

/*
 *
 * Set/add functions
 * These are safe in the sense of not causing problems when dealing with partially empty nested maps.
 *
 */

// (Safely) set the value for a field of a given entry.
func (l *TBibTeXLibrary) SetEntryFieldValue(entry, field, value string) {
	l.EntryFields.SetValueForStringPairMap(entry, field, value)
}

// (Safely) set the entry type
func (l *TBibTeXLibrary) SetEntryType(entry, value string) {
	l.EntryFields.SetValueForStringPairMap(entry, EntryTypeField, value)
}

// Add a comment to the current library.
func (l *TBibTeXLibrary) AddComment(comment string) bool {
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

	if l.GenericFieldAliasToTarget[field][alias] == target {
		return
	}

	// Check for ambiguity of aliases
	if check {
		if currentTarget, aliasIsAlreadyAliased := l.EntryFieldAliasToTarget[entry][field][alias]; aliasIsAlreadyAliased {
			if currentTarget != target {
				l.Warning("line 162: "+WarningAmbiguousAlias, alias, currentTarget, target)
				l.NoEntryFieldAliasesFileWriting = true

				return
			}
		}
	}

	// Set the actual mapping
	l.EntryFieldAliasToTarget.SetValueForStringTripleMap(entry, field, alias, target)

	// And inverse mapping
	l.EntryFieldTargetToAliases.AddValueToStringTrippleSetMap(entry, field, target, alias)
}

// Update the registration of a target over an alias for a given entry and its field.
// As we have a new target, we also need to update any other existing challenges for this field.
func (l *TBibTeXLibrary) UpdateGenericFieldAlias(field, alias, target string) {
	l.AddGenericFieldAlias(field, alias, target, true)

	for otherAlias, otherTarget := range l.GenericFieldAliasToTarget[field] {
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

	if l.GenericFieldAliasToTarget[field][alias] == target {
		return
	}

	// Check for ambiguity of aliases
	if check {
		if currentTarget, aliasIsAlreadyAliased := l.GenericFieldAliasToTarget[field][alias]; aliasIsAlreadyAliased {
			if currentTarget != target {
				l.Warning("line: 206"+WarningAmbiguousAlias, alias, currentTarget, target)
				l.NoGenericFieldAliasesFileWriting = true

				return
			}
		}
	}

	// Set the actual mapping
	l.GenericFieldAliasToTarget.SetValueForStringPairMap(field, alias, target)

	// And inverse mapping
	l.GenericFieldTargetToAliases.AddValueToStringPairSetMap(field, target, alias)

}

// Update the registration of a target over a alias for a given entry and its field.
// As we have a new target, we also need to update any other existing challenges for this field.
func (l *TBibTeXLibrary) UpdateEntryFieldAlias(entry, field, alias, target string) {
	l.AddEntryFieldAlias(entry, field, alias, target, true)

	for otherAlias, otherTarget := range l.EntryFieldAliasToTarget[entry][field] {
		if otherTarget == alias {
			l.AddEntryFieldAlias(entry, field, otherAlias, target, false)
		}
	}
}

func (l *TBibTeXLibrary) ReassignEntryFieldAliases(source, target string) {
	for field, AliasAssignments := range l.EntryFieldAliasToTarget[source] {
		for alias, winner := range AliasAssignments {
			if dealiasedWinner := l.DeAliasEntryFieldValue(target, field, winner); dealiasedWinner != "" {
				l.AddEntryFieldAlias(target, field, alias, dealiasedWinner, false)
			}
		}
	}
}

// Add an implied alias ... order of key/alias ... seems not consistent.
//func (l *TBibTeXLibrary) AddImpliedKeyAlias(key, alias string) {
//	knownKey, keyExists := l.KeyAliasToKey[alias]
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

// Add a new name alias
func (l *TBibTeXLibrary) AddAliasForName(alias, name string, aliasMap *TStringMap, inverseMap *TStringSetMap) {
	l.MaybeAddReorderedName(name, name, aliasMap, inverseMap)
	l.MaybeAddReorderedName(alias, name, aliasMap, inverseMap)

	l.AddAlias(alias, name, aliasMap, inverseMap, true)
}

func (l *TBibTeXLibrary) FileReferencety(key string) string {
	var (
		trimFileReferenceStart = regexp.MustCompile(`^[~:]*:`)
		trimFileReferenceEnd   = regexp.MustCompile(`:[^:]*$`)
	)

	FileReference := l.EntryFieldValueity(key, "file")

	// Remove leading/trailing stuff
	FileReference = trimFileReferenceStart.ReplaceAllString(FileReference, "")
	FileReference = trimFileReferenceEnd.ReplaceAllString(FileReference, "")

	return FileReference
}

// /// SPLIT??
// /// fix source/target orders ...
// /// MergeEntryFromTo(source, target)
// ///   ResolveFieldValueOfAgainst(current, challenge)
func (l *TBibTeXLibrary) MergeEntries(sourceRAW, targetRAW string) string {
	if sourceRAW != "" && targetRAW != "" {
		// Fix names
		source := sourceRAW //l.DeAliasEntryKey(sourceRAW)
		target := l.DeAliasEntryKey(targetRAW)

		if source != target && l.EntryExists(source) { // }&& l.EntryExists(target) {
			l.Progress("Merging %s to %s", source, target)

			sourceType := l.EntryType(source)
			targetType := l.EntryType(target)
			targetType = l.MaybeResolveFieldValue(target, source, EntryTypeField, sourceType, targetType)
			l.SetEntryType(target, targetType)

			// Can be like a constant ...
			regularFields := TStringSet{}
			regularFields.Initialise().Unite(BibTeXAllowedEntryFields[targetType])
			for regularField := range regularFields.Elements() {
				// Do we need l.EntryFields still as (implied) parameter for MaybeResolveFieldValue, once we're done with migrating/legacy??
				l.EntryFields.SetValueForStringPairMap(target, regularField, l.MaybeResolveFieldValue(target, source, regularField, l.EntryFields[source][regularField], l.EntryFields[target][regularField]))
			}

			if !l.KeyIsTemporary.Contains(source) {
				l.AddKeyAlias(source, target)
				l.AddNonDoubles(source, target)
			}
			l.ReassignEntryFieldAliases(source, target)

			delete(l.EntryFields, source)

			l.CheckEntry(target)
		}

		return target
	}

	return ""
}

func EvidencedUnequal(a, b string) bool {
	return a != "" && b != "" && a != b
}

func (l *TBibTeXLibrary) EvidencedUnequalEntryFields(source, target, field string) bool {
	return EvidencedUnequal(l.EntryFieldValueity(source, field), l.EntryFieldValueity(target, field))
}

func (l *TBibTeXLibrary) EvidenceForBeingDifferentEntries(source, target string) bool {
	return l.EvidencedUnequalEntryFields(source, target, "dblp") ||
		l.EvidencedUnequalEntryFields(source, target, "doi") ||
		l.EvidencedUnequalEntryFields(source, target, "crossref")
}

func (l *TBibTeXLibrary) MaybeMergeEntries(sourceRAW, targetRAW string) {
	// Fix names
	source := l.DeAliasEntryKey(sourceRAW)
	target := l.DeAliasEntryKey(targetRAW)

	if source != target && !l.NonDoubles[source].Set().Contains(target) && !l.EvidenceForBeingDifferentEntries(source, target) {
		l.Warning("Found potential double entries")

		sourceEntry := l.EntryString(source, "  ")
		targetEntry := l.EntryString(target, "  ")

		if sourceEntry == "" {
			l.Warning("Empty source entry: %s", source)
		}

		if targetEntry == "" {
			l.Warning("Empty target entry: %s", target)
		}

		if l.WarningYesNoQuestion("Merge these entries", "First entry:\n%s\nSecond entry:\n%s", sourceEntry, targetEntry) {
			l.MergeEntries(source, target)
			l.WriteAliasesFiles()
			l.WriteMappingsFiles()
			l.WriteBibTeXFile()
			l.WriteCache()
		} else {
			l.AddNonDoubles(source, target)
			l.WriteNonDoublesFile()
		}
	}
}

func (l *TBibTeXLibrary) MaybeMergeEntrySet(keys TStringSet) {
	if keys.Size() > 1 {
		sortedkeys := keys.ElementsSorted()
		for _, a := range sortedkeys {
			aDeAlias := l.DeAliasEntryKey(a)
			if a == aDeAlias {
				for _, b := range sortedkeys {
					bDeAlias := l.DeAliasEntryKey(b)
					if b == bDeAlias {
						l.MaybeMergeEntries(aDeAlias, bDeAlias)
					}
				}
			}
		}
	}
}

func (l *TBibTeXLibrary) AddAliasForKey(aliasMap *TStringMap, alias, key, warning string) bool {
	key = l.DeAliasEntryKey(key)

	if alias == key {
		return true
	} else {
		if currentKey, aliasIsUsedAsKeyForSomeAlias := (*aliasMap)[alias]; aliasIsUsedAsKeyForSomeAlias {
			currentKey = l.DeAliasEntryKey(currentKey)

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

// Add a new key alias ... AddAliasForKey would be the more consistent name
func (l *TBibTeXLibrary) AddKeyAlias(alias, key string) {
	l.NoKeyAliasesFileWriting = l.NoKeyAliasesFileWriting || !l.AddAliasForKey(&l.KeyAliasToKey, alias, key, WarningAmbiguousKeyAlias)
}

// Add a new key hint
func (l *TBibTeXLibrary) AddKeyHint(hint, key string) {
	l.NoKeyHintsFileWriting = l.NoKeyHintsFileWriting || !l.AddAliasForKey(&l.KeyHintToKey, hint, key, WarningAmbiguousKeyHint)
}

func (l *TBibTeXLibrary) AddFieldMapping(sourceField, sourceValue, targetField, targetValue string) {
	l.FieldMappings.SetValueForStringTripleMap(sourceField, sourceValue, targetField, targetValue)
}

/*
 *
 * Retrieval & lookup functions
 *
 */

// Get the value of the field of a specific entry. Returns the empty string if it is not there.
func (l *TBibTeXLibrary) EntryFieldValueity(entry, field string) string {
	return l.EntryFields.GetValueityFromStringPairMap(entry, field)
}

func (l *TBibTeXLibrary) EntryType(entry string) string {
	return l.EntryFieldValueity(entry, EntryTypeField)
}

func (l *TBibTeXLibrary) PreferredKey(entry string) string {
	return l.EntryFieldValueity(entry, PreferredKeyField)
}

// Returns the size of this library.
func (l *TBibTeXLibrary) LibrarySize() int {
	return len(l.EntryFields)
}

// Reports the size of this library.
func (l *TBibTeXLibrary) ReportLibrarySize() {
	l.Progress(ProgressLibrarySize, l.name, l.LibrarySize())
}

// ONLY needed for migration???
// Lookup the entry key and type for a given key/alias
func (l *TBibTeXLibrary) DeAliasEntryKeyWithType(key string) (string, string, bool) {
	deAliasedKey := l.DeAliasEntryKey(key)

	if entryType := l.EntryType(deAliasedKey); entryType != "" {
		return deAliasedKey, entryType, true
	}

	return "", "", false
}

// Lookup the entry key for a given key/alias
func (l *TBibTeXLibrary) EntryKeyAliasTraverser(originalKey, currentKey string, visited TStringSet) string {
	lookupKey, isAlias := l.KeyAliasToKey[currentKey]

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

func (l *TBibTeXLibrary) DeAliasEntryKey(key string) string {
	visited := TStringSetNew()

	return l.EntryKeyAliasTraverser(key, key, visited)
}

func (l *TBibTeXLibrary) LookupDBLPKey(DBLPkey string) string {
	lookupKey, isAlias := l.KeyAliasToKey[KeyForDBLP(DBLPkey)]

	if isAlias {
		return lookupKey
	} else {
		return ""
	}
}

// Create a string (with newlines) with a BibTeX based representation of the provided key, while using an optional prefix for each line.
func (l *TBibTeXLibrary) EntryString(key string, prefixes ...string) string {
	_, knownEntry := l.EntryFields[key]

	if knownEntry {
		// Combine all prefixes into one
		linePrefix := ""
		for _, prefix := range prefixes {
			linePrefix += prefix
		}

		result := ""
		// Add the type and key
		result = linePrefix + "@" + l.EntryType(key) + "{" + key + ",\n"

		// Iterate over the fields and their values .... l.EntryTypes[key] via type := ??
		for _, field := range BibTeXAllowedEntryFields[l.EntryType(key)].Set().ElementsSorted() {
			if field != EntryTypeField { // Fix this with AllowedEntryFields with/without it
				if value := l.EntryFieldValueity(key, field); value != "" {
					result += linePrefix + "   " + field + " = {" + l.DeAliasEntryFieldValue(key, field, value) + "},\n"
				}
			}
		}

		// Close the entry statement
		result += linePrefix + "}\n"

		return result
	} else {
		// When the specified entry does not exist, all we can do is return the empty string
		return ""
	}
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
	return l.EntryType(entry) != ""
}

func (l *TBibTeXLibrary) AliasExists(alias string) bool {
	return l.KeyAliasToKey.IsMappedString(alias)
}

// Checks if the provided winner is, indeed, the winner of the challenge by the challenger for the provided field of the provided entry.
func (l *TBibTeXLibrary) EntryFieldAliasHasTarget(entry, field, challenger, winner string) bool {
	return l.EntryFieldAliasToTarget.GetValueityFromStringTripleMap(entry, field, challenger) == winner
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
		KeyPrefix,
		ForwardKeyTime.Year(),
		int(KeyTime.Month()),
		KeyTime.Day(),
		KeyTime.Hour(),
		KeyTime.Minute(),
		KeyTime.Second())
}

// ////// Place me somehwere
func (l *TBibTeXLibrary) IsKnownKey(key string) bool {
	_, KnownOriginalKey := l.EntryFields[key]
	_, KnownAliasKey := l.KeyAliasToKey[key]

	return KnownOriginalKey || KnownAliasKey
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
	if !l.IgnoreIllegalFields && l.illegalFields.Size() > 0 {
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

// Start to record a library entry
func (l *TBibTeXLibrary) StartRecordingLibraryEntry(key, entryType string) bool {
	if !BibTeXAllowedEntries.Contains(entryType) {
		l.Warning(WarningUnknownEntryType, key, entryType)
		/////// MAYBE UPDATE THIS
		l.NoBibFileWriting = true
	}

	// Check if an entry with the given key already exists
	if l.EntryExists(key) {
		// When the entry already exists, we need to report that we found doubles, as well as possibly resolve the entry type.

		l.Warning(WarningEntryAlreadyExists, key)
		l.foundDoubles = true

		// Resolve the double entry issue
		l.SetEntryType(key, l.ResolveFieldValue(key, key, EntryTypeField, entryType, l.EntryType(key)))
	} else {
		l.EntryFields[key] = TStringMap{}
		l.SetEntryType(key, entryType)
	}

	return true
}

// Assign a value to a field
// Post legacy ... we may want to add a key as well, when the parser maintains the current key on that side.
func (l *TBibTeXLibrary) AssignField(key, field, value string) bool {
	// Note: The parser for BibTeX streams is responsible for the mapping of field name aliases, such as editors to editor, etc.
	// Here we only need to take care of the normalisation and processing of field values.
	// This includes the checking if e.g. files exist, and adding dblp keys as aliases.

	newValue := l.ProcessEntryFieldValue(key, field, value)
	currentValue := l.EntryFieldValueity(key, field)

	// Assign the new value, while, if needed, resolve it with the current value
	l.EntryFields[key][field] = l.MaybeResolveFieldValue(key, key, field, newValue, currentValue)

	// If the field is not allowed, we need to report this
	if !BibTeXAllowedFields.Contains(field) {
		l.illegalFields.Add(field)
	}

	return true
}

// Update versus DeAlias versus Normalise ... yuck!!
func (l *TBibTeXLibrary) UpdateFieldValue(field, value string) string {
	return l.DeAliasFieldValue(field, l.NormaliseFieldValue(field, "", value))
}

func (l *TBibTeXLibrary) MaybeApplyFieldMappings(key string) {
	for sourceField, sourceValue := range l.EntryFields[key] {
		for targetField, targetValue := range l.FieldMappings[sourceField][sourceValue] {
			l.SetEntryFieldValue(key, targetField, targetValue)
		}
	}
}

// Finish recording the current library entry
func (l *TBibTeXLibrary) FinishRecordingLibraryEntry(key string) bool {
	//	if ISBN := l.EntryFieldValueity(key, "isbn"); ISBN != "" {
	//		l.ISBNIndex.AddValueToStringSetMap(ISBN, key)
	//	}

	//	if DOI := l.EntryFieldValueity(key, "doi"); DOI != "" {
	//		l.DOIIndex.AddValueToStringSetMap(DOI, key)
	//	}

	if title := l.EntryFieldValueity(key, "title"); title != "" {
		l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(title), key)
	}

	//	if bookTitle := l.EntryFieldValueity(key, "booktitle"); bookTitle != "" {
	//		l.BookTitleIndex.AddValueToStringSetMap(TeXStringIndexer(bookTitle), key)
	//	}

	// Check if no illegal fields were used
	// As this potentially requires interaction with the user, we only do this when we're not in silenced mode.
	if !l.InteractionIsOff() {
		for field, value := range l.EntryFields[key] {
			// Check if the field is allowed for this type.
			// If not, we need to ask if it can be deleted.
			if !l.EntryAllowsForField(key, field) {
				if l.IgnoreIllegalFields || l.WarningYesNoQuestion(QuestionIgnore, WarningIllegalField, field, value, key, l.EntryType(key)) {
					delete(l.EntryFields[key], field)
				} else {
					l.Warning("Stopping programme. Please fix this manually.")
					os.Exit(0)
				}
			}
		}
	}

	l.MaybeApplyFieldMappings(key)

	return true
}

func (l *TBibTeXLibrary) AddNonDoubles(a, b string) {
	s := TStringSet{}
	s.Initialise().Add(a, b).Unite(l.NonDoubles[a]).Unite(l.NonDoubles[b])

	for c := range s.Elements() {
		l.NonDoubles[c] = s
	}
}

func init() {
	// Set this at the start so we have enough keys to be generated by the time we need them.
	ForwardKeyTime = time.Now()
	BackwardKeyTime = ForwardKeyTime
}
