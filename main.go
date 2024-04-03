package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
)

//import "io"

/// 1: Positions (line + raw(!), so before CharacterMap, char position )

/// 2: Make robust when file is not found

/// 3: Typing stuff. Interfaces, etc.

/// 4: BiBTeXParser parsing

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
	t.endOfStream = true
	t.textfileIsOpen = false
	t.textRunes = []rune{}
	t.textRunesPosition = 0
	t.RuneMap = TRuneMap{}
	t.runeString = ""
	t.runeStringPosition = 0
	t.currentCharacter = ' '
	t.linePosition = 0
	t.runePosition = 0
}

func (t *TCharacterStream) SetRuneMap(RuneMap TRuneMap) bool {
	t.RuneMap = RuneMap

	return true
}

func (t *TCharacterStream) TextfileOpen(fileName string) bool {
	var err error

	t.textfile, err = os.Open(fileName)
	t.textfileIsOpen = true

	t.endOfStream = false

	t.textRunes = []rune{}
	t.textRunesPosition = 0

	t.runeString = ""
	t.runeStringPosition = 0

	t.linePosition = 1
	t.runePosition = 0

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

	t.endOfStream = false

	t.textRunes = []rune(s)
	t.textRunesPosition = 0

	t.runeString = ""
	t.runeStringPosition = 0

	t.linePosition = 1
	t.runePosition = 0

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

func (t *TCharacterStream) CollectThisCharacter(c *string) bool {
	*c += string(t.currentCharacter)

	return true
}

func (t *TCharacterStream) ThisCharacterIsIn(s TByteSet) bool {
	return s.Contains(t.ThisCharacter())
}

func (t *TCharacterStream) ThisCharacterWasIn(s TByteSet) bool {
	return t.ThisCharacterIsIn(s) && t.NextCharacter()
}

func (t *TCharacterStream) CollectCharacterThatWasIn(s TByteSet, c *string) bool {
	return t.ThisCharacterIsIn(s) && t.CollectThisCharacter(c) && t.NextCharacter()
}

func (t *TCharacterStream) ThisCharacterWasNotIn(s TByteSet) bool {
	return (!t.ThisCharacterIsIn(s)) && t.NextCharacter()
}

func (t *TCharacterStream) ThisCharacterIs(c byte) bool {
	return t.ThisCharacter() == c
}

func (t *TCharacterStream) ThisCharacterWas(c byte) bool {
	return t.ThisCharacterIs(c) && t.NextCharacter()
}

// BiBTeXParser specific

const (
	EntryStartCharacter  = '@'
	BeginGroupCharacter  = '{'
	EndGroupCharacter    = '}'
	DoubleQuoteCharacter = '"'
	PercentCharacter     = '%'
)

var RuneMap TRuneMap

type TBiBTeXParser struct {
	TCharacterStream

	CommentStarters TByteSet
	CommentEnders   TByteSet
	SpaceCharacters TByteSet
}

func (t *TBiBTeXParser) NewBiBTeXParser() {
	t.NewCharacterStream()

	t.RuneMap = BiBTeXParserRuneMap

	t.SpaceCharacters.Add(
		SpaceCharacter, NewlineCharacter, BackspaceCharacter, BellCharacter,
		CarriageReturnCharacter, FormFeedCharacter, TabCharacter,
		VerticalTabCharacter).TreatAsCharacters()

	t.CommentStarters.Add(PercentCharacter).TreatAsCharacters()

	t.CommentEnders.Add(NewlineCharacter).TreatAsCharacters()
}

func (t *TBiBTeXParser) CommentsClausety() bool {
	for t.ThisCharacterWasNotIn(t.CommentEnders) {
		// skip
	}

	return true
}

func (t *TBiBTeXParser) Comments() bool {
	return t.ThisCharacterWasIn(t.CommentStarters) &&
		t.CommentsClausety() && t.ThisCharacterWasIn(t.CommentEnders)
}

func (t *TBiBTeXParser) Spaces() bool {
	result := false
	for t.ThisCharacterWasIn(t.SpaceCharacters) {
		result = true
	}

	return result
}

func (t *TBiBTeXParser) MoveToToken() bool {
	for t.Spaces() || t.Comments() {
		// skip
	}

	return true
}

// {
func (t *TBiBTeXParser) EntryBodyBegin() bool {
	return t.MoveToToken() &&
		true
}

// }
func (t *TBiBTeXParser) EntryBodyEnd() bool {
	return t.MoveToToken() &&
		true
}

func (t *TBiBTeXParser) EntryFields() bool {
	return t.MoveToToken() &&
		true
}

func (t *TBiBTeXParser) BiBTeXParserName(name *string) bool {
	*name = ""

	result := t.MoveToToken() &&
		t.CollectCharacterThatWasIn(BiBTeXParserNameCharacters, name)

	for t.CollectCharacterThatWasIn(BiBTeXParserNameCharacters, name) {
		// Skip
	}

	fmt.Println("[[" + *name + "]]")

	return result
}

func (t *TBiBTeXParser) EntryType(entryType *string) bool {
	return t.BiBTeXParserName(entryType)
}

func (t *TBiBTeXParser) EntryStarter() bool {
	return t.MoveToToken() &&
		t.ThisCharacterWas(EntryStartCharacter)
}

func (t *TBiBTeXParser) Entry() bool {
	var entryType string

	return t.EntryStarter() &&
		t.EntryType(&entryType) &&
		t.EntryBodyBegin() &&
		t.EntryFields() &&
		t.EntryBodyEnd()
}

func (t *TBiBTeXParser) Entriesety() bool {
	return (t.Entry() && t.Entriesety()) || true
}

func (t *TBiBTeXParser) OK(p string) bool {
	fmt.Println("OK: [", string(t.ThisCharacter()), "] ", p)

	return true
}

var BiBTeXParser TBiBTeXParser
var count int
var BiBTeXParserNameCharacters TByteSet
var BiBTeXParserRuneMap TRuneMap

func main() {
	// Should move into a settings file.
	// Settings should be an environment variable ...
	// see https://gobyexample.com/environment-variables
	// If settings file does not exist, then create one and push this as default into it.
	BiBTeXParserRuneMap = TRuneMap{
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

	// Should go into the init for the parser package

	BiBTeXParser.NewBiBTeXParser()

	BiBTeXParserNameCharacters.AddString(
		"abcdefghijklmnopqrstuvwxyz" +
			"ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
			"0123456789" +
			"-_:/").TreatAsCharacters()

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
