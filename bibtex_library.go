//
// Module: bibtex_library
//
// This module is concerned with the storage of BibTeX libraties
//
// Creator: Henderik A. Proper (erikproper@fastmail.com)
//
// Version of: 24.04.2024
//

package main

import (
	"fmt"
	"time"
)

// Warnings regarding the consistency of the libraries
const (
	WarningEntryAlreadyExists = "Entry '%s' already exists"
	WarningUnknownFields      = "Unknown field(s) used: %s"
	WarningAmbiguousAlias     = "Ambiguous alias; for %s we have %s and %s"
	WarningAliasIsKey         = "Alias %s is already known to be a key"
	WarningPreferredNotExist  = "Can't select a non existing alias %s as preferred alias"
	WarningAliasTargetIsAlias = "Alias %s has a target $s, which is actually an alias for $s"
	WarningBadAlias           = "Alias %s for %s does not comply to the rules"
	WarningIllegalField       = "Field \"%s\", with value \"%s\", is not allowed for entry %s of type %s"
	QuestionIgnore            = "Ignore this field?"
)

// As the bibtex keys we generate are day and time based (down to seconds only), we need to have enough "room" to quickly generate new keys.
// By taking the time of starting the app as the based, we can at least generate keys from that point in time on.
// However, we should never go beyond the present time, of course.
var KeyTime time.Time

type (
	// We use several mappings from strings to strings
	TStringMap map[string]string

	// The type for BibTeXLibraries
	TBibTeXLibrary struct {
		files            string                // Path to root of folder with PDF files of the entries
		comments         []string              // The comments included in a BibTeX library. These are not always "just" comments. BiBDesk uses this to store (as XML) information on e.g. static groups.
		entryFields      map[string]TStringMap // Per entry key, the fields associated to the actual entries.
		entryType        TStringMap            // Per entry key, the type of the enty.
		deAlias          TStringMap            // Mapping from aliases to the actual entry key.
		preferredAliases TStringMap            // Per entry key, the preferred alias
		illegalFields    TStringSet            // Collect the unknown fields we encounter. We can warn about these when e.g. parsing has been finished.
		currentKey       string                // The key of the entry we are currently working on.
		foundDoubles     bool                  // If set, we found double entries. In this case, we may not want to e.g. write this file.
		legacyMode       bool                  // If set, we may switch off certain checks as we know we are importing from a legacy BibTeX file.
		TInteraction                           // Error reporting channel
	}
)

// Checks if a given alias fits the desired format of [a-z][a-z]*[0-9][0-9][0-9][0-9][a-z][a-z,0-9]*
// Examples: gordijn2002e3value, overbeek2010matchmaking, ...
func CheckPreferredAlias(alias string) bool {
	pre_year := 0
	in_year := 0
	post_year := 0

	for _, character := range alias {
		switch {
		case pre_year == 0: // [a-z]
			if 'a' <= character && character <= 'z' {
				pre_year++
			} else {
				return false
			}

		case pre_year > 0 && in_year == 0: // [a-z]*
			if 'a' <= character && character <= 'z' {
				pre_year++
			} else if '0' <= character && character <= '9' {
				in_year++
			} else {
				return false
			}

		case 1 <= in_year && in_year < 4: // [0-9][0-9][0-9]
			if '0' <= character && character <= '9' {
				in_year++
			} else {
				return false
			}

		case in_year == 4 && post_year == 0: // [a-z]
			if 'a' <= character && character <= 'z' {
				post_year++
			} else {
				return false
			}

		case in_year == 4 && post_year > 0: // [a-z,0-9]*
			if ('a' <= character && character <= 'z') ||
				('0' <= character && character <= '9') {
				post_year++
			} else {
				return false
			}

		default:
			return false
		}
	}

	return post_year > 0
}

func (l *TBibTeXLibrary) SetFilePath(path string) bool {
	l.files = path

	return true
}

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

// Add an alias to a key to the current library.
func (l *TBibTeXLibrary) AddKeyAlias(alias, key string) {
	// Check if the provided is already used.
	currentKey, aliasIsAlreadyAliased := l.deAlias[alias]

	// Check if the provided alias is itself not a key that is in use.
	_, aliasIsActuallyKey := l.entryFields[alias]

	// Check if the provided key is itself not an alias.
	aliasedKey, keyIsActuallyAlias := l.deAlias[key]

	if aliasIsAlreadyAliased && currentKey != key {
		// No ambiguous aliases allowed.
		l.Warning(WarningAmbiguousAlias, alias, currentKey, key)
	} else if aliasIsActuallyKey {
		// Aliases cannot be keys themsleves.
		l.Warning(WarningAliasIsKey, alias)
	} else if keyIsActuallyAlias {
		// We cannot alias aliases.
		l.Warning(WarningAliasTargetIsAlias, alias, key, aliasedKey)
	} else {
		l.deAlias[alias] = key
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
	l.currentKey = ""
	l.TInteraction = reporting
	l.foundDoubles = false
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

// Start to record a library entry
func (l *TBibTeXLibrary) StartRecordingLibraryEntry(key, entryType string) bool {
	l.currentKey = key

	// Check if an entry with the given key already exists
	_, exists := l.entryType[l.currentKey]
	if exists {
		// When the entry already exists, we need to report that we found doubles, as well as possibly resolve the entry type.
		if !l.legacyMode {
			l.Warning(WarningEntryAlreadyExists, l.currentKey)
			l.foundDoubles = true
		}
		l.entryType[l.currentKey] = l.ResolveFieldValue(EntryTypeField, entryType, l.entryType[key])
	} else {
		l.entryFields[l.currentKey] = TStringMap{}
		l.entryType[l.currentKey] = entryType
	}

	return true
}

// Assign a value to a field
func (l *TBibTeXLibrary) AssignField(field, value string) bool {
	// Note: The parser for BibTeX streams is responsible for the mapping of field names.
	// Here we do need to take care of the normalisation of the field values.
	// This includes e.g.:
	//   Renaming of author/editor names to their desired format.
	//   Rewriting titles with proper protection and TeX accents:
	//     {Hello WORLD {\" a}} ==> {Hello {WORLD} {\"a}}
	newValue := l.NormaliseFieldValue(field, value)

	// If the new value is empty, we assign nothing.
	if newValue != "" {
		currentValue, alreadyHasValue := l.entryFields[l.currentKey][field]
		// If the field already has a value that is different from the new value, we need to resolve this.
		if alreadyHasValue && newValue != currentValue {
			l.entryFields[l.currentKey][field] = l.ResolveFieldValue(field, newValue, currentValue)
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
