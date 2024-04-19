package main

const (
	WarningEntryAlreadyExists = "Entry '%s' already exists"
	WarningUnknownFields      = "Unknown field(s) used: %s"
	WarningAmbiguousAlias     = "Ambiguous alias; for %s we have %s and %s"
	WarningAliasIsKey	      = "Alias %s is already known to be a key %s"
	WarningPreferredNotExist  = "Can't select a non existing alias %s as preferred alias"
	WarningAliasTargetIsAlias = "Alias %s has a target $s, which is actually an alias for $s"

)

type (
	TStringMap map[string]string

	TBiBTeXLibrary struct {
		entryFields      map[string]TStringMap
		entryType        TStringMap
		deAlias          TStringMap
		aliasedKeys      TStringSet
		preferredAliases TStringMap
		unknownFields    TStringSet
		currentKey       string
		warnOnDoubles    bool
		legacyMode       bool
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
	currentKey, exists := l.deAlias[alias]
	if exists && currentKey != key {
		l.Warning(WarningAmbiguousAlias, alias, currentKey, key)
	} else {
		aliasedKey, isAliasedKey := l.deAlias[key]

		if isAliasedKey {
			l.Warning(WarningAliasIsKey, alias, key, aliasedKey)
		} else {
			if l.aliasedKeys.Contains(alias) {
				l.Warning(WarningPreferredNotExist, alias, key)
			} else {
				l.deAlias[alias] = key
				l.aliasedKeys[key] = true
			}
		}
	}
}

func (l *TBiBTeXLibrary) AddPreferredAlias(alias string) {
	key, exists := l.deAlias[alias]
	if !exists {
		l.Warning(WarningAliasTargetIsAlias, key)
	} else {
		l.preferredAliases[key] = alias
	}
}

func (l *TBiBTeXLibrary) Initialise(reporting TReporting, warnOnDoubles bool) {
	l.entryFields = map[string]TStringMap{}
	l.entryType = TStringMap{}
	l.deAlias = TStringMap{}
	l.preferredAliases = TStringMap{}
	l.aliasedKeys = TStringSet{}
	l.currentKey = ""
	l.TReporting = reporting
	l.warnOnDoubles = warnOnDoubles
}

func (l *TBiBTeXLibrary) StartRecordingToLibrary() bool {
	l.unknownFields = TStringSet{}

	return true
}

func (l *TBiBTeXLibrary) FinishRecordingToLibrary() bool {
	if !l.legacyMode && len(l.unknownFields) > 0 {
		unknownFields := ""
		comma := ""

		// No general function to do this?
		for field, _ := range l.unknownFields {
			unknownFields += comma + field
			comma = ", "
		}

		l.Warning(WarningUnknownFields, unknownFields)
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
