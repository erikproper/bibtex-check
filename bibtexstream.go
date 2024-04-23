package main

import "strings"

const (
	CharacterClass             = "Character"
	EntryBodyClass             = "EntryBody"
	FieldNameClass             = "FieldName"
	EntryTypeClass             = "EntryType"
	FieldValueClass            = "FieldValue"
	ErrorMissing               = "Missing"
	ErrorMissingCharacter      = ErrorMissing + " " + CharacterClass + "'%s', found '%s'"
	ErrorMissingEntryBody      = ErrorMissing + " " + EntryBodyClass
	ErrorMissingEntryType      = ErrorMissing + " " + EntryTypeClass
	ErrorMissingFieldName      = ErrorMissing + " " + FieldNameClass
	ErrorMissingFieldValue     = ErrorMissing + " " + FieldValueClass
	ErrorOpeningFile           = "Could not open file '%s'"
	ErrorUnknownString         = "Unknown string '%s' referenced"
	WarningSkippingToNextEntry = "Skipping to next entry"
	TeXMode                    = true
	EntryStartCharacter        = '@'
	BeginGroupCharacter        = '{'
	EndGroupCharacter          = '}'
	DoubleQuotesCharacter      = '"'
	AssignmentCharacter        = '='
	AdditionCharacter          = '#'
	PercentCharacter           = '%'
	CommaCharacter             = ','
	EscapeCharacter            = '\\'
	CommentEntryType           = "comment"
	PreambleEntryType          = "preamble"
	StringEntryType            = "string"
)

type (
	TMapField     func(string, string) bool
	TBiBTeXStream struct {
		TCharacterStream                  //        // The underlying stream of characters
		library           *TBiBTeXLibrary //        // The BiBTeX Library this parser will contribute to
		currentFieldName, //        // The name of the field that is currently being defined
		currentFieldValue, //       // The value ...
		currentEntryTypeName string // The tyoe ...
		skippingEntry bool       // // If we're skipping
		stringMap     TStringMap // // The mapping of strings ...
	}
)

var (
	BiBTeXRuneMap TRuneMap

	BiBTeXEmptyNameMap = TStringMap{}

	BiBTeXCommentEnders,
	BiBTeXKeyCharacters,
	BiBTeXSpaceCharacters,
	BiBTeXCommentStarters,
	BiBTeXFieldNameStarters,
	BiBTeXNumberCharacters,
	BiBTeXFieldNameCharacters,
	BiBTeXEntryTypeStarters,
	BiBTeXEntryTypeCharacters TByteSet
)

func (b *TBiBTeXStream) Initialise(reporting TInteraction, library *TBiBTeXLibrary) {
	b.TCharacterStream.Initialise(reporting)
	b.SetRuneMap(BiBTeXRuneMap)
	b.stringMap = BiBTeXDefaultStrings
	b.currentEntryTypeName = ""
	b.skippingEntry = false
	b.library = library
}

func (b *TBiBTeXStream) AssignString(str, value string) bool {
	b.stringMap[str] = value

	return true
}

func (b *TBiBTeXStream) MaybeReportError(message string, context ...any) bool {
	return b.skippingEntry || b.ReportError(message, context...)
}

func (b *TBiBTeXStream) SkipToNextEntry(from string) bool {
	b.skippingEntry = true

	if from != "" {
		b.ReportWarning(WarningSkippingToNextEntry + " from " + from)
	} else {
		b.ReportWarning(WarningSkippingToNextEntry)
	}

	for !b.ThisTokenIsCharacter(EntryStartCharacter) && !b.EndOfStream() {
		b.NextCharacter()
	}

	return b.ThisCharacterIs(EntryStartCharacter)
}

func (b *TBiBTeXStream) CommentsClausety() bool {
	for b.ThisCharacterWasNotIn(BiBTeXCommentEnders) {
		// Skip
	}

	return true
}

func (b *TBiBTeXStream) Comments() bool {
	return b.ThisCharacterWasIn(BiBTeXCommentStarters) &&
		/**/ b.CommentsClausety() &&
		/*  */ b.ForcedThisTokenWasCharacterIn(BiBTeXCommentEnders)
}

func (b *TBiBTeXStream) TeXSpaces() bool {
	result := false

	for b.ThisCharacterWasIn(BiBTeXSpaceCharacters) {
		result = true
	}

	return result
}

func (b *TBiBTeXStream) MoveToToken() bool {
	for b.TeXSpaces() || b.Comments() {
		// Skip
	}

	return true
}

func (b *TBiBTeXStream) ThisTokenIsCharacter(character byte) bool {
	return b.MoveToToken() &&
		/**/ b.ThisCharacterIs(character)
}

func (b *TBiBTeXStream) ThisTokenWasCharacter(character byte) bool {
	return b.MoveToToken() &&
		/**/ b.ThisCharacterWas(character)
}

func (b *TBiBTeXStream) ForcedThisTokenWasCharacterIn(S TByteSet) bool {
	return b.ThisCharacterWasIn(S) ||
		b.MaybeReportError(errorCharacterNotIn, S.String()) ||
		b.SkipToNextEntry("")
}

func (b *TBiBTeXStream) ForcedThisTokenWasCharacter(character byte) bool {
	return b.ThisTokenWasCharacter(character) ||
		b.MaybeReportError(ErrorMissingCharacter, string(character), string(b.ThisCharacter())) ||
		b.SkipToNextEntry("")
}

func (b *TBiBTeXStream) CollectCharacterOfNextTokenThatWasIn(characters TByteSet, s *string) bool {
	return b.MoveToToken() &&
		/**/ b.CollectCharacterThatWasIn(characters, s)
}

func (b *TBiBTeXStream) CharacterSequence(starters, characters TByteSet, sequence *string) bool {
	result := b.CollectCharacterOfNextTokenThatWasIn(starters, sequence)

	if result {
		for b.CollectCharacterThatWasIn(characters, sequence) {
			// Skip
		}
	}

	return result
}

func (b *TBiBTeXStream) GroupedFieldElement(groupEndCharacter byte, inTeXMode bool, content *string) bool {
	switch {
	case b.CollectCharacterThatWas(BeginGroupCharacter, content):
		return b.GroupedContentety(EndGroupCharacter, inTeXMode, content) &&
			/*    */ b.CollectCharacterThatWas(EndGroupCharacter, content)

	case b.CollectCharacterThatWas(EscapeCharacter, content):
		return b.CollectCharacterThatWasThere(content)

	case inTeXMode && b.TeXSpaces():
		return b.AddCharacter(SpaceCharacter, content)
	}

	return b.CollectCharacterThatWasNot(groupEndCharacter, content)
}

func (b *TBiBTeXStream) GroupedContentety(groupEndCharacter byte, inTeXMode bool, content *string) bool {
	for b.GroupedFieldElement(groupEndCharacter, inTeXMode, content) {
		// Skip
	}

	return true
}

func (b *TBiBTeXStream) Key(key *string) bool {
	return b.CharacterSequence(BiBTeXKeyCharacters, BiBTeXKeyCharacters, key)
}

func (b *TBiBTeXStream) Number(number *string) bool {
	return b.CharacterSequence(BiBTeXNumberCharacters, BiBTeXNumberCharacters, number)
}

func (b *TBiBTeXStream) FieldTypeName(starters, characters TByteSet, nameMap TStringMap, name *string) bool {
	result := b.CharacterSequence(starters, characters, name)

	*name = strings.ToLower(*name)

	normalized, mapped := nameMap[*name]
	if mapped {
		*name = normalized
	}

	return result
}

func (b *TBiBTeXStream) FieldName(nameMap TStringMap, name *string) bool {
	return b.FieldTypeName(BiBTeXFieldNameStarters, BiBTeXFieldNameCharacters, nameMap, name)
}

func (b *TBiBTeXStream) ForcedFieldName(nameMap TStringMap, name *string) bool {
	return b.FieldName(nameMap, name) ||
		b.MaybeReportError(ErrorMissingFieldName) ||
		b.SkipToNextEntry(FieldNameClass)
}

func (b *TBiBTeXStream) EntryType() bool {
	return b.FieldTypeName(BiBTeXEntryTypeStarters, BiBTeXEntryTypeCharacters, BiBTeXEntryNameMap, &b.currentEntryTypeName)
}

func (b *TBiBTeXStream) ForcedEntryType() bool {
	return b.EntryType() ||
		b.MaybeReportError(ErrorMissingEntryType) ||
		b.SkipToNextEntry(EntryTypeClass)
}

func (b *TBiBTeXStream) AddStringDefinition(name string, s *string) bool {
	value, defined := b.stringMap[name]

	if defined {
		*s += value
	} else {
		b.ReportError(ErrorUnknownString, name)
	}

	return true
}

func (b *TBiBTeXStream) FieldValueAdditionety(value *string) bool {
	switch {

	case b.ThisTokenWasCharacter(AdditionCharacter):
		return b.ForcedFieldValue(value) &&
			/* */ b.FieldValueAdditionety(value)

	default:
		return true

	}
}

func (b *TBiBTeXStream) FieldValue(value *string) bool {
	stringName := ""

	switch {

	case b.ThisTokenWasCharacter(BeginGroupCharacter):
		return b.GroupedContentety(EndGroupCharacter, TeXMode, value) &&
			/* */ b.ForcedThisTokenWasCharacter(EndGroupCharacter)

	case b.ThisTokenWasCharacter(DoubleQuotesCharacter):
		return b.GroupedContentety(DoubleQuotesCharacter, TeXMode, value) &&
			/* */ b.ForcedThisTokenWasCharacter(DoubleQuotesCharacter)

	case b.FieldName(BiBTeXEmptyNameMap, &stringName):
		return b.AddStringDefinition(stringName, value)

	default:
		return b.Number(value)
	}

	return false
}

func (b *TBiBTeXStream) ForcedFieldValue(value *string) bool {
	return b.FieldValue(value) ||
		b.MaybeReportError(ErrorMissingFieldValue) ||
		b.SkipToNextEntry(FieldValueClass)
}

func (b *TBiBTeXStream) RecordFieldAssignment(fieldName, fieldValue string, fieldMap TStringMap, fieldNames TStringSet) bool {
	fieldMap[fieldName] = fieldValue

	//	if fieldNames != nil {
	//		fieldNames[fieldName] = true
	//	}
	fieldNames.Add(fieldName)

	return true
}

func (b *TBiBTeXStream) ForcedFieldDefinitionProper(fieldName string, nameMap TStringMap, fieldMapper TMapField) bool {
	fieldValue := ""

	return b.ForcedThisTokenWasCharacter(AssignmentCharacter) &&
		/**/ b.ForcedFieldValue(&fieldValue) &&
		/*  */ b.FieldValueAdditionety(&fieldValue) &&
		/*    */ fieldMapper(fieldName, fieldValue)
}

func (b *TBiBTeXStream) FieldDefinitionety(nameMap TStringMap, fieldMapper TMapField) bool {
	fieldName := ""

	return b.FieldName(nameMap, &fieldName) &&
		/**/ b.ForcedFieldDefinitionProper(fieldName, nameMap, fieldMapper) ||
		true
}

func (b *TBiBTeXStream) FieldDefinitionsety(nameMap TStringMap, fieldMapper TMapField) bool {
	b.FieldDefinitionety(nameMap, fieldMapper)

	for b.ThisTokenWasCharacter(CommaCharacter) {
		b.FieldDefinitionety(nameMap, fieldMapper)
	}

	return true
}

func (b *TBiBTeXStream) EntryBodyProper() bool {
	switch b.currentEntryTypeName {
	case PreambleEntryType:
		ignore := ""
		return b.GroupedContentety(EndGroupCharacter, TeXMode, &ignore)

	case CommentEntryType:
		comment := ""
		return b.GroupedContentety(EndGroupCharacter, !TeXMode, &comment) &&
			/**/ b.library.AddComment(comment)

	case StringEntryType:
		return b.FieldDefinitionsety(BiBTeXEmptyNameMap, b.AssignString)

	default:
		key := ""
		return b.Key(&key) &&
			// Encapsulate this library. stuff
			/**/
			b.library.StartRecordingLibraryEntry(key, b.currentEntryTypeName) &&
			/*  */ b.FieldDefinitionsety(BiBTeXFieldNameMap, b.library.AssignField) &&
			/*    */ b.library.FinishRecordingLibraryEntry()
	}
}

func (b *TBiBTeXStream) ForcedEntryBodyProper() bool {
	return b.EntryBodyProper() ||
		b.MaybeReportError(ErrorMissingEntryBody) ||
		b.SkipToNextEntry(EntryBodyClass)
}

func (b *TBiBTeXStream) Entry() bool {
	b.currentEntryTypeName = ""

	return b.ThisTokenWasCharacter(EntryStartCharacter) &&
		/**/ b.ForcedEntryType() &&
		/*  */ b.ForcedThisTokenWasCharacter(BeginGroupCharacter) &&
		/*    */ b.ForcedEntryBodyProper() &&
		/*      */ b.ForcedThisTokenWasCharacter(EndGroupCharacter)
}

func (b *TBiBTeXStream) Entriesety() bool {
	// b.StartRecordingToLibrary() should encapsulate this.
	b.library.StartRecordingToLibrary()

	for b.Entry() {
		b.skippingEntry = false
	}

	b.library.FinishRecordingToLibrary()

	return true
}

func (b *TBiBTeXStream) ParseBiBFile(file string) bool {
	return b.ForcedTextfileOpen(file, ErrorOpeningFile) &&
		/**/ b.Entriesety()
}

func init() {
	BiBTeXRuneMap = TRuneMap{
		'À': "{\\`A}",
		'Á': "{\\'A}",
		'Â': "{\\^A}",
		'Ã': "{\\~A}",
		'Ä': "{\\\"A}",
		'Å': "{\\AA}",
		'Æ': "{\\AE}",
		'Ç': "{\\c C}",
		'È': "{\\`E}",
		'É': "{\\'E}",
		'Ê': "{\\^E}",
		'Ë': "{\\\"E}",
		'Ì': "{\\`\\I}",
		'Í': "{\\'\\I}",
		'Î': "{\\^I}",
		'Ï': "{\\\"\\I}",
		'Ñ': "{\\~n}",
		'Ò': "{\\`O}",
		'Ó': "{\\'O}",
		'Ô': "{\\^O}",
		'Õ': "{\\~O}",
		'Ö': "{\\\"O}",
		'Ø': "{\\O}",
		'Ù': "{\\`U}",
		'Ú': "{\\'U}",
		'Û': "{\\^U}",
		'Ü': "{\\\"Y}",
		'Ý': "{\\'Y}",
		'ß': "{\\ss}",
		'à': "{\\`a}",
		'á': "{\\'a}",
		'â': "{\\^a}",
		'ã': "{\\~a}",
		'ä': "{\\\"a}",
		'å': "{\\\aa}",
		'æ': "{\\ae}",
		'ç': "{\\c c}",
		'è': "{\\`e}",
		'é': "{\\'e}",
		'ê': "{\\^e}",
		'ë': "{\\\"e}",
		'ì': "{\\`\\i}",
		'í': "{\\'\\i}",
		'î': "{\\^i}",
		'ï': "{\\\"\\i}",
		'ñ': "{\\~n}",
		'ò': "{\\`o}",
		'ó': "{\\'o}",
		'ô': "{\\^o}",
		'õ': "{\\~o}",
		'ö': "{\\\"o}",
		'ø': "{\\o}",
		'ù': "{\\`u}",
		'ú': "{\\'u}",
		'û': "{\\^u}",
		'ü': "{\\\"u}",
		'ý': "{\\'y}",
		'ÿ': "{\\\"y}",
		'Ā': "{\\=A}",
		'ā': "{\\=a}",
		'Ć': "{\\'E}",
		'ć': "{\\'e}",
		'Ċ': "{\\.C}",
		'ċ': "{\\.c}",
		'Č': "{\\v C}",
		'č': "{\\v c}",
		'Ē': "{\\=E}",
		'ē': "{\\=e}",
		'Ė': "{\\.E}",
		'ė': "{\\.e}",
		'Ę': "{\\k E}",
		'ę': "{\\k e}",
		'Ě': "{\\v E}",
		'ě': "{\\v e}",
		'Ğ': "{\\v G}",
		'ğ': "{\\v g}",
		'Ġ': "{\\.G}",
		'ġ': "{\\.g}",
		'Ĩ': "{\\~\\I}",
		'ĩ': "{\\~\\i}",
		'Ī': "{\\=\\I}",
		'ī': "{\\=\\i}",
		'Į': "{\\k I}",
		'į': "{\\k i}",
		'ı': "{\\i}",
		'Ł': "{\\L}",
		'ł': "{\\l}",
		'Ń': "{\\'N}",
		'ń': "{\\'n}",
		'Ň': "{\\v N}",
		'ň': "{\\v a}",
		'Ō': "{\\= O}",
		'ō': "{\\=o}",
		'Œ': "{\\OE}",
		'œ': "{\\oe}",
		'Ř': "{\\v R}",
		'ř': "{\\v r}",
		'Ś': "{\\'S}",
		'ś': "{\\'s}",
		'Ş': "{\\c S}",
		'ş': "{\\c s}",
		'Š': "{\\v S}",
		'š': "{\\v s}",
		'Ũ': "{\\~U}",
		'ũ': "{\\~u}",
		'Ū': "{\\=U}",
		'ū': "{\\= u}",
		'Ů': "{\\r U}",
		'ů': "{\\r u}",
		'Ű': "{\\H U}",
		'ű': "{\\H u}",
		'Ŵ': "{\\v U}",
		'ŵ': "{\\v u}",
		'Ŷ': "{\\^Y}",
		'ŷ': "{\\^y}",
		'Ÿ': "{\\\"Y}",
		'Ź': "{\\'Y}",
		'ź': "{\\'y}",
		'Ż': "{\\.Z}",
		'ż': "{\\.z}",
		'Ž': "{\\v Z}",
		'ž': "{\\v z}",
		'Ǎ': "{\\v A}",
		'ǎ': "{\\v a}",
		'Ǐ': "{\\v\\I}",
		'ǐ': "{\\v\\i}",
		'Ǒ': "{\\v O}",
		'ǒ': "{\\v o}",
		'Ǔ': "{\\v U}",
		'ǔ': "{\\v u}",
		'ȍ': "{\\H o}",
		'Ẽ': "{\\~E}",
		'ẽ': "{\\~e}",
		'–': "--",
		'©': "{\textcopyright}",
		'®': "{\textregistered}",
	}

	BiBTeXSpaceCharacters.Add(
		SpaceCharacter, NewlineCharacter, BackspaceCharacter, BellCharacter,
		CarriageReturnCharacter, FormFeedCharacter, TabCharacter,
		VerticalTabCharacter).TreatAsCharacters()

	BiBTeXCommentStarters.Add(PercentCharacter).TreatAsCharacters()

	BiBTeXCommentEnders.Add(NewlineCharacter).TreatAsCharacters()

	BiBTeXNumberCharacters.AddString("0123456789").TreatAsCharacters()

	BiBTeXKeyCharacters.AddString(
		"abcdefghijklmnopqrstuvwxyz" +
			"ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
			"0123456789" +
			"<>()[];|!*+?&#$-_:/.'`").TreatAsCharacters()

	BiBTeXFieldNameStarters.AddString(
		"abcdefghijklmnopqrstuvwxyz" +
			"ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
			"-_").TreatAsCharacters()

	BiBTeXFieldNameCharacters = BiBTeXFieldNameStarters
	BiBTeXFieldNameCharacters.Unite(BiBTeXNumberCharacters)

	BiBTeXEntryTypeStarters.AddString(
		"abcdefghijklmnopqrstuvwxyz" +
			"ABCDEFGHIJKLMNOPQRSTUVWXYZ").TreatAsCharacters()

	BiBTeXEntryTypeCharacters.AddString(
		"abcdefghijklmnopqrstuvwxyz" +
			"ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
			"0123456789" +
			"-_").TreatAsCharacters()
}
