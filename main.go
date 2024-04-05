package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

//import "io"

/// Initial BiBTeX file parsing
/// String definitions via map ... initialise that one for each parse round ... also default strings for e.g. dates
/// Enable logging/error reporting
/// Error messages for parser "ForcedXXXX"
/// Add comments ... also to the already splitted files.
/// Split files
/// Make things robust and reporting when file is not found
/// Generate MetaData folder

/// Reading person names:
/// - Read names file first {<name>} {<alias>}
/// -  name from bibtext
/// - Use normalised string representatation to lookup in a string to string map

type TRuneMap map[rune]string
type TCharacterMap [256]byte

type TCharacterStream struct {
	endOfStream        bool
	textfile           *os.File
	textScanner        *bufio.Scanner
	textfileIsOpen     bool
	textRunes          []rune
	textRunesPosition  int
	RuneMap            TRuneMap
	runeString         string
	runeStringPosition int
	currentCharacter   byte
	linePosition       int
	runePosition       int
}

func (t *TCharacterStream) NewCharacterStream() {
	t.textfileIsOpen = false
	t.endOfStream = true
	t.RuneMap = TRuneMap{}
}

func (t *TCharacterStream) SetRuneMap(RuneMap TRuneMap) bool {
	t.RuneMap = RuneMap

	return true
}

func (t *TCharacterStream) initializeStream(s string) {
	t.endOfStream = false

	t.textRunes = []rune(s)
	t.textRunesPosition = 0

	t.runeString = ""
	t.runeStringPosition = 0

	t.linePosition = 1
	t.runePosition = 0

	t.currentCharacter = ' '
}

func (t *TCharacterStream) TextfileOpen(fileName string) bool {
	var err error

	t.textfile, err = os.Open(fileName)
	t.textfileIsOpen = true

	t.initializeStream("")

	if err == nil {
		t.textScanner = bufio.NewScanner(t.textfile)

		return t.NextCharacter()
	} else {
		t.endOfStream = true

		return t.TextfileClose()
	}
}

func (t *TCharacterStream) TextfileClose() bool {
	if t.textfileIsOpen {
		err := t.textfile.Close()

		t.textfileIsOpen = false
		t.endOfStream = true

		return err == nil
	} else {
		return false
	}
}

func (t *TCharacterStream) TextString(s string) bool {
	if t.textfileIsOpen {
		t.TextfileClose()
	}

	t.initializeStream(s)

	return t.NextCharacter()
}

func (t *TCharacterStream) EndOfStream() bool {
	return t.endOfStream
}

func (t *TCharacterStream) NextCharacter() bool {
	if t.endOfStream {
		return false
	} else if t.runeStringPosition < len(t.runeString) {
		t.currentCharacter = byte(t.runeString[t.runeStringPosition])
		t.runeStringPosition++

		return true
	} else if t.textRunesPosition < len(t.textRunes) {
		var mapped bool

		t.currentCharacter = byte(t.textRunes[t.textRunesPosition])

		// As we can be working with inputs from strings, newlines can occur
		// in the middle of strings. So, we need to check this to ensure the
		// positioning is right for error messages.
		t.runePosition++
		if t.currentCharacter == NewlineCharacter {
			t.linePosition++
			t.runePosition = 0
		}

		t.runeStringPosition = 0
		t.runeString, mapped = t.RuneMap[t.textRunes[t.textRunesPosition]]

		t.textRunesPosition++

		return !mapped || t.NextCharacter()
	} else if t.textfileIsOpen && t.textScanner.Scan() {
		t.textRunesPosition = 0
		t.textRunes = []rune(t.textScanner.Text() + "\n")

		t.runeString = ""
		t.runeStringPosition = 0

		return t.NextCharacter()
	} else {
		t.endOfStream = true

		return false
	}
}

func (t *TCharacterStream) ThisCharacter() byte {
	return t.currentCharacter
}

func (t *TCharacterStream) AddCharacter(c byte, s *string) bool {
	*s += string(c)

	return true
}

func (t *TCharacterStream) CollectCharacter(s *string) bool {
	return t.AddCharacter(t.currentCharacter, s)
}

func (t *TCharacterStream) CollectCharacterThatWasThere(s *string) bool {
	return t.CollectCharacter(s) && t.NextCharacter()
}

func (t *TCharacterStream) ThisCharacterIsIn(S TByteSet) bool {
	return S.Contains(t.ThisCharacter())
}

func (t *TCharacterStream) ThisCharacterWasIn(S TByteSet) bool {
	return t.ThisCharacterIsIn(S) && t.NextCharacter()
}

func (t *TCharacterStream) CollectCharacterThatWasIn(S TByteSet, s *string) bool {
	return t.ThisCharacterIsIn(S) && t.CollectCharacter(s) && t.NextCharacter()
}

func (t *TCharacterStream) ThisCharacterWasNotIn(s TByteSet) bool {
	return (!t.ThisCharacterIsIn(s)) && t.NextCharacter()
}

func (t *TCharacterStream) CollectCharacterThatWasNot(c byte, s *string) bool {
	return !t.ThisCharacterIs(c) && t.CollectCharacter(s) && t.NextCharacter()
}

func (t *TCharacterStream) ThisCharacterIs(c byte) bool {
	return t.ThisCharacter() == c
}

func (t *TCharacterStream) ThisCharacterWas(c byte) bool {
	return t.ThisCharacterIs(c) && t.NextCharacter()
}

func (t *TCharacterStream) CollectCharacterThatWas(c byte, s *string) bool {
	return t.ThisCharacterIs(c) && t.CollectCharacter(s) && t.NextCharacter()
}

// BiBTeXParser specific

type TBiBTeXStream struct {
	TCharacterStream
}

const (
	TeXMode               = true
	EntryStartCharacter   = '@'
	BeginGroupCharacter   = '{'
	EndGroupCharacter     = '}'
	DoubleQuotesCharacter = '"'
	AssignmentCharacter   = '='
	AdditionCharacter     = '#'
	PercentCharacter      = '%'
	CommaCharacter        = ','
	EscapeCharacter       = '\\'
	CommentEntryType      = "comment"
	PreambleEntryType     = "preamble"
	StringEntryType       = "string"
)

type TBiBTeXNameMap = map[string]string

var (
	BiBTeXRuneMap TRuneMap

	BiBTeXEntryTypeNameMap,
	BiBTeXFieldTypeNameMap TBiBTeXNameMap

	BiBTeXNameCharacters,
	BiBTeXCommentStarters,
	BiBTeXCommentEnders,
	BiBTeXSpaceCharacters TByteSet
)

func NormalizeEntryTypeName(name *string) {
	*name = strings.ToLower(*name)

	normalized, mapped := BiBTeXEntryTypeNameMap[*name]

	if mapped {
		*name = normalized
	}
}

func NormalizeFieldTypeName(name *string) {
	*name = strings.ToLower(*name)

	normalized, mapped := BiBTeXFieldTypeNameMap[*name]

	if mapped {
		*name = normalized
	}
}

func (t *TBiBTeXStream) NewBiBTeXParser() {
	t.NewCharacterStream()

	t.RuneMap = BiBTeXRuneMap
}

func (t *TBiBTeXStream) CommentsClausety() bool {
	for t.ThisCharacterWasNotIn(BiBTeXCommentEnders) {
		// Skip
	}

	return true
}

func (t *TBiBTeXStream) Comments() bool {
	return t.ThisCharacterWasIn(BiBTeXCommentStarters) &&
		/**/ t.CommentsClausety() &&
		/*  */ t.ThisCharacterWasIn(BiBTeXCommentEnders)
}

func (t *TBiBTeXStream) TeXSpaces() bool {
	result := false

	for t.ThisCharacterWasIn(BiBTeXSpaceCharacters) {
		result = true
	}

	return result
}

func (t *TBiBTeXStream) MoveToToken() bool {
	for t.TeXSpaces() || t.Comments() {
		// Skip
	}

	return true
}

func (t *TBiBTeXStream) CharacterOfNextTokenWas(character byte) bool {
	return t.MoveToToken() &&
		/**/ t.ThisCharacterWas(character)
}

func (t *TBiBTeXStream) CollectCharacterOfNextTokenThatWasIn(characters TByteSet, s *string) bool {
	return t.MoveToToken() &&
		/**/ t.CollectCharacterThatWasIn(characters, s)
}

func (t *TBiBTeXStream) GroupedFieldElement(groupEndCharacter byte, inTeXMode bool, content *string) bool {
	switch {
	case t.CollectCharacterThatWas(BeginGroupCharacter, content):
		return t.GroupedFieldContentety(EndGroupCharacter, inTeXMode, content)
		/*    */ t.CollectCharacterThatWas(EndGroupCharacter, content)

	case t.CollectCharacterThatWas(EscapeCharacter, content):
		return t.CollectCharacterThatWasThere(content)

	case inTeXMode && t.TeXSpaces():
		return t.AddCharacter(SpaceCharacter, content)
	}

	return t.CollectCharacterThatWasNot(groupEndCharacter, content)
}

func (t *TBiBTeXStream) GroupedFieldContentety(groupEndCharacter byte, inTeXMode bool, content *string) bool {
	for t.GroupedFieldElement(groupEndCharacter, inTeXMode, content) {
		// Skip
	}

	return true
}

func (t *TBiBTeXStream) EntryType(entryType *string) bool {
	result := t.CollectCharacterOfNextTokenThatWasIn(BiBTeXNameCharacters, entryType)

	for t.CollectCharacterThatWasIn(BiBTeXNameCharacters, entryType) {
		// Skip
	}

	NormalizeEntryTypeName(entryType)

	fmt.Println("[E[" + *entryType + "]]")

	return result
}

func (t *TBiBTeXStream) FieldType(fieldType *string) bool {
	result := t.CollectCharacterOfNextTokenThatWasIn(BiBTeXNameCharacters, fieldType)

	for t.CollectCharacterThatWasIn(BiBTeXNameCharacters, fieldType) {
		// Skip
	}

	NormalizeFieldTypeName(fieldType)

	return result
}

// //// Then allow for # between values ... .... not after values.
// //// Then add semantics to strings
// //// Then keys on regular entries, and, indeed, parse the rest of the regular entries.
// //// Then create a {Field,Entry}Admin using byte, with (1) defaults (+ named/constant identifiers) based on the pre-defined types, and (2) allow for aliases

func (t *TBiBTeXStream) RecordFieldAssignment(fieldType, fieldValue string) bool {
	fmt.Println("[F[" + fieldType + "={" + fieldValue + "}]")

	return true
}

func (t *TBiBTeXStream) StringReference(fieldValue *string) bool {
	stringName := ""

	*fieldValue += "@STRING"

	return t.FieldType(&stringName)
}

func (t *TBiBTeXStream) FieldValueAdditionety(fieldValue *string) bool {
	switch {

	case t.CharacterOfNextTokenWas(AdditionCharacter):
		return t.CharacterOfNextTokenWas(AdditionCharacter)

	default:
		return true

	}
}

func (t *TBiBTeXStream) FieldValue(fieldValue *string) bool {
	switch {

	case t.CharacterOfNextTokenWas(BeginGroupCharacter):
		return t.GroupedFieldContentety(EndGroupCharacter, TeXMode, fieldValue) &&
			/* */ t.CharacterOfNextTokenWas(EndGroupCharacter)

	case t.CharacterOfNextTokenWas(DoubleQuotesCharacter):
		return t.GroupedFieldContentety(DoubleQuotesCharacter, TeXMode, fieldValue) &&
			/* */ t.CharacterOfNextTokenWas(DoubleQuotesCharacter)

	default:
		return t.StringReference(fieldValue)

	}

	return false
}

func (t *TBiBTeXStream) FieldDefinition() bool {
	fieldType := ""
	fieldValue := ""

	return t.FieldType(&fieldType) &&
		/**/ t.CharacterOfNextTokenWas(AssignmentCharacter) &&
		/*  */ t.FieldValue(&fieldValue) &&
		/*    */ t.FieldValueAdditionety(&fieldValue) &&
		/*      */ t.RecordFieldAssignment(fieldType, fieldValue)
}

func (t *TBiBTeXStream) FieldDefinitionsety() bool {
	t.FieldDefinition()

	for t.CharacterOfNextTokenWas(CommaCharacter) && t.FieldDefinition() {
		// Skip
	}

	t.CharacterOfNextTokenWas(CommaCharacter)

	return true
}

func (t *TBiBTeXStream) EntryBodyProper(entryType string) bool {
	content := ""

	switch entryType {
	case PreambleEntryType:
		return t.GroupedFieldContentety(EndGroupCharacter, TeXMode, &content)

	case CommentEntryType:
		return t.GroupedFieldContentety(EndGroupCharacter, !TeXMode, &content)

	case StringEntryType:
		return t.FieldDefinitionsety()

	default:
		t.MoveToToken()
		fmt.Println("General")
		return true // Key() && FieldDefinitions()
	}
}

func (t *TBiBTeXStream) Entry() bool {
	entryType := ""

	return t.CharacterOfNextTokenWas(EntryStartCharacter) &&
		/**/ t.EntryType(&entryType) &&
		/*  */ t.CharacterOfNextTokenWas(BeginGroupCharacter) &&
		/*    */ t.EntryBodyProper(entryType) &&
		/*      */ t.CharacterOfNextTokenWas(EndGroupCharacter)
}

func (t *TBiBTeXStream) Entriesety() bool {
	return (t.Entry() && t.Entriesety()) || true
}

func (t *TBiBTeXStream) OK(p string) bool {
	fmt.Println("OK: [", string(t.ThisCharacter()), "] ", p)

	return true
}

var BiBTeXParser TBiBTeXStream
var count int

func main() {
	// Should move into a settings file.
	// Settings should be an environment variable ...
	// see https://gobyexample.com/environment-variables
	// If settings file does not exist, then create one and push this as default into it.
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

	BiBTeXEntryTypeNameMap = TBiBTeXNameMap{}
	BiBTeXEntryTypeNameMap["conference"] = "inproceedings"
	BiBTeXEntryTypeNameMap["softmisc"] = "misc"
	BiBTeXEntryTypeNameMap["patent"] = "misc"

	//	BiBTeXFieldTypeNameMap

	BiBTeXParser.NewBiBTeXParser()

	BiBTeXSpaceCharacters.Add(
		SpaceCharacter, NewlineCharacter, BackspaceCharacter, BellCharacter,
		CarriageReturnCharacter, FormFeedCharacter, TabCharacter,
		VerticalTabCharacter).TreatAsCharacters()

	BiBTeXCommentStarters.Add(PercentCharacter).TreatAsCharacters()

	BiBTeXCommentEnders.Add(NewlineCharacter).TreatAsCharacters()

	BiBTeXNameCharacters.AddString(
		"abcdefghijklmnopqrstuvwxyz" +
			"ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
			"0123456789" +
			"-_:/").TreatAsCharacters()

	// End of Init

	if BiBTeXParser.TextfileOpen("Test.bib") {
		for BiBTeXParser.Entriesety() &&
			!BiBTeXParser.EndOfStream() {
			count++
			fmt.Print("[" + string(BiBTeXParser.ThisCharacter()) + "]")
			BiBTeXParser.NextCharacter()
		}
	}
	fmt.Printf("\n")

	fmt.Printf("Count: " + strconv.Itoa(count) + "\n")

	//			fmt.Print("[" + runeToString(runes[i]) + "]")

	// log import ...
	//
	//	log.Fatal(err)
}

//  { address
//    author
//    booktitle
//    chapter
//    doi
//    edition
//    editor
//    howpublished
//    institution
//    isbn
//    issn
//    journal
//    eprinttype
//    eprint??
//    key
//    month
//    note
//    number
//    occasion
//    organization
//    pages
//    publisher
//    school
//    series
//    title
//    type
//    url
//    volume
//    year
//
