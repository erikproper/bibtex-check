package main

import "strings"

const (
	errorMissingCharacter      = "Missing character '%s'"
	errorMissingEntryBody      = "Missing EntryBody"
	errorMissingEntryType      = "Missing EntryType"
	errorMissingTagName        = "Missing TagName"
	errorMissingTagValue       = "Missing TagValue"
	errorOpeningFile           = "Could not open file '%s'"
	errorUnknownString         = "Unknown string '%s' referenced"
	warningSkippingToNextEntry = "Skipping to next entry"
	fromEntryBody              = "EntryBody"
	fromTagName                = "TagName"
	fromEntryType              = "EntryType"
	fromTagValue               = "TagValue"
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
	TStringMap    map[string]string
	TStringSet    map[string]bool
	TBiBTeXStream struct {
		TCharacterStream
		TBiBTeXLibrary
		currentTagName,
		currentTagValue,
		currentEntryTypeName string
		skippingEntry bool
		stringMap     TStringMap
	}
)

var (
	BiBTeXRuneMap TRuneMap

	BiBTeXTagNameMap,
	BiBTeXEmptyNameMap,
	BiBTeXEntryNameMap TStringMap

	BiBTeXCommentEnders,
	BiBTeXKeyCharacters,
	BiBTeXSpaceCharacters,
	BiBTeXCommentStarters,
	BiBTeXTagNameStarters,
	BiBTeXNumberCharacters,
	BiBTeXTagNameCharacters,
	BiBTeXEntryTypeStarters,
	BiBTeXEntryTypeCharacters TByteSet
)

func (b *TBiBTeXStream) NewBiBTeXParser(reporting TReporting, library TBiBTeXLibrary) {
	b.NewCharacterStream(reporting)
	b.SetRuneMap(BiBTeXRuneMap)
	b.stringMap = TStringMap{}
	b.currentEntryTypeName = ""
	b.skippingEntry = false
	b.TBiBTeXLibrary = library
}

func (b *TBiBTeXStream) RegisterNewLibraryEntry(key string) bool {
	return b.NewLibraryEntry(key)
}

func (b *TBiBTeXStream) MaybeReportError(message string, context ...any) bool {
	return b.skippingEntry ||
		b.ReportError(message, context...)
}

func (b *TBiBTeXStream) SkipToNextEntry(from string) bool {
	b.skippingEntry = true

	if from != "" {
		b.ReportWarning(warningSkippingToNextEntry + " from " + from)
	} else {
		b.ReportWarning(warningSkippingToNextEntry)
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
		b.MaybeReportError(errorMissingCharacter, string(character)) ||
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

func (b *TBiBTeXStream) GroupedTagElement(groupEndCharacter byte, inTeXMode bool, content *string) bool {
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
	for b.GroupedTagElement(groupEndCharacter, inTeXMode, content) {
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

func (b *TBiBTeXStream) TagTypeName(starters, characters TByteSet, nameMap TStringMap, name *string) bool {
	result := b.CharacterSequence(starters, characters, name)

	*name = strings.ToLower(*name)

	normalized, mapped := nameMap[*name]
	if mapped {
		*name = normalized
	}

	return result
}

func (b *TBiBTeXStream) TagName(nameMap TStringMap, name *string) bool {
	return b.TagTypeName(BiBTeXTagNameStarters, BiBTeXTagNameCharacters, nameMap, name)
}

func (b *TBiBTeXStream) ForcedTagName(nameMap TStringMap, name *string) bool {
	return b.TagName(nameMap, name) ||
		b.MaybeReportError(errorMissingTagName) ||
		b.SkipToNextEntry(fromTagName)
}

func (b *TBiBTeXStream) EntryType() bool {
	return b.TagTypeName(BiBTeXEntryTypeStarters, BiBTeXEntryTypeCharacters, BiBTeXEntryNameMap, &b.currentEntryTypeName)
}

func (b *TBiBTeXStream) ForcedEntryType() bool {
	return b.EntryType() ||
		b.MaybeReportError(errorMissingEntryType) ||
		b.SkipToNextEntry(fromEntryType)
}

func (b *TBiBTeXStream) AddStringDefinition(name string, s *string) bool {
	value, defined := b.stringMap[name]

	if defined {
		*s += value
	} else {
		b.ReportError(errorUnknownString, name)
	}

	return true
}

func (b *TBiBTeXStream) TagValueAdditionety(value *string) bool {
	switch {

	case b.ThisTokenWasCharacter(AdditionCharacter):
		return b.ForcedTagValue(value) &&
			/* */ b.TagValueAdditionety(value)

	default:
		return true

	}
}

func (b *TBiBTeXStream) TagValue(value *string) bool {
	stringName := ""

	switch {

	case b.ThisTokenWasCharacter(BeginGroupCharacter):
		return b.GroupedContentety(EndGroupCharacter, TeXMode, value) &&
			/* */ b.ForcedThisTokenWasCharacter(EndGroupCharacter)

	case b.ThisTokenWasCharacter(DoubleQuotesCharacter):
		return b.GroupedContentety(DoubleQuotesCharacter, TeXMode, value) &&
			/* */ b.ForcedThisTokenWasCharacter(DoubleQuotesCharacter)

	case b.TagName(BiBTeXEmptyNameMap, &stringName):
		return b.AddStringDefinition(stringName, value)

	default:
		return b.Number(value)
	}

	return false
}

func (b *TBiBTeXStream) ForcedTagValue(value *string) bool {
	return b.TagValue(value) ||
		b.MaybeReportError(errorMissingTagValue) ||
		b.SkipToNextEntry(fromTagValue)
}

func (b *TBiBTeXStream) RecordTagAssignment(tagName, tagValue string, tagMap TStringMap, tagNames TStringSet) bool {
	tagMap[tagName] = tagValue

	if tagNames != nil {
		tagNames[tagName] = true
	}

	return true
}

func (b *TBiBTeXStream) ForcedTagDefinitionProper(tagName string, nameMap TStringMap, tagMap TStringMap, tagNames TStringSet) bool {
	tagValue := ""

	return b.ForcedThisTokenWasCharacter(AssignmentCharacter) &&
		/**/ b.ForcedTagValue(&tagValue) &&
		/*  */ b.TagValueAdditionety(&tagValue) &&
		/*    */ b.RecordTagAssignment(tagName, tagValue, tagMap, tagNames)
}

func (b *TBiBTeXStream) TagDefinitionety(nameMap TStringMap, tagMap TStringMap, tagNames TStringSet) bool {
	tagName := ""

	return b.TagName(nameMap, &tagName) &&
		/**/ b.ForcedTagDefinitionProper(tagName, nameMap, tagMap, tagNames) ||
		true
}

func (b *TBiBTeXStream) TagDefinitionsety(nameMap TStringMap, tagMap TStringMap, tagNames TStringSet) bool {
	b.TagDefinitionety(nameMap, tagMap, tagNames)

	for b.ThisTokenWasCharacter(CommaCharacter) {
		b.TagDefinitionety(nameMap, tagMap, tagNames)
	}

	return true
}

func (b *TBiBTeXStream) EntryBodyProper() bool {

	switch b.currentEntryTypeName {
	case PreambleEntryType:
		ignore := ""
		return b.GroupedContentety(EndGroupCharacter, TeXMode, &ignore)

		/// Store/write as well
	case CommentEntryType:
		comment := ""
		return b.GroupedContentety(EndGroupCharacter, !TeXMode, &comment)

	case StringEntryType:
		return b.TagDefinitionsety(BiBTeXEmptyNameMap, b.stringMap, nil)
		// StringMap

	default:
		key := ""

		return b.Key(&key) &&
			/**/ b.RegisterNewLibraryEntry(key) &&
			/*  */ b.TagDefinitionsety(BiBTeXTagNameMap, b.SelectedEntry.Tags, b.UsedTags)
	}
}

func (b *TBiBTeXStream) ForcedEntryBodyProper() bool {
	return b.EntryBodyProper() ||
		b.MaybeReportError(errorMissingEntryBody) ||
		b.SkipToNextEntry(fromEntryBody)
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
	for b.Entry() {
		b.skippingEntry = false
	}

	return true
}

func (b *TBiBTeXStream) ParseBiBFile(file string) bool {
	return b.ForcedTextfileOpen(file, errorOpeningFile) &&
		/**/ b.Entriesety()
}

func init() {
	// Should move into a settings file.
	// Settings should be an environment variable ...
	// see https://gobyexample.com/environment-variables
	// If settings file does not exist, then create one and push this as default into ib.
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
		'©': "{\textcopyright}",
		'®': "{\textregistered}",
	}

	BiBTeXEmptyNameMap = TStringMap{}
	BiBTeXEntryNameMap = BiBTeXEmptyNameMap
	BiBTeXEntryNameMap["conference"] = "inproceedings"
	BiBTeXEntryNameMap["softmisc"] = "misc"
	BiBTeXEntryNameMap["patent"] = "misc"

	BiBTeXTagNameMap = BiBTeXEmptyNameMap
	BiBTeXTagNameMap["editors"] = "editor"
	BiBTeXTagNameMap["authors"] = "author"
	BiBTeXTagNameMap["contributor"] = "author"
	BiBTeXTagNameMap["contributors"] = "author"

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
			"<>;\\|!*+?&#$-_:/.`").TreatAsCharacters()

	BiBTeXTagNameStarters.AddString(
		"abcdefghijklmnopqrstuvwxyz" +
			"ABCDEFGHIJKLMNOPQRSTUVWXYZ").TreatAsCharacters()

	BiBTeXTagNameCharacters.AddString(
		"abcdefghijklmnopqrstuvwxyz" +
			"ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
			"0123456789" +
			"-").TreatAsCharacters()

	BiBTeXEntryTypeStarters.AddString(
		"abcdefghijklmnopqrstuvwxyz" +
			"ABCDEFGHIJKLMNOPQRSTUVWXYZ").TreatAsCharacters()

	BiBTeXEntryTypeCharacters.AddString(
		"abcdefghijklmnopqrstuvwxyz" +
			"ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
			"0123456789" +
			"-").TreatAsCharacters()
}
