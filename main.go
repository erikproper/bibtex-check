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

func (t *TCharacterStream) CollectCharacter(s *string) bool {
	*s += string(t.currentCharacter)

	return true
}

func (t *TCharacterStream) CollectAnyCharacterThatWas(s *string) bool {
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
	return ! t.ThisCharacterIs(c) && t.CollectCharacter(s) && t.NextCharacter()
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
	EntryStartCharacter  = '@'
	BeginGroupCharacter  = '{'
	EndGroupCharacter    = '}'
	DoubleQuoteCharacter = '"'
	PercentCharacter     = '%'
	EscapeCharacter      = '\\'
	CommentEntryType     = "comment"
	PreambleEntryType    = "preamble"
	StringEntryType      = "string"
)

type TBiBTeXNameMap = map[string]string

var (
	BiBTeXRuneMap TRuneMap

	BiBTeXTypeNameMap,
	BiBTeXFieldNameMap TBiBTeXNameMap

	BiBTeXNameCharacters,
	BiBTeXCommentStarters,
	BiBTeXCommentEnders,
	BiBTeXSpaceCharacters TByteSet
)

func NormalizeBiBTeXTypeName(name *string) {
	*name = strings.ToLower(*name)

	normalized, mapped := BiBTeXTypeNameMap[*name]

	if mapped {
		*name = normalized
	}
}

func NormalizeBiBTeXFieldName(name *string) {
	*name = strings.ToLower(*name)

	normalized, mapped := BiBTeXFieldNameMap[*name]

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
		// skip
	}

	return true
}

func (t *TBiBTeXStream) Comments() bool {
	return t.ThisCharacterWasIn(BiBTeXCommentStarters) &&
		/**/ t.CommentsClausety() &&
		/*  */ t.ThisCharacterWasIn(BiBTeXCommentEnders)
}

func (t *TBiBTeXStream) Spaces() bool {
	result := false

	for t.ThisCharacterWasIn(BiBTeXSpaceCharacters) {
		result = true
	}

	return result
}

func (t *TBiBTeXStream) MoveToToken() bool {
	for t.Spaces() || t.Comments() {
		// skip
	}

	return true
}

func (t *TBiBTeXStream) GroupedFieldElement(content *string) bool {
	switch {
	case t.CollectCharacterThatWas(BeginGroupCharacter, content):
		return t.GroupedFieldContentety(content)
			/**/ t.CollectCharacterThatWas(EndGroupCharacter, content)

	case t.CollectCharacterThatWas(EscapeCharacter, content):
		return t.CollectAnyCharacterThatWas(content)
	}
	
	return t.CollectCharacterThatWasNot(EndGroupCharacter, content)
}

func (t *TBiBTeXStream) GroupedFieldContentety(content *string) bool {
	for t.GroupedFieldElement(content) {
		// skip
	}

	return true
}

func (t *TBiBTeXStream) EntryFields(entryType string) bool {
	content := ""
	switch entryType {
	case PreambleEntryType:
		return t.GroupedFieldContentety(&content)

	case CommentEntryType:
		return t.GroupedFieldContentety(&content)

	case StringEntryType:
		t.MoveToToken()
		fmt.Println("Stringssss")
		return true

	default:
		t.MoveToToken()
		fmt.Println("General")
		return true // Key() && FieldDefinitions()
	}
}

func (t *TBiBTeXStream) EntryBodyBegin() bool {
	return t.MoveToToken() && t.ThisCharacterWas(BeginGroupCharacter)
}

func (t *TBiBTeXStream) EntryBodyEnd() bool {
	return t.MoveToToken() && t.ThisCharacterWas(EndGroupCharacter)
}

func (t *TBiBTeXStream) BiBTeXName(name *string) bool {
	result := t.MoveToToken() &&
		/*   */ t.CollectCharacterThatWasIn(BiBTeXNameCharacters, name)

	for t.CollectCharacterThatWasIn(BiBTeXNameCharacters, name) {
		// Skip
	}

	NormalizeBiBTeXTypeName(name)

	fmt.Println("[[" + *name + "]]")

	return result
}

func (t *TBiBTeXStream) EntryType(entryType *string) bool {
	return t.BiBTeXName(entryType)
}

func (t *TBiBTeXStream) EntryStarter() bool {
	return t.MoveToToken() &&
		/**/ t.ThisCharacterWas(EntryStartCharacter)
}

func (t *TBiBTeXStream) Entry() bool {
	entryType := ""

	return t.EntryStarter() &&
		/**/ t.EntryType(&entryType) &&
		/*  */ t.EntryBodyBegin() &&
		/*    */ t.EntryFields(entryType) &&
		/*      */ t.EntryBodyEnd()
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

	BiBTeXTypeNameMap = TBiBTeXNameMap{}
	BiBTeXTypeNameMap["conference"] = "inproceedings"
	BiBTeXTypeNameMap["softmisc"] = "misc"
	BiBTeXTypeNameMap["patent"] = "misc"

	//	BiBTeXFieldNameMap

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
/// eprinttype / eprint??
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