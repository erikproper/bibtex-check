//
// Module: bibtexlibrary
//
// This module is concerned with the storage of BiBTeX libraties
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
)

// As the bibtex keys we generate are day and time based (down to seconds only), we need to have enough "room" to quickly generate new keys.
// By taking the time of starting the app as the based, we can at least generate keys from that point in time on.
// However, we should never go beyond the present time, of course.
var KeyTime time.Time

type (
	// We use several mappings from strings to strings
	TStringMap map[string]string

	TBiBTeXLibrary struct {
		comments         []string              // The comments included in a BiBTeX library. These are not always "just" comments. BiBDesk uses this to store (as XML) information on e.g. static groups.
		entryFields      map[string]TStringMap // Per entry key, the fields associated to the actual entries.
		entryType        TStringMap            // Per entry key, the type of the enty.
		deAlias          TStringMap            // Mapping from aliases to the actual entry key.
		preferredAliases TStringMap            // Per entry key, the preferred alias
		unknownFields    TStringSet            // Collect the unknown fields we encounter. We can warn about these when e.g. parsing has been finished.
		currentKey       string                // The key of the entry we are currently working on.
		warnOnDoubles    bool                  // If set, we need to warn when we try to record an entry which already exists.
		legacyMode       bool                  // If set, we may switch off certain checks as we know we are importing from a legacy BiBTeX file.
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

// Generate a new key based on the KeyTime.
func (l *TBiBTeXLibrary) NewKey() string {

	// We're not allowed to move into the future.
	if KeyTime.After(time.Now())
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

func (l *TBiBTeXLibrary) AddComment(comment string) bool {
	l.comments = append(l.comments, comment)

	return true
}

func (l *TBiBTeXLibrary) AddKeyAlias(alias, key string) {
	currentKey, aliasIsAlreadyAliased := l.deAlias[alias]
	_, aliasIsActuallyKey := l.entryFields[alias]
	aliasedKey, keyIsActuallyAlias := l.deAlias[key]

	if aliasIsAlreadyAliased && currentKey != key {
		l.Warning(WarningAmbiguousAlias, alias, currentKey, key)
	} else if aliasIsActuallyKey {
		l.Warning(WarningAliasIsKey, alias)
	} else if keyIsActuallyAlias {
		l.Warning(WarningAliasTargetIsAlias, alias, key, aliasedKey)
	} else {
		l.deAlias[alias] = key
	}
}

func (l *TBiBTeXLibrary) AddPreferredAlias(alias string) {
	key, exists := l.deAlias[alias]
	if !exists {
		l.Warning(WarningPreferredNotExist, key)
	} else {
		l.preferredAliases[key] = alias
	}
}

func (l *TBiBTeXLibrary) Initialise(reporting TInteraction, warnOnDoubles bool) {
	l.comments = []string{}
	l.entryFields = map[string]TStringMap{}
	l.entryType = TStringMap{}
	l.deAlias = TStringMap{}
	l.preferredAliases = TStringMap{}
	l.currentKey = ""
	l.TInteraction = reporting
	l.warnOnDoubles = warnOnDoubles
}

func (l *TBiBTeXLibrary) StartRecordingToLibrary() bool {
	l.unknownFields = TStringSetNew()

	return true
}

func (l *TBiBTeXLibrary) FinishRecordingToLibrary() bool {
	//////// This should be a function in itself
	//////// Legacy model set by what???
	if !l.legacyMode && l.unknownFields.Size() > 0 {
		l.Warning(WarningUnknownFields, l.unknownFields.String())
	}

	return true
}

func (l *TBiBTeXLibrary) StartRecordingLibraryEntry(key, entryType string) bool {
	l.currentKey = key

	alias, aliased := l.deAlias[l.currentKey]
	if aliased {
		l.currentKey = alias
		//		fmt.Println("Mapped: ", key, "->", alias)
	}

	_, exists := l.entryType[l.currentKey]

	//	computedEntryType := entryType

	if exists {
		if l.warnOnDoubles {
			l.Warning(WarningEntryAlreadyExists, l.currentKey)
		}

		//		if l.legacyMode {
		//			if l.entryType[l.currentKey] != entryType {
		//				_, known := Library.entryType[l.currentKey]
		//				if !known {
		//					fmt.Println("KK", l.currentKey, l.entryType[l.currentKey])
		//					fmt.Println("KK", l.currentKey, entryType)
		//					//				fmt.Println("Ambiguous types. From:", l.entryType[l.currentKey], "to: ", entryType)
		//					//				fmt.Println(l.entryFields[l.currentKey])
		//					//				fmt.Println()
		//				} else {
		//					computedEntryType = Library.entryType[l.currentKey]
		//					fmt.Println("DD", computedEntryType, "from", entryType, "of", key)
		//				}
		//			}
		//		}
	} else {
		l.entryFields[l.currentKey] = TStringMap{}
	}

	//	l.entryType[l.currentKey] = computedEntryType

	l.entryType[l.currentKey] = entryType

	return true
}

func (l *TBiBTeXLibrary) AssignField(field, value string) bool {
	//
	// Needed for the import of the legacy files.
	//
	//	if l.legacyMode && field == "file" {
	//		fmt.Println("CHECK FILE!", value)
	//	}

	//currentValue, hasValue := l.entryFields[l.currentKey][field]
	//	if hasValue && currentValue != value {
	//		fmt.Println("Changed value for", l.currentKey, "field", field)
	//		fmt.Println(" from:", currentValue)
	//		fmt.Println("   to:", value)
	//	}
	l.entryFields[l.currentKey][field] = value

	if field == "dblp" && !l.legacyMode {
		l.AddKeyAlias("DBLP:"+value, l.currentKey)
	}

	return true
}

func (l *TBiBTeXLibrary) FinishRecordingLibraryEntry() bool {
	// This is where we need to do a lot of checks ...

	// Make more abstract by adding field to set.

	for field, _ := range l.entryFields[l.currentKey] {
		if !BiBTeXAllowedFields.Contains(field) {
			l.unknownFields.Add(field)
		}
	}

	/// for each field used in entry:
	///   if unknown then
	///      if exists new_field = FieldTypeAliasesMap[field] then
	///         if new_field already has value then
	///            ask
	///         else
	///            rename
	///         fi
	///      else
	///         Add to UnknownFields list for current stream
	///      fi
	///
	///   if resulting field is AllowedFields, but not in AllowedFields list for this type
	///      then warning
	///

	return true
}

func init() {
	KeyTime = time.Now()
}
