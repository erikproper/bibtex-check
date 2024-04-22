package main

import (
	"fmt"
	"strings"
	"time"
)

const (
	KeyPrefix                 = "EP"
	WarningEntryAlreadyExists = "Entry '%s' already exists"
	WarningUnknownFields      = "Unknown field(s) used: %s"
	WarningAmbiguousAlias     = "Ambiguous alias; for %s we have %s and %s"
	WarningAliasIsKey         = "Alias %s is already known to be a key"
	WarningPreferredNotExist  = "Can't select a non existing alias %s as preferred alias"
	WarningAliasTargetIsAlias = "Alias %s has a target $s, which is actually an alias for $s"
	WarningBadAlias 		  = "Alias %s for %s does not comply to the rules"
)

type (
	TStringMap map[string]string

	TBiBTeXLibrary struct {
		comments         []string
		entryFields      map[string]TStringMap
		entryType        TStringMap
		deAlias          TStringMap
		preferredAliases TStringMap
		unknownFields    TStringSet
		currentKey       string
		warnOnDoubles    bool
		legacyMode       bool
		lastNewKey       string
		TReporting       // Error reporting channel
	}
)

var (
	BiBTeXAllowedEntryFields map[string]TStringSet
	BiBTeXAllowedFields,
	BiBTeXAllowedEntries TStringSet
	BiBTeXFieldNameMap,
	BiBTeXEntryNameMap,
	BiBTeXDefaultStrings TStringMap
)

// / UP!!
func CheckPreferredAlias(alias string) bool {
	pre_year := 0
	in_year := 0
	post_year := 0

	for _, character := range alias {
		switch {
		case pre_year == 0:
			if 'a' <= character && character <= 'z' {
				pre_year++
			} else {
				return false
			}

		case pre_year > 0 && in_year == 0:
			if 'a' <= character && character <= 'z' {
				pre_year++
			} else if '0' <= character && character <= '9' {
				in_year++
			} else {
				return false
			}

		case 1 <= in_year && in_year < 4:
			if '0' <= character && character <= '9' {
				in_year++
			} else {
				return false
			}

		case in_year == 4 && post_year == 0:
			if 'a' <= character && character <= 'z' {
				post_year++
			} else {
				return false
			}

		case in_year == 4 && post_year > 0:
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

func (l *TBiBTeXLibrary) CheckPreferredAliases() {
	for key, alias := range l.preferredAliases {
		if !CheckPreferredAlias(alias) {
			l.Warning(WarningBadAlias, alias, key)
		}
	}

	for alias, key := range l.deAlias {
		if l.preferredAliases[key] == "" {
			if CheckPreferredAlias(alias) {
				l.AddPreferredAlias(alias)
			} else {
				loweredAlias := strings.ToLower(alias)
				if l.deAlias[loweredAlias] == "" && loweredAlias != key && CheckPreferredAlias(loweredAlias) {
					l.AddKeyAlias(loweredAlias, key)
					l.AddPreferredAlias(loweredAlias)
				}
			}
		}
	}
}

func (l *TBiBTeXLibrary) NewKey() string {
	key := l.lastNewKey
	for key == l.lastNewKey {
		now := time.Now()
		key = fmt.Sprintf(
			"%s-%04d-%02d-%02d-%02d-%02d-%02d",
			KeyPrefix,
			now.Year(),
			int(now.Month()),
			now.Day(),
			now.Hour(),
			now.Minute(),
			now.Second())
	}

	l.lastNewKey = key

	return key
}

func (l *TBiBTeXLibrary) AddComment(comment string) bool {
	l.comments = append(l.comments, comment)

	return true
}

func AllowedEntryFields(entry string, fields ...string) {
	if BiBTeXAllowedEntries == nil {
		BiBTeXAllowedEntries = TStringSet{}
	}
	BiBTeXAllowedEntries[entry] = true

	if BiBTeXAllowedFields == nil {
		BiBTeXAllowedFields = TStringSet{}
	}

	if BiBTeXAllowedEntryFields == nil {
		BiBTeXAllowedEntryFields = map[string]TStringSet{}
	}

	for _, field := range fields {
		BiBTeXAllowedFields[field] = true

		if BiBTeXAllowedEntryFields[entry] == nil {
			BiBTeXAllowedEntryFields[entry] = TStringSet{}
		}
		BiBTeXAllowedEntryFields[entry][field] = true
	}
}

func AllowedFields(fields ...string) {
	for entry, allowed := range BiBTeXAllowedEntries {
		if allowed {
			AllowedEntryFields(entry, fields...)
		}
	}
}

func EntryTypeAlias(entry, alias string) {
	BiBTeXEntryNameMap[entry] = alias
}

func FieldTypeAlias(entry, alias string) {
	BiBTeXEntryNameMap[entry] = alias
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

func (l *TBiBTeXLibrary) Initialise(reporting TReporting, warnOnDoubles bool) {
	l.comments = []string{}
	l.entryFields = map[string]TStringMap{}
	l.entryType = TStringMap{}
	l.deAlias = TStringMap{}
	l.preferredAliases = TStringMap{}
	l.currentKey = ""
	l.TReporting = reporting
	l.lastNewKey = ""
	l.warnOnDoubles = warnOnDoubles
}

func (l *TBiBTeXLibrary) StartRecordingToLibrary() bool {
	l.unknownFields = TStringSet{}

	return true
}

func (l *TBiBTeXLibrary) FinishRecordingToLibrary() bool {
	if !l.legacyMode && len(l.unknownFields) > 0 {
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
		allowed, _ := BiBTeXAllowedFields[field]
		if !allowed {
			l.unknownFields[field] = true
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
