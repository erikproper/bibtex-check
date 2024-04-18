package main

const (
	WarningEntryAlreadyExists = "Entry '%s' already exists"
	WarningUnknownFields      = "Unknown field(s) used: %s"
)

type (
	TStringMap map[string]string
	TStringSet map[string]bool

	TBiBTeXLibrary struct {
		entryFields   map[string]TStringMap
		entryType     TStringMap
		unknownFields TStringSet
		currentKey    string
		warnOnDoubles bool
		TReporting    // Error reporting channel
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

func EntryAlias(entry, alias string) {
	BiBTeXEntryNameMap[entry] = alias
}

func FieldAlias(entry, alias string) {
	BiBTeXEntryNameMap[entry] = alias
}

func (l *TBiBTeXLibrary) NewLibrary(reporting TReporting, warnOnDoubles bool) {
	l.entryFields = map[string]TStringMap{}
	l.entryType = TStringMap{}
	l.currentKey = ""
	l.TReporting = reporting
	l.warnOnDoubles = warnOnDoubles
}

func (l *TBiBTeXLibrary) StartRecordingToLibrary() bool {
	l.unknownFields = TStringSet{}

	return true
}

func (l *TBiBTeXLibrary) FinishRecordingToLibrary() bool {
	if len(l.unknownFields) > 0 {
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

	_, exists := l.entryType[l.currentKey]

	if exists {
		if l.warnOnDoubles {
			l.Warning(WarningEntryAlreadyExists, l.currentKey)
		}
	} else {
		l.entryFields[l.currentKey] = TStringMap{}
	}

	l.entryType[l.currentKey] = entryType

	return true
}

func (l *TBiBTeXLibrary) AssignField(field, value string) bool {
	l.entryFields[l.currentKey][field] = value

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
	///      if exists new_field = FieldAliasesMap[field] then
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
