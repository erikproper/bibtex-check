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
		name                        string                    // Name of the library
		FilesRoot                   string                    // Path to folder with library related files
		BaseName                    string                    // BaseName of the library related files
		Comments                    []string                  // The Comments included in a BibTeX library. These are not always "just" Comments. BiBDesk uses this to store (as XML) information on e.g. static groups.
		EntryFields                 TStringStringMap          // Per entry key, the fields associated to the actual entries.
		TitleIndex                  TStringSetMap             //
		BookTitleIndex              TStringSetMap             //
		ISBNIndex                   TStringSetMap             //
		BDSKFileIndex               TStringSetMap             //
		DOIIndex                    TStringSetMap             //
		EntryTypes                  TStringMap                // Per entry key, the type of the enty.
		KeyAliasToKey               TStringMap                // Mapping from key aliases to the actual entry key.
		SeriesToISSN                TStringMap                // Mapping from series/journals to ISSN
		KeyToAliases                TStringSetMap             // The inverted version of KeyAliasToKey NEEEEEEEDED??????
		PreferredKeyAliases         TStringMap                // Per entry key, the preferred alias
		NameAliasToName             TStringMap                // Mapping from name aliases to the actual name.
		NameToAliases               TStringSetMap             // The inverted version of NameAliasToName
		OrganisationalAddresses     TStringMap                // Addresses of publishers, organisations, etc.
		illegalFields               TStringSet                // Collect the unknown fields we encounter. We can warn about these when e.g. parsing has been finished.
		currentKey                  string                    // The key of the entry we are currently working on.
		foundDoubles                bool                      // If set, we found double entries. In this case, we may not want to e.g. write this file.
		legacyMode                  bool                      // If set, we may switch off certain checks as we know we are importing from a legacy BibTeX file.
		EntryFieldAliasToTarget     TStringStringStringMap    // A key and field specific mapping from challenged value to winner values
		EntryFieldTargetToAliases   TStringStringStringSetMap //
		GenericFieldAliasToTarget   TStringStringMap          // A field specific mapping from challenged value to winner values
		GenericFieldTargetToAliases TStringStringSetMap       //
		NoBibFileWriting            bool                      // If set, we should not write out a Bib file for this library as entries might have been lost.
		NoEntryAliasesFileWriting   bool                      // If set, we should not write out a entry mappings file as entries might have been lost.
		NoGenericAliasesFileWriting bool                      // If set, we should not write out a generic mappings file as entries might have been lost.
		migrationMode               bool
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

	l.migrationMode = false
	l.FilesRoot = filesRoot
	l.BaseName = baseName

	l.Comments = []string{}
	l.EntryFields = TStringStringMap{}
	l.TitleIndex = TStringSetMap{}
	l.BookTitleIndex = TStringSetMap{}
	l.ISBNIndex = TStringSetMap{}
	l.BDSKFileIndex = TStringSetMap{}
	l.DOIIndex = TStringSetMap{}
	l.EntryTypes = TStringMap{}
	l.KeyAliasToKey = TStringMap{}
	l.NameAliasToName = TStringMap{}
	l.PreferredKeyAliases = TStringMap{}

	if AllowLegacy {
		// Do we really need this one? And .. it should then be KeyToAliasKey
		l.KeyToAliases = TStringSetMap{}
	}

	l.EntryFieldTargetToAliases = TStringStringStringSetMap{}
	l.GenericFieldTargetToAliases = TStringStringSetMap{}

	l.currentKey = ""
	l.foundDoubles = false
	l.EntryFieldAliasToTarget = TStringStringStringMap{}
	l.EntryFieldTargetToAliases = TStringStringStringSetMap{}
	l.GenericFieldAliasToTarget = TStringStringMap{}
	l.GenericFieldTargetToAliases = TStringStringSetMap{}

	l.NoBibFileWriting = false
	l.NoEntryAliasesFileWriting = false
	l.NoGenericAliasesFileWriting = false

	if AllowLegacy {
		l.legacyMode = false
	}
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
				l.Warning(WarningAmbiguousAlias, alias, currentTarget, target)
				l.NoEntryAliasesFileWriting = true

				return
			}
		}
	}

	// Set the actual mapping
	l.EntryFieldAliasToTarget.SetValueForStringTripleMap(entry, field, alias, target)

	// And inverse mapping
	l.EntryFieldTargetToAliases.AddValueToStringTrippleSetMap(entry, field, target, alias)
}

// Update the registration of a target over a alias for a given entry and its field.
// As we have a new target, we also need to update any other existing challenges for this field.
func (l *TBibTeXLibrary) UpdateGenericFieldAlias(field, alias, target string) {
	l.AddGenericFieldAlias(field, alias, target, true)

	for otherChallenger := range l.GenericFieldAliasToTarget[field] {
		l.AddGenericFieldAlias(field, otherChallenger, target, false)
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
				l.Warning(WarningAmbiguousAlias, alias, currentTarget, target)
				l.NoGenericAliasesFileWriting = true

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

	for otherChallenger := range l.EntryFieldAliasToTarget[entry][field] {
		l.AddEntryFieldAlias(entry, field, otherChallenger, target, false)
	}
}

// Add a preferred alias
func (l *TBibTeXLibrary) AddPreferredKeyAlias(alias string) {
	///  SAVE!? Clean!
	key, exists := l.KeyAliasToKey[alias]

	// Of course, a preferred alias must be an alias.
	if !exists {
		l.Warning(WarningPreferredAliasNotExist, alias)

		return
	}

	if !PreferredKeyAliasIsValid(alias) {
		l.Warning(WarningInvalidPreferredKeyAlias, alias, key)

		return
	}

	///  SAVE WAY!!
	l.PreferredKeyAliases[key] = alias
}

// Move the alias preference to another key
func (l *TBibTeXLibrary) moveKeyAliasPreference(alias, currentKey, key string) {
	if l.PreferredKeyAliases[currentKey] == alias && AllowLegacy {
		delete(l.PreferredKeyAliases, currentKey)
		l.PreferredKeyAliases[key] = alias
	}
}

// Add a new alias
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

// Do we still need this one?
// Add a new key alias (for use in generic read function)
func (l *TBibTeXLibrary) AddAliasForKey(alias, key string, aliasMap *TStringMap, inverseMap *TStringSetMap) {
	if alias != key {
		if _, aliasIsUsedAsKeyForSomeAlias := (*inverseMap)[alias]; aliasIsUsedAsKeyForSomeAlias {
			for oldAlias := range (*inverseMap)[alias].Set().Elements() {
				l.AddAlias(oldAlias, key, aliasMap, inverseMap, false)
				l.moveKeyAliasPreference(oldAlias, alias, key)
			}

			delete(*inverseMap, alias)
		}

		l.AddAlias(alias, key, aliasMap, inverseMap, false)
	}
}

// Add a new key alias
func (l *TBibTeXLibrary) AddKeyAlias(alias, key string) {
	l.AddAliasForKey(alias, key, &l.KeyAliasToKey, &l.KeyToAliases)
}

func (l *TBibTeXLibrary) AddOrganisationalAddress(organisationRaw, addressRaw string) {
	organisation := NormaliseTitleString(l, organisationRaw)
	address := NormaliseTitleString(l, addressRaw)

	if currentAddress, organisationHasAddress := l.OrganisationalAddresses[organisation]; organisationHasAddress {
		if currentAddress != address {
			l.Warning(WarningAmbiguousAddress, organisation, currentAddress, address)

			return
		}
	}

	// Set the actual mapping
	l.OrganisationalAddresses.SetValueForStringMap(organisation, address)
}

func (l *TBibTeXLibrary) AddSeriesISSN(name, ISSN string) {
	l.SeriesToISSN.SetValueForStringMap(name, NormaliseISSNValue(l, ISSN))
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

// Returns the size of this library.
func (l *TBibTeXLibrary) LibrarySize() int {
	return len(l.EntryTypes)
}

// Reports the size of this library.
func (l *TBibTeXLibrary) ReportLibrarySize() {
	l.Progress(ProgressLibrarySize, l.name, l.LibrarySize())
}

// ONLY needed for migration???
// Lookup the entry key and type for a given key/alias 
func (l *TBibTeXLibrary) DeAliasEntryKeyWithType(key string) (string, string, bool) {
	if entryType, isKey := l.EntryTypes[DeAliasEntryKey(key)]; isKey {
		return lookupKey, entryType, true
	} 
	
	return "", "", false
}

// Lookup the entry key for a given key/alias
func (l *TBibTeXLibrary) DeAliasEntryKey(key string) (string, bool) {
	lookupKey, isAlias := l.KeyAliasToKey[key]
	
	if isAlias {
		return lookupKey
	}
	
	return key
}

// Create a string (with newlines) with a BibTeX based representation of the provided key, while using an optional prefix for each line.
func (l *TBibTeXLibrary) EntryString(key string, prefixes ...string) string {
	fields, knownEntry := l.EntryFields[key]

	if knownEntry {
		// Combine all prefixes into one
		linePrefix := ""
		for _, prefix := range prefixes {
			linePrefix += prefix
		}

		result := ""
		// Add the type and key
		if !l.migrationMode {
			result = linePrefix + "@" + l.EntryTypes[key] + "{" + key + ",\n"
		} else if realKey, isAlias := Library.KeyAliasToKey[key]; isAlias {
			result = linePrefix + "@" + l.EntryTypes[key] + "{" + realKey + ",\n"
		} else {
			result = linePrefix + "@" + l.EntryTypes[key] + "{" + key + ",\n"
		}

		// Iterate over the fields and their values
		for field, value := range fields {
			if l.EntryAllowsForField(key, field) {
				if value != "" {
					if field == "crossref" {
						result += linePrefix + "   " + field + " = {" + NormaliseCrossrefValue(l, value) + "},\n"
					} else if field == "file" {
						result += linePrefix + "   local-url = {" + value + "},\n"
					} else {
						result += linePrefix + "   " + field + " = {" + value + "},\n"
					}
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

/*
 *
 * Checking functions
 *
 */

func (l *TBibTeXLibrary) EntryExists(entry string) bool {
	return l.EntryTypes.IsMappedString(entry)
}

func (l *TBibTeXLibrary) PreferredKeyAliasExists(alias string) bool {
	return l.PreferredKeyAliases.IsMappedString(alias)
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
// By taking the time of starting the programme as the based, we can at least generate keys from that point in time on.
// However, we should never go beyond the present time, of course.
var KeyTime time.Time // The time used for the latest generated key. Is set (see init() below) at the start of the programme.

// Generate a new key based on the KeyTime.
func (l *TBibTeXLibrary) NewKey() string {

	// We're not allowed to move into the future.
	if KeyTime.After(time.Now()) {
		///////// WAAARNING
		fmt.Println("Sleep on key generation")
		for KeyTime.After(time.Now()) {
			// Sleep ...
		}
	}

	// Create the actual new key
	key := fmt.Sprintf(
		"%s-%04d-%02d-%02d-%02d-%02d-%02d",
		KeyPrefix,
		KeyTime.Year(),
		int(KeyTime.Month()),
		KeyTime.Day(),
		KeyTime.Hour(),
		KeyTime.Minute(),
		KeyTime.Second())

	// Move to the next time for which we can generate a key.
	KeyTime = KeyTime.Add(time.Second)

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
	if !l.legacyMode && l.illegalFields.Size() > 0 {
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

////////
//////// l.currentKey ... keep in parser??
////////

// Start to record a library entry
func (l *TBibTeXLibrary) StartRecordingLibraryEntry(key, entryType string) bool {
	if l.legacyMode && !l.migrationMode {
		// Post legacy question: Do we want to use currentKey or can this be kept on the parser side??
		l.currentKey = fmt.Sprintf("%dAAAAA", uniqueID) + key
		uniqueID++
	} else if l.migrationMode {
		if mappedKey, isAlias := Library.KeyAliasToKey[key]; isAlias {
			l.currentKey = mappedKey
		} else {
			l.currentKey = key
		}
	} else {
		// Set the current key. But can't we keep that current key "inside" the parser?
		l.currentKey = key
	}

	if !BibTeXAllowedEntries.Contains(entryType) {
		l.Warning(WarningUnknownEntryType, l.currentKey, entryType)
		l.NoBibFileWriting = true
	}

	// Check if an entry with the given key already exists
	if l.EntryExists(l.currentKey) {
		// When the entry already exists, we need to report that we found doubles, as well as possibly resolve the entry type.

		if !l.migrationMode {
			l.Warning(WarningEntryAlreadyExists, l.currentKey)
			l.foundDoubles = true
		}

		// Resolve the double typing issue
		// Post legacy migration, we still need to do this, but then we will always have: key == l.currentKey
		l.EntryTypes[l.currentKey] = l.ResolveFieldValue(l.currentKey, EntryTypeField, entryType, l.EntryTypes[l.currentKey])
	} else {
		l.EntryFields[l.currentKey] = TStringMap{}
		l.EntryTypes[l.currentKey] = entryType
	}

	return true
}

// Assign a value to a field
// Post legacy ... we may want to add a key as well, when the parser maintains the current key on that side.
func (l *TBibTeXLibrary) AssignField(field, value string) bool {
	// Note: The parser for BibTeX streams is responsible for the mapping of field name alises, such as editors to editor, etc.
	// Here we only need to take care of the normalisation and processing of field values.
	// This includes the checking if e.g. files exist, and adding dblp keys as aliases.

	newValue := l.ProcessEntryFieldValue(l.currentKey, field, value)
	currentValue := l.EntryFieldValueity(l.currentKey, field)

	// Assign the new value, while, if needed, resolve it with the current value
	l.EntryFields[l.currentKey][field] = l.MaybeResolveFieldValue(l.currentKey, field, newValue, currentValue)

	// If the field is not allowed, we need to report this
	if !BibTeXAllowedFields.Contains(field) {
		l.illegalFields.Add(field)
	}

	return true
}

func (l *TBibTeXLibrary) MaybeApplyOrganisationalAddressMapping(key, field string) {
	if fieldValue := l.EntryFieldValueity(key, field); fieldValue != "" {
		if address, isMapped := l.OrganisationalAddresses[fieldValue]; isMapped {
			/////// SAFE!!
			l.EntryFields[key]["address"] = address
		}
	}
}

// Finish recording the current library entry
func (l *TBibTeXLibrary) FinishRecordingLibraryEntry() bool {
	l.MaybeApplyOrganisationalAddressMapping(l.currentKey, "organization")
	l.MaybeApplyOrganisationalAddressMapping(l.currentKey, "institution")
	l.MaybeApplyOrganisationalAddressMapping(l.currentKey, "school")
	l.MaybeApplyOrganisationalAddressMapping(l.currentKey, "publisher")

	for BDSKFileField := range BibTeXBDSKFileFields.Elements() {
		if BDSKFile := l.EntryFieldValueity(l.currentKey, BDSKFileField); BDSKFile != "" {
			if l.BDSKFileIndex[BDSKFile].Set().Contains(l.currentKey) {
				l.Warning("Double DSK file within entry %s", l.currentKey)
				l.EntryFields[l.currentKey][BDSKFileField] = ""
			} else {
				l.BDSKFileIndex.AddValueToStringSetMap(BDSKFile, l.currentKey)
			}
		}
	}

	if ISBN := l.EntryFieldValueity(l.currentKey, "isbn"); ISBN != "" {
		l.ISBNIndex.AddValueToStringSetMap(ISBN, l.currentKey)
	}

	if DOI := l.EntryFieldValueity(l.currentKey, "doi"); DOI != "" {
		l.DOIIndex.AddValueToStringSetMap(DOI, l.currentKey)
	}

	if title := l.EntryFieldValueity(l.currentKey, "title"); title != "" {
		l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(title), l.currentKey)
	}

	if bookTitle := l.EntryFieldValueity(l.currentKey, "booktitle"); bookTitle != "" {
		l.BookTitleIndex.AddValueToStringSetMap(TeXStringIndexer(bookTitle), l.currentKey)
	}

	// Check if no illegal fields were used
	// As this potentially requires interaction with the user, we only do this when we're not in silenced mode.
	if !l.legacyMode && !l.InteractionIsOff() {
		key := l.currentKey
		for field, value := range l.EntryFields[key] {
			// Check if the field is allowed for this type.
			// If not, we need to ask if it can be deleted.
			if !l.EntryAllowsForField(key, field) {
				if l.WarningYesNoQuestion(QuestionIgnore, WarningIllegalField, field, value, key, l.EntryTypes[key]) {
					delete(l.EntryFields[key], field)
				}
			}
		}
	}

	return true
}

func init() {
	// Set this at the start so we have enough keys to be generated by the time we need them.
	KeyTime = time.Now()
}
