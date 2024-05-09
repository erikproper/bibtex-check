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
		name                string                 // Name of the library
		FilesRoot           string                 // Path to folder with library related files
		BibFilePath         string                 // Relative path to the BibTeX file
		KeyAliasesFilePath  string                 // Relative path to the entry aliases file
		ChallengesFilePath  string                 // Relative path to the challenges file
		NameAliasesFilePath string                 // Relative path to the name aliases file
		Comments            []string               // The Comments included in a BibTeX library. These are not always "just" Comments. BiBDesk uses this to store (as XML) information on e.g. static groups.
		EntryFields         TStringStringMap       // Per entry key, the fields associated to the actual entries.
		EntryTypes          TStringMap             // Per entry key, the type of the enty.
		KeyAliasToKey       TStringMap             // Mapping from key aliases to the actual entry key.
		NameAliasToName     TStringMap             // Mapping from name aliases to the actual name.
		EntryToAliases      TStringSetMap          // The inverted version of AliasToEntry.
		PreferredKeyAliases TStringMap             // Per entry key, the preferred alias
		illegalFields       TStringSet             // Collect the unknown fields we encounter. We can warn about these when e.g. parsing has been finished.
		currentKey          string                 // The key of the entry we are currently working on.
		foundDoubles        bool                   // If set, we found double entries. In this case, we may not want to e.g. write this file.
		legacyMode          bool                   // If set, we may switch off certain checks as we know we are importing from a legacy BibTeX file.
		ChallengeWinners    TStringStringStringMap // A key and field specific mapping from challenged value to winner values
		TInteraction                               // Error reporting channel
		TBibTeXStream                              // BibTeX parser
		TBibTeXTeX
	}
)

// Initialise a library
func (l *TBibTeXLibrary) Initialise(reporting TInteraction, name, filesRoot string) {
	l.TInteraction = reporting
	l.name = name
	l.Progress(ProgressInitialiseLibrary, l.name)

	l.TBibTeXStream = TBibTeXStream{}
	l.TBibTeXStream.Initialise(reporting, l)

	l.TBibTeXTeX = TBibTeXTeX{}

	//// Abstraction
	l.TBibTeXTeX.library = l

	l.FilesRoot = filesRoot
	l.BibFilePath = ""
	l.KeyAliasesFilePath = ""
	l.NameAliasesFilePath = ""
	l.ChallengesFilePath = ""

	l.Comments = []string{}
	l.EntryFields = TStringStringMap{}
	l.EntryTypes = TStringMap{}
	l.KeyAliasToKey = TStringMap{}
	l.NameAliasToName = TStringMap{}
	l.PreferredKeyAliases = TStringMap{}

	if AllowLegacy {
		// Do we really need this one? And .. it should then be KeyToAliasKey
		l.EntryToAliases = TStringSetMap{}
	}

	l.currentKey = ""
	l.foundDoubles = false
	l.ChallengeWinners = TStringStringStringMap{}

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

// Initial registration of a winner over a challenger for a given entry and its field.
func (l *TBibTeXLibrary) RegisterChallengeWinner(entry, field, challenger, winner string) {
	// Only register challenger/winner pairs when both are non-empty
	if winner != challenger {
		l.ChallengeWinners.SetValueForStringTripleMap(entry, field, challenger, winner)
	}
}

// Update the registration of a winner over a challenger for a given entry and its field.
// As we have a new winner, we also need to update any other existing challenges for this field.
func (l *TBibTeXLibrary) UpdateChallengeWinner(entry, field, challenger, winner string) {
	l.RegisterChallengeWinner(entry, field, challenger, winner)

	for otherChallenger := range l.ChallengeWinners[entry][field] {
		l.RegisterChallengeWinner(entry, field, otherChallenger, winner)
	}
}

// The low level registering of the alias for a key.
// Also takes care of registering the inverse mapping.
func (l *TBibTeXLibrary) registerKeyAlias(alias, key string) {
	l.KeyAliasToKey[alias] = key

	// Also create and/or update the inverse mapping
	_, hasAliases := l.EntryToAliases[key]
	if !hasAliases && AllowLegacy {
		l.EntryToAliases[key] = TStringSetNew()
	}
	l.EntryToAliases[key].Set().Add(alias)
}

// Move the alias preference to another key
func (l *TBibTeXLibrary) moveKeyAliasPreference(alias, currentKey, key string) {
	if l.PreferredKeyAliases[currentKey] == alias && AllowLegacy {
		delete(l.PreferredKeyAliases, currentKey)
		l.PreferredKeyAliases[key] = alias
	}
}

// Adds an alias to a key in the current library.
// If allowRemap is true then we allow for a situation where the alias is actually a (former) key.
// In the latter situation, we would need to update the EntryToAliases to that former key as well.
// Note: The present complexity is caused due to the legacy libraries. The present mapping file refers to keys that are not yet in the main library.
// Once that is solved, the checks here can be simpler:
// - Aliasses cannot be keys
// - Keys must be actual keys of entries
// - The latter check can be deferred until after (actually) reading the library
// - The latter might not always be necessary. E.g. when simply doing a "-alias" call
func (l *TBibTeXLibrary) AddKeyAlias(alias, key string, allowRemap bool) {
	// Check if the provided is already used.
	currentKey, aliasIsAlreadyAliased := l.KeyAliasToKey[alias]

	// Check if the provided alias is itself not a key that is in use by an entry.
	_, aliasIsActuallyKeyToEntry := l.EntryFields[alias]

	// Check if the provided alias is itself not the target of an alias mapping.
	_, aliasIsActuallyKeyForAlias := l.EntryToAliases[alias]

	// Check if the provided key is itself not an alias.
	aliasedKey, keyIsActuallyAlias := l.KeyAliasToKey[key]

	if aliasIsAlreadyAliased && currentKey != key {
		if allowRemap && AllowLegacy {
			l.EntryToAliases[currentKey].Set().Delete(key)
			l.registerKeyAlias(alias, key)
			l.moveKeyAliasPreference(alias, currentKey, key)
		} else {
			// No ambiguous EntryToAliases allowed
			l.Warning(WarningAmbiguousAlias, alias, currentKey, key)
		}
	} else if aliasIsActuallyKeyToEntry {
		// Aliases cannot be keys of actual themselves.
		l.Warning(WarningAliasIsKey, alias)
	} else if aliasIsActuallyKeyForAlias && AllowLegacy {
		if allowRemap && AllowLegacy { // After the migration, this can only happen when merging two entries.
			for old_alias := range l.EntryToAliases[alias].Set().Elements() {
				l.registerKeyAlias(old_alias, key)
				l.moveKeyAliasPreference(old_alias, alias, key)
			}
			l.registerKeyAlias(alias, key)
			delete(l.EntryToAliases, alias)
		} else {
			// Unless we allow for a remap of existing EntryToAliases, EntryToAliases cannot be keys themselves.
			l.Warning(WarningAliasIsKey, alias)
		}
	} else if keyIsActuallyAlias {
		// We cannot alias EntryToAliases.
		l.Warning(WarningAliasTargetIsAlias, alias, key, aliasedKey)
	} else {
		l.registerKeyAlias(alias, key)
	}
}

// Add a preferred alias
func (l *TBibTeXLibrary) AddPreferredKeyAlias(alias string) {
	key, exists := l.KeyAliasToKey[alias]

	// Of course, a preferred alias must be an alias.
	if !exists {
		l.Warning(WarningPreferredNotExist, key)
	} else {
		l.PreferredKeyAliases[key] = alias
	}
}

// Register a new name (of an author/editor) alias
func (l *TBibTeXLibrary) RegisterAliasForName(alias, name string) {
	l.NameAliasToName.SetValueForStringMap(alias, name)
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
	return len(Library.EntryTypes)
}

// Reports the size of this library.
func (l *TBibTeXLibrary) ReportLibrarySize() {
	l.Progress(ProgressLibrarySize, l.name, l.LibrarySize())
}

// Normalise the name of a person based on the aliases
func (l *TBibTeXLibrary) NormalisePersonName(name string) string {
	if normalised, isMapped := l.NameAliasToName[name]; isMapped {
		return normalised
	} else {
		return name
	}
}

// Lookup the entry key and type for a given key/alias
func (l *TBibTeXLibrary) LookupEntryWithType(key string) (string, string, bool) {
	lookupKey, isAlias := l.KeyAliasToKey[key]
	if !isAlias {
		lookupKey = key
	}

	EntryTypes, isKey := l.EntryTypes[lookupKey]
	if isKey {
		return lookupKey, EntryTypes, true
	} else {
		return "", "", false
	}
}

// Lookup the entry key for a given key/alias
func (l *TBibTeXLibrary) LookupEntry(key string) (string, bool) {
	lookupKey, _, isKey := l.LookupEntryWithType(key)

	return lookupKey, isKey
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

		// Add the type and key
		result := linePrefix + "@" + l.EntryTypes[key] + "{" + key + ",\n"

		// Iterate over the fields and their values
		for field, value := range fields {
			if value != "" {
				result += linePrefix + "   " + field + " = {" + value + "},\n"
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
func (l *TBibTeXLibrary) CheckChallengeWinner(entry, field, challenger, winner string) bool {
	return l.ChallengeWinners.GetValueityFromStringTripleMap(entry, field, challenger) == winner
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

// Start to record a library entry
func (l *TBibTeXLibrary) StartRecordingLibraryEntry(key, entryType string) bool {
	if l.legacyMode {
		// Post legacy question: Do we want to use currentKey or can this be kept on the parser side??
		l.currentKey = fmt.Sprintf("%dAAAAA", uniqueID) + key
		uniqueID++
	} else {
		// Set the current key. But can't we keep that current key "inside" the parser?
		l.currentKey = key
	}

	// Check if an entry with the given key already exists
	if l.EntryExists(l.currentKey) {
		// When the entry already exists, we need to report that we found doubles, as well as possibly resolve the entry type.
		l.Warning(WarningEntryAlreadyExists, l.currentKey)
		l.foundDoubles = true
		// Resolve the double typing issue
		// Post legacy migration, we still need to do this, but then we will always have: key == l.currentKey
		l.EntryTypes[l.currentKey] = l.ResolveFieldValue(l.currentKey, EntryTypeField, entryType, l.EntryTypes[key])
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

	newValue := l.ProcessFieldValue(field, value)
	currentValue := l.EntryFieldValueity(l.currentKey, field)

	// Assign the new value, while, if needed, resolve it with the current value
	l.EntryFields[l.currentKey][field] = l.MaybeResolveFieldValue(l.currentKey, field, newValue, currentValue)

	// If the field is not allowed, we need to report this
	if !BibTeXAllowedFields.Contains(field) {
		l.illegalFields.Add(field)
	}

	return true
}

// Finish recording the current library entry
func (l *TBibTeXLibrary) FinishRecordingLibraryEntry() bool {
	// Check if no illegal fields were used
	// As this potentially requires interaction with the user, we only do this when we're not in silenced mode.
	if !l.legacyMode && !l.IsSilenced() {
		key := l.currentKey
		for field, value := range l.EntryFields[key] {
			// Check if the field is allowed for this type.
			// If not, we need to ask if it can be deleted.
			if !l.EntryAllowsForField(key, field) {
				if l.WarningBoolQuestion(QuestionIgnore, WarningIllegalField, field, value, key, l.EntryTypes[key]) {
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
