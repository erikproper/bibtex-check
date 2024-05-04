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
		files            string                           // Path to root of folder with PDF files of the entries
		comments         []string                         // The comments included in a BibTeX library. These are not always "just" comments. BiBDesk uses this to store (as XML) information on e.g. static groups.
		entryFields      TStringStringMap                 // Per entry key, the fields associated to the actual entries.
		entryType        TStringMap                       // Per entry key, the type of the enty.
		deAlias          TStringMap                       // Mapping from aliases to the actual entry key.
		aliases          map[string]TStringSet            // The inverted version of deAlias.
		preferredAliases TStringMap                       // Per entry key, the preferred alias
		illegalFields    TStringSet                       // Collect the unknown fields we encounter. We can warn about these when e.g. parsing has been finished.
		currentKey       string                           // The key of the entry we are currently working on.
		foundDoubles     bool                             // If set, we found double entries. In this case, we may not want to e.g. write this file.
		legacyMode       bool                             // If set, we may switch off certain checks as we know we are importing from a legacy BibTeX file.
		challengeWinners map[string]map[string]TStringMap // A key and field specific mapping from challenged value to winner values
		TInteraction                                      // Error reporting channel
	}
)

/*
 *
 * Access abstraction -- Set functions
 *
 */

func (l *TBibTeXLibrary) SetFilePath(path string) bool {
	l.files = path

	return true
}

func (l *TBibTeXLibrary) SetEntryFieldValue(entry, field, value string) {
	l.entryFields.StringStringMapSetValue(entry, field, value)
}

/*
 *
 * Access abstraction -- Set functions
 *
 */ // Access abstraction -- Processing functions
//
//

func (l *TBibTeXLibrary) ForEachEntryTypePair(f func(string, string)) {
	l.entryType.ForEachStringPair(f)
}

func (l *TBibTeXLibrary) ForEachAliasEntryPair(f func(string, string)) {
	l.deAlias.ForEachStringPair(f)
}

/*
 *
 * Access abstraction -- Get functions
 *
 */

func (l *TBibTeXLibrary) EntryExists(entry string) bool {
	return l.entryType.StringMapped(entry)
}

func (l *TBibTeXLibrary) GetEntryFieldValue(entry, field string) string {
	return l.entryFields.StringStringMapGetValue(entry, field)
}

/*
 *
 * Access abstraction -- Checking functions
 *
 */

func (l *TBibTeXLibrary) PreferredAliasExists(alias string) bool {
	_, exists := l.preferredAliases[alias]

	return exists
}

func (l *TBibTeXLibrary) AliasExists(alias string) bool {
	_, exists := l.deAlias[alias]

	return exists
}

//////// OTHER stuff

// As the bibtex keys we generate are day and time based (down to seconds only), we need to have enough "room" to quickly generate new keys.
// By taking the time of starting the programme as the based, we can at least generate keys from that point in time on.
// However, we should never go beyond the present time, of course.
var KeyTime time.Time

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

// Add a comment to the current library
func (l *TBibTeXLibrary) AddComment(comment string) bool {
	l.comments = append(l.comments, comment)

	return true
}

// Lookup the entry key and type for a given key/alias
func (l *TBibTeXLibrary) LookupEntryWithType(key string) (string, string, bool) {
	lookupKey, isAlias := l.deAlias[key]
	if !isAlias {
		lookupKey = key
	}

	entryType, isKey := l.entryType[lookupKey]
	if isKey {
		return lookupKey, entryType, true
	} else {
		return "", "", false
	}
}

// Lookup the entry key for a given key/alias
func (l *TBibTeXLibrary) LookupEntry(key string) (string, bool) {
	lookupKey, _, isKey := l.LookupEntryWithType(key)

	return lookupKey, isKey
}

func (l *TBibTeXLibrary) registerChallengeWinner(entry, field, challenger, winner string) {
	_, isDefined := l.challengeWinners[entry]
	if !isDefined {
		l.challengeWinners[entry] = map[string]TStringMap{}
	}

	_, isDefined = l.challengeWinners[entry][field]
	if !isDefined {
		l.challengeWinners[entry][field] = TStringMap{}
	}

	l.challengeWinners[entry][field][challenger] = winner
}

func (l *TBibTeXLibrary) updateChallengeWinner(entry, field, challenger, winner string) {
	l.registerChallengeWinner(entry, field, challenger, winner)

	for otherChallenger := range l.challengeWinners[entry][field] {
		l.challengeWinners[entry][field][otherChallenger] = winner
	}
}

func (l *TBibTeXLibrary) checkChallengeWinner(entry, field, challenger, winner string) bool {
	return l.challengeWinners[entry][field][challenger] == winner
}

func (l *TBibTeXLibrary) entryString(key string) string {
	result := ""
	fields, knownEntry := l.entryFields[key]

	if knownEntry {
		result = "@" + l.entryType[key] + "{" + key + ",\n"
		for field, value := range fields {
			result += "   " + field + " = {" + value + "},\n"
		}
		result += "}\n"
	}

	return result
}

// The low level registering of the alias for a key.
// Also takes care of registering the inverse mapping.
func (l *TBibTeXLibrary) registerAlias(alias, key string) {
	l.deAlias[alias] = key

	// Also create and/or update the inverse mapping
	_, hasAliases := l.aliases[key]
	if !hasAliases && AllowLegacy {
		l.aliases[key] = TStringSetNew()
	}
	l.aliases[key].Set().Add(alias)
}

// Move the alias preference to another key
func (l *TBibTeXLibrary) moveAliasPreference(alias, currentKey, key string) {
	if l.preferredAliases[currentKey] == alias && AllowLegacy {
		delete(l.preferredAliases, currentKey)
		l.preferredAliases[key] = alias
	}
}

// Adds an alias to a key in the current library.
// If allowRemap is true then we allow for a situation where the alias is actually a (former) key.
// In the latter situation, we would need to update the aliases to that former key as well.
// Note: The present complexity is caused due to the legacy libraries. The present mapping file refers to keys that are not yet in the main library.
// Once that is solved, the checks here can be simpler:
// - Aliasses cannot be keys
// - Keys must be actual keys of entries
// - The latter check can be deferred until after (actually) reading the library
// - The latter might not always be necessary. E.g. when simply doing a "-alias" call
func (l *TBibTeXLibrary) AddKeyAlias(alias, key string, allowRemap bool) {
	// Check if the provided is already used.
	currentKey, aliasIsAlreadyAliased := l.deAlias[alias]

	// Check if the provided alias is itself not a key that is in use by an entry.
	_, aliasIsActuallyKeyToEntry := l.entryFields[alias]

	// Check if the provided alias is itself not the target of an alias mapping.
	_, aliasIsActuallyKeyForAlias := l.aliases[alias]

	// Check if the provided key is itself not an alias.
	aliasedKey, keyIsActuallyAlias := l.deAlias[key]

	if aliasIsAlreadyAliased && currentKey != key {
		if allowRemap && AllowLegacy {
			l.aliases[currentKey].Set().Delete(key)
			l.registerAlias(alias, key)
			l.moveAliasPreference(alias, currentKey, key)
		} else {
			// No ambiguous aliases allowed
			l.Warning(WarningAmbiguousAlias, alias, currentKey, key)
		}
	} else if aliasIsActuallyKeyToEntry {
		// Aliases cannot be keys of actual themselves.
		l.Warning(WarningAliasIsKey, alias)
	} else if aliasIsActuallyKeyForAlias && AllowLegacy {
		if allowRemap && AllowLegacy { // After the migration, this can only happen when merging two entries.
			for old_alias := range l.aliases[alias].Set().Elements() {
				l.registerAlias(old_alias, key)
				l.moveAliasPreference(old_alias, alias, key)
			}
			l.registerAlias(alias, key)
			delete(l.aliases, alias)
		} else {
			// Unless we allow for a remap of existing aliases, aliases cannot be keys themselves.
			l.Warning(WarningAliasIsKey, alias)
		}
	} else if keyIsActuallyAlias {
		// We cannot alias aliases.
		l.Warning(WarningAliasTargetIsAlias, alias, key, aliasedKey)
	} else {
		l.registerAlias(alias, key)
	}
}

// Add a preferred alias
func (l *TBibTeXLibrary) AddPreferredAlias(alias string) {
	key, exists := l.deAlias[alias]

	// Of course, a preferred alias must be an alias.
	if !exists {
		l.Warning(WarningPreferredNotExist, key)
	} else {
		l.preferredAliases[key] = alias
	}
}

// Initialise a library
func (l *TBibTeXLibrary) Initialise(reporting TInteraction) {
	l.files = ""
	l.comments = []string{}
	l.entryFields = map[string]TStringMap{}
	l.entryType = TStringMap{}
	l.deAlias = TStringMap{}
	l.preferredAliases = TStringMap{}
	l.aliases = map[string]TStringSet{}
	l.currentKey = ""
	l.TInteraction = reporting
	l.foundDoubles = false
	l.challengeWinners = map[string]map[string]TStringMap{}
	if AllowLegacy {
		l.legacyMode = false
	}
}

// Start recording to the library
func (l *TBibTeXLibrary) StartRecordingToLibrary() bool {
	l.illegalFields = TStringSetNew()

	return true
}

// Finish recording to the library
func (l *TBibTeXLibrary) FinishRecordingToLibrary() bool {
	if !l.legacyMode && l.illegalFields.Size() > 0 {
		l.Warning(WarningUnknownFields, l.illegalFields.String())
	}

	return true
}

// Report back if doubles were found
func (l *TBibTeXLibrary) FoundDoubles() bool {
	return l.foundDoubles
}

var uniqueID int

// Start to record a library entry
func (l *TBibTeXLibrary) StartRecordingLibraryEntry(key, entryType string) bool {
	if l.legacyMode {
		// Post legacy question: Do we want to use currentKey or can this be kept on the parser side??
		l.currentKey = fmt.Sprintf("%dAAAAA", uniqueID) + key
		uniqueID++
	} else {
		l.currentKey = key
	}

	// Check if an entry with the given key already exists
	_, exists := l.entryType[l.currentKey]
	if exists {
		// When the entry already exists, we need to report that we found doubles, as well as possibly resolve the entry type.
		l.Warning(WarningEntryAlreadyExists, l.currentKey)
		l.foundDoubles = true
		l.entryType[l.currentKey] = l.ResolveFieldValue(l.currentKey, EntryTypeField, entryType, l.entryType[key])
	} else {
		l.entryFields[l.currentKey] = TStringMap{}
		l.entryType[l.currentKey] = entryType
	}

	return true
}

// Assign a value to a field
func (l *TBibTeXLibrary) AssignField(field, value string) bool {
	// Note: The parser for BibTeX streams is responsible for the mapping of field name aliases.
	// Here we need to take care of the normalisation and processing of field values.
	// This includes the checking if e.g. files exist, and adding dblp keys as aliases.
	newValue := l.ProcessFieldValue(field, value)

	// If the new value is empty, we assign nothing.
	if newValue != "" {
		currentValue, alreadyHasValue := l.entryFields[l.currentKey][field]

		// If the field already has a value that is different from the new value, we need to resolve this.
		if alreadyHasValue && newValue != currentValue {
			l.entryFields[l.currentKey][field] = l.ResolveFieldValue(l.currentKey, field, newValue, currentValue)
		} else {
			l.entryFields[l.currentKey][field] = newValue
		}
	}

	// If the field is not allowed, we need to report this
	if !BibTeXAllowedFields.Contains(field) {
		l.illegalFields.Add(field)
	}

	return true
}

// Finish recording the current library entry
func (l *TBibTeXLibrary) FinishRecordingLibraryEntry() bool {
	if !l.legacyMode {
		// Check if no illegal fields were used
		key := l.currentKey
		entryType := l.entryType[key]
		for field, value := range l.entryFields[key] {
			if !BibTeXAllowedEntryFields[entryType].Set().Contains(field) {
				if l.WarningBoolQuestion(QuestionIgnore, WarningIllegalField, field, value, key, entryType) {
					delete(l.entryFields[key], field)
				}
			}
		}
	}

	return true
}

func init() {
	KeyTime = time.Now()
}
