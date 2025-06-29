/*
 *
 *  Module: bibtex_stream
 *
 * This module is defines the TBibTeXStream type as a parser of BibTeX entries
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 26.04.2024
 *
 */

package main

import "strings"

const (
	// TeXMode on
	TeXMode = true

	// Different characters
	EntryStartCharacter   = '@'
	BeginGroupCharacter   = '{'
	EndGroupCharacter     = '}'
	DoubleQuotesCharacter = '"'
	AssignmentCharacter   = '='
	AdditionCharacter     = '#'
	PercentCharacter      = '%'
	CommaCharacter        = ','
	EscapeCharacter       = '\\'

	// Entry types with a hard-wired meaning
	CommentEntryType  = "comment"
	PreambleEntryType = "preamble"
	StringEntryType   = "string"
)

type (
	// As we need to cater for the fact that values for fields in BibTeX entries need to be assigned differently depending on the fact if it concerns an @string entry, or an actual publication, we will use a function as parameter to take care of the correct assignment.
	TFieldAssigner func(string, string, string) bool

	// The actual TBibTeXStream type
	TBibTeXStream struct {
		TCharacterStream                 // The underlying stream of characters.
		library          *TBibTeXLibrary // The BibTeX Library this parser will contribute to.
		skippingEntry    bool            // Set to true if we need to be skipping things to the next entry.
		stringMap        TStringMap      // The mapping of the defined strings.
		succeeded        bool            // Set to false if we had a serious problem in parsing the stream.
	}
)

// NOTE: can't we keep the currentXXX inside the parsing structure?
// NOTE: or actually ... include currentKey as well, solving the dummy parameter for string assignments ...

var (
	// Translation from runes to TeX strings
	BibTeXRuneMap TRuneMap

	// The empty name mapping
	BibTeXEmptyNameMap = TStringMap{}

	// Character sets used by the parser.
	// These are defined in the init() function
	BibTeXCommentEnders,
	BibTeXKeyCharacters,
	BibTeXSpaceCharacters,
	BibTeXCommentStarters,
	BibTeXNumberCharacters,
	BibTeXEntryTypeStarters,
	BibTeXFieldNameStarters,
	BibTeXFieldNameCharacters,
	BibTeXEntryTypeCharacters TByteSet
)

// Initialise a BibTeXStream-er.
func (b *TBibTeXStream) Initialise(reporting TInteraction, library *TBibTeXLibrary) {
	b.TCharacterStream.Initialise(reporting)
	b.SetRuneMap(BibTeXRuneMap)
	b.stringMap = BibTeXDefaultStrings
	b.skippingEntry = false
	b.library = library
	b.succeeded = true
}

// HELLO. Can we do this cleaner? The dummy for the key??
// Assignment of a string definition.
func (b *TBibTeXStream) AssignString(dummy, str, value string) bool {
	b.stringMap[str] = value

	return true
}

// If we're stuck in parsing an entry (due to syntax errors), we need to the next entry.
func (b *TBibTeXStream) SkipToNextEntry(from string) bool {
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

// CONVENTIONS regarding the parser
//
// In defining the parser, we use the short-cut AND and OR operators.
// Doing so, allows us to translate a grammar rule such as:
//   A: ( B, C ) | D
// into:
//   func A() bool {
//      return ( B() && C() ) || D()
//   }
//
// This convention is inspired by the work on the compiler description language:
//    https://en.wikipedia.org/wiki/Compiler_Description_Language
// and CDL1 in particular.
//
// A further convention is the use of the "ety" ending of the name of a grammatical class.
// For instance, CommentsClausety.
// The "CommentsClause" is a grammatical class, where the "ety" ending indicates that it could example be empty.
// So, CommentsClausety should be read as "CommentsClausety or Empty".
// If course, the function CommentsClausety() should cater for the "or Empty" part.
//
// Furthermore, when we want to signify that syntactically something is required to be present, we will prefix "Forced" to the function name.
// So, in terms of the above example:
//   A: ( B, C ) | D
// We would actually use
//   func A() bool {
//      return ( B() && ForcedC() ) || D()
//   }
// Since, after the B, we *must* have a C.
// Of course, the ForcedC() function would need to report an error, when no C is found.

// When we run into a syntax error, we need to report this.
// However, when we're in the process of skipping to a next entry, we need to remain silent about further errors, until we have reached the next entry.
// Therefore, we will use ReportParsingError with the skippingEntry condition
func (b *TBibTeXStream) ReportParsingError(message string, context ...any) bool {
	// If we needed to report an error, then do not write the BibFile after an update
	// POST MIGRO update
	b.library.NoBibFileWriting = true

	if !b.skippingEntry {
		b.ReportError(message, context...)
		b.succeeded = false
	}

	return true
}

// Dealing with comments.
func (b *TBibTeXStream) Comments() bool {
	return b.ThisCharacterWasIn(BibTeXCommentStarters) &&
		/**/ b.CommentsClausety() &&
		/*  */ b.ForcedThisTokenWasCharacterIn(BibTeXCommentEnders)
}

// The actual comment, which could be empty.
func (b *TBibTeXStream) CommentsClausety() bool {
	for b.ThisCharacterWasNotIn(BibTeXCommentEnders) {
		// Skip
	}

	return true
}

// Gobbling up characters that are treated as (a single!) space by TeX.
func (b *TBibTeXStream) TeXSpaces() bool {
	result := false

	for b.ThisCharacterWasIn(BibTeXSpaceCharacters) {
		result = true
	}

	return result
}

// Move to the next token, while gobbling up TeXSpaces and Comments.
func (b *TBibTeXStream) MoveToToken() bool {
	for b.TeXSpaces() || b.Comments() {
		// Skip
	}

	return true
}

// Check if the present token is equal to the given character.
// As we're not certain we're already "at" the token, we need to do a MoveToToken first.
func (b *TBibTeXStream) ThisTokenIsCharacter(character byte) bool {
	return b.MoveToToken() &&
		/**/ b.ThisCharacterIs(character)
}

// Check if the present token is equal to the given character.
// As we're not certain we're already "at" the token, we need to do a MoveToToken first.
func (b *TBibTeXStream) ThisTokenWasCharacter(character byte) bool {
	return b.MoveToToken() &&
		/**/ b.ThisCharacterWas(character)
}

// The Forced version of ThisTokenWasCharacterIn, with an error message if not found.
func (b *TBibTeXStream) ForcedThisTokenWasCharacterIn(S TByteSet) bool {
	return b.ThisCharacterWasIn(S) ||
		b.ReportParsingError(ErrorCharacterNotIn, S.String()) ||
		b.SkipToNextEntry("")
}

// The Forced version of ThisTokenWasCharacter, with an error message if not found.
func (b *TBibTeXStream) ForcedThisTokenWasCharacter(character byte) bool {
	return b.ThisTokenWasCharacter(character) ||
		b.ReportParsingError(ErrorMissingCharacter, string(character), string(b.ThisCharacter())) ||
		b.SkipToNextEntry("")
}

// Collect characters from the stream that are in a given set.
// Before doing so, we need to make sure we're at the start of the current ((Bib)TeX) Token.
func (b *TBibTeXStream) CollectCharacterOfNextTokenThatWasIn(characters TByteSet, s *string) bool {
	return b.MoveToToken() &&
		/**/ b.CollectCharacterThatWasIn(characters, s)
}

// Parse a (non-empty) character sequence, where the characters must be from a given set.
func (b *TBibTeXStream) CharacterSequence(starters, characters TByteSet, sequence *string) bool {
	result := b.CollectCharacterOfNextTokenThatWasIn(starters, sequence)

	if result {
		for b.CollectCharacterThatWasIn(characters, sequence) {
			// Skip
		}
	}

	return result
}

// Elements (characters, spaces, or nested elements) of field values
func (b *TBibTeXStream) GroupedFieldElement(groupEndCharacter byte, inTeXMode bool, content *string) bool {
	switch {
	// Elements nested between { }. For example {\"a} {\v O} or {KLM}
	case b.CollectCharacterThatWas(BeginGroupCharacter, content):
		return b.GroupedContentety(EndGroupCharacter, inTeXMode, content) &&
			/* */ b.CollectCharacterThatWas(EndGroupCharacter, content)

	// Elements that involve an escape. For example \", \', or \vdash
	case b.CollectCharacterThatWas(EscapeCharacter, content):
		return b.CollectCharacterThatWasThere(content)

	// When inTeXMode, then TeXSpaces count as one space
	case inTeXMode && b.TeXSpaces():
		return b.AddCharacter(SpaceCharacter, content)

	// Otherwise, collect any character that is not the provided groupEndCharacter
	// Since LaTeX allows the values of fields to be enclosed by { and }, or by " and ", we need to use this as a parameter.
	default:
		return b.CollectCharacterThatWasNot(groupEndCharacter, content)
	}
}

// Collect the (grouped) content of a field value.
func (b *TBibTeXStream) GroupedContentety(groupEndCharacter byte, inTeXMode bool, content *string) bool {
	for b.GroupedFieldElement(groupEndCharacter, inTeXMode, content) {
		// Skip
	}

	return true
}

// Keys of BibTeX entries
func (b *TBibTeXStream) Key(key *string) bool {
	return b.CharacterSequence(BibTeXKeyCharacters, BibTeXKeyCharacters, key)
}

// Numbers can be used as field values, without grouping them using { and } or " and "
func (b *TBibTeXStream) Number(number *string) bool {
	return b.CharacterSequence(BibTeXNumberCharacters, BibTeXNumberCharacters, number)
}

// BibTeX names: i.e. entry types or field names.
// Both of these need to be normalised towards lowercase characters.
func (b *TBibTeXStream) BibTeXName(starters, characters TByteSet, nameMap TStringMap, name *string) bool {
	result := b.CharacterSequence(starters, characters, name)

	*name = strings.ToLower(*name)

	mappedName, isMapped := nameMap[*name]
	if isMapped {
		*name = mappedName
	}

	return result
}

// Note: we cannot leave this nameMap-ing to the Library functionality, since the mapped name influences the behaviour of the parser.

// FieldNames
func (b *TBibTeXStream) FieldName(nameMap TStringMap, name *string) bool {
	return b.BibTeXName(BibTeXFieldNameStarters, BibTeXFieldNameCharacters, nameMap, name)
}

// EntryTypes
func (b *TBibTeXStream) EntryType(entryType *string) bool {
	return b.BibTeXName(BibTeXEntryTypeStarters, BibTeXEntryTypeCharacters, BibTeXEntryMap, entryType)
}

// Forced EntryTypes
func (b *TBibTeXStream) ForcedEntryType(entryType *string) bool {
	return b.EntryType(entryType) ||
		b.ReportParsingError(ErrorMissingEntryType) ||
		b.SkipToNextEntry(EntryTypeClass)
}

// We do not "keep" string definitions in BibTeX files we write out.
// Therefore, string definition need to be added to the parser's administration, and they are not stored in the actual library.
// So, when the definition of a field value refers to a string definition, the value of that string needs to be added.
func (b *TBibTeXStream) AddStringDefinition(name string, s *string) bool {
	value, defined := b.stringMap[name]

	if defined {
		*s += value
	} else {
		b.ReportParsingError(ErrorUnknownString, name)
	}

	return true
}

// Values of the fields of BibTeX entries may be composed. For example: "{Hello} # { } # {World}".
func (b *TBibTeXStream) FieldValueAdditionety(value *string) bool {
	switch {

	case b.ThisTokenWasCharacter(AdditionCharacter):
		return b.ForcedFieldValue(value) &&
			/* */ b.FieldValueAdditionety(value)

	default:
		return true

	}
}

// Field values
func (b *TBibTeXStream) FieldValue(value *string) bool {
	stringName := ""

	switch {
	// A "normal" field value enclosed by { and }
	case b.ThisTokenWasCharacter(BeginGroupCharacter):
		return b.GroupedContentety(EndGroupCharacter, TeXMode, value) &&
			/* */ b.ForcedThisTokenWasCharacter(EndGroupCharacter)

	// A "normal" field value enclosed by " and "
	case b.ThisTokenWasCharacter(DoubleQuotesCharacter):
		return b.GroupedContentety(DoubleQuotesCharacter, TeXMode, value) &&
			/* */ b.ForcedThisTokenWasCharacter(DoubleQuotesCharacter)

	// The reference to a string definition.
	case b.FieldName(BibTeXEmptyNameMap, &stringName):
		return b.AddStringDefinition(stringName, value)

	// A (non enclosed) number.
	default:
		return b.Number(value)
	}

	return false
}

// Forced FieldValue
func (b *TBibTeXStream) ForcedFieldValue(value *string) bool {
	return b.FieldValue(value) ||
		b.ReportParsingError(ErrorMissingFieldValue) ||
		b.SkipToNextEntry(FieldValueClass)
}

// Forced FieldDefinitionBody
func (b *TBibTeXStream) ForcedFieldDefinitionBody(key, fieldName string, nameMap TStringMap, fieldAssigner TFieldAssigner) bool {
	fieldValue := ""

	return b.ForcedThisTokenWasCharacter(AssignmentCharacter) &&
		/**/ b.ForcedFieldValue(&fieldValue) &&
		/*  */ b.FieldValueAdditionety(&fieldValue) &&
		/*    */ fieldAssigner(key, fieldName, fieldValue)
}

// The FieldDefinition.
// We do allow for empty field definitions, when the BibTeXFile e.g. contains ", ," in the middel, or ", }" at the end.
func (b *TBibTeXStream) FieldDefinitionety(key string, nameMap TStringMap, fieldAssigner TFieldAssigner) bool {
	fieldName := ""

	return b.FieldName(nameMap, &fieldName) &&
		/**/ b.ForcedFieldDefinitionBody(key, fieldName, nameMap, fieldAssigner) ||
		true
}

// A sequence of FieldDefinitions, separated by a ","
func (b *TBibTeXStream) FieldDefinitionsety(key string, nameMap TStringMap, fieldAssigner TFieldAssigner) bool {
	b.FieldDefinitionety(key, nameMap, fieldAssigner)

	for b.ThisTokenWasCharacter(CommaCharacter) {
		b.FieldDefinitionety(key, nameMap, fieldAssigner)
	}

	return true
}

// EntryDefinitionBodies.
// Depending on the EntryType, we need to allow for four variations.
func (b *TBibTeXStream) EntryDefinitionBody(entryType string) bool {
	switch entryType {

	// LaTeX preambles.
	// For the moment, we actually completely ignore these.
	case PreambleEntryType:
		ignore := ""
		return b.GroupedContentety(EndGroupCharacter, TeXMode, &ignore)

	// Comments.
	// As BibDesk uses the comments to store static group, etc, we cannot ignore these.
	// In future versions, we would actually need to parse these, while switching to XML mode.
	case CommentEntryType:
		comment := ""
		return b.GroupedContentety(EndGroupCharacter, !TeXMode, &comment) &&
			/**/ (b.BibDeskStaticGroupDefinition(comment) || b.library.ProcessComment(comment))

	// String definitions.
	case StringEntryType:
		key := ""
		return b.FieldDefinitionsety(key, BibTeXEmptyNameMap, b.AssignString)

	// Regular entries.
	default:
		key := ""
		return b.Key(&key) &&
			/**/ b.library.StartRecordingLibraryEntry(key, entryType) &&
			/*  */ b.FieldDefinitionsety(key, BibTeXFieldMap, b.library.AssignField) &&
			/*    */ b.library.FinishRecordingLibraryEntry(key)
	}
}

// ForcedEntryDefinitionBody
func (b *TBibTeXStream) ForcedEntryDefinitionBody(entryType string) bool {
	return b.EntryDefinitionBody(entryType) ||
		b.ReportParsingError(ErrorMissingEntryBody) ||
		b.SkipToNextEntry(EntryBodyClass)
}

// The actual Entries
func (b *TBibTeXStream) Entry() bool {
	entryType := ""

	return b.ThisTokenWasCharacter(EntryStartCharacter) &&
		/**/ b.ForcedEntryType(&entryType) &&
		/*  */ b.ForcedThisTokenWasCharacter(BeginGroupCharacter) &&
		/*    */ b.ForcedEntryDefinitionBody(entryType) &&
		/*      */ b.ForcedThisTokenWasCharacter(EndGroupCharacter)
}

// The possibly empty series of Entries included in tha BibTeX file.
func (b *TBibTeXStream) Entriesety() bool {
	for b.Entry() {
		// Reset this to false after each (possibly skipped) entry
		b.skippingEntry = false
	}

	return true
}

// Parse current stream to the library
func (b *TBibTeXStream) ParseBibTeXStream() bool {
	return b.library.StartRecordingToLibrary() &&
		/**/ b.Entriesety() &&
		/*  */ b.library.FinishRecordingToLibrary() &&
		/*    */ b.succeeded
}

// Opening a BibTeX file, and then parse it (and add it to the selected Library.)
func (b *TBibTeXStream) ParseBibFile(fileName string) bool {
	b.ReportProgress(ProgressReadingBibFile, fileName)

	return b.ForcedTextfileOpen(fileName, ErrorOpeningFile) &&
		/**/ b.ParseBibTeXStream()
}

// Opening a string with BibTeX entries, and then parse it (and add it to the selected Library.)
func (b *TBibTeXStream) ParseBibString(bibtex string) bool {
	return b.TextString(bibtex) &&
		/**/ b.ParseBibTeXStream()
}

// Things to be initialised
func init() {
	// Define the RuneMap for the BibTeX parser.
	BibTeXRuneMap = TRuneMap{
		///// Better order!
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
		'Ì': "{\\`I}",
		'Í': "{\\'I}",
		'Î': "{\\^I}",
		'Ï': "{\\\"I}",
		'Ñ': "{\\~N}",
		'Ò': "{\\`O}",
		'Ó': "{\\'O}",
		'Ô': "{\\^O}",
		'Õ': "{\\~O}",
		'Ö': "{\\\"O}",
		'Ø': "{\\O}",
		'Ù': "{\\`U}",
		'Ú': "{\\'U}",
		'Û': "{\\^U}",
		'Ü': "{\\\"U}",
		'ß': "{\\ss}",
		'à': "{\\`a}",
		'á': "{\\'a}",
		'â': "{\\^a}",
		'ã': "{\\~a}",
		'ä': "{\\\"a}",
		'å': "{\\aa}",
		'æ': "{\\ae}",
		'ç': "{\\c c}",
		'è': "{\\`e}",
		'é': "{\\'e}",
		'ê': "{\\^e}",
		'ë': "{\\\"e}",
		'ì': "{\\`\\i}",
		'í': "{\\'\\i}",
		'î': "{\\^\\i}",
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
		'Ć': "{\\'C}",
		'ć': "{\\'c}",
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
		'Ĩ': "{\\~I}",
		'ĩ': "{\\~\\i}",
		'Ī': "{\\=I}",
		'ī': "{\\=\\i}",
		'Į': "{\\k I}",
		'į': "{\\k i}",
		'ı': "\\i",
		'Ł': "{\\L}",
		'ł': "{\\l}",
		'Ń': "{\\'N}",
		'ń': "{\\'n}",
		'Ň': "{\\v N}",
		'ň': "{\\v n}",
		'Ō': "{\\=O}",
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
		'ū': "{\\=u}",
		'Ů': "{\\r U}",
		'ů': "{\\r u}",
		'Ű': "{\\H U}",
		'ű': "{\\H u}",
		'Ŵ': "{\\v W}",
		'ŵ': "{\\v w}",
		'Ŷ': "{\\^Y}",
		'ŷ': "{\\^y}",
		'Ÿ': "{\\\"Y}",
		'Ý': "{\\'Y}",
		'Ź': "{\\'Y}",
		'ź': "{\\'z}",
		'Ż': "{\\.Z}",
		'ż': "{\\.z}",
		'Ž': "{\\v Z}",
		'ž': "{\\v z}",
		'Ǎ': "{\\v A}",
		'ǎ': "{\\v a}",
		'Ǐ': "{\\v I}",
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

	// Characters that count as spaces from a TeX perspective.
	BibTeXSpaceCharacters.Add(
		SpaceCharacter, NewlineCharacter, BackspaceCharacter, BellCharacter,
		CarriageReturnCharacter, FormFeedCharacter, TabCharacter,
		VerticalTabCharacter).TreatAsCharacters()

	// Start of comments.
	BibTeXCommentStarters.Add(PercentCharacter).TreatAsCharacters()

	// End of comments.
	BibTeXCommentEnders.Add(NewlineCharacter).TreatAsCharacters()

	// Numbers
	BibTeXNumberCharacters.AddString("0123456789").TreatAsCharacters()

	// Characters allowed in BibTeX keys
	BibTeXKeyCharacters.AddString(
		"abcdefghijklmnopqrstuvwxyz" +
			"ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
			"0123456789" +
			"<>()[];|!*+?&#$-_:/.'`").TreatAsCharacters()

	// Characters allowed at the start of field names
	BibTeXFieldNameStarters.AddString(
		"abcdefghijklmnopqrstuvwxyz" +
			"ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
			"-_").TreatAsCharacters()

	// Characters allowed in the remainder of field names
	BibTeXFieldNameCharacters.Unite(BibTeXFieldNameStarters).Unite(BibTeXNumberCharacters).TreatAsCharacters()

	// Characters allowed at the start of entry types
	BibTeXEntryTypeStarters.AddString(
		"abcdefghijklmnopqrstuvwxyz" +
			"ABCDEFGHIJKLMNOPQRSTUVWXYZ").TreatAsCharacters()

	// Characters allowed in the remainder of field names
	BibTeXEntryTypeCharacters.Unite(BibTeXFieldNameCharacters).TreatAsCharacters()
}
