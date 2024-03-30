package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
)

//import "io"

type TLexerControl struct {
	Escapers   TByteSet
	Delimiters TByteSet
	Singletons TByteSet
}

/// Make robust when file is not found, etc

/// Positions (line + raw(!), so before CharMap, char position )

/// Reading person names:
/// - Read names file first {<name>} {<alias>}
/// - Parse name from bibtext
/// - Use normalised string representatation to lookup in a string to string map

type TRuneMap map[rune]string

type TCharStream struct {
	textFile           *os.File
	textScanner        *bufio.Scanner
	textFileIsOpen     bool
	textRunes          []rune
	textRunesPosition  int
	runeMap            TRuneMap
	runeString         string
	runeStringPosition int
	currCharacter      byte
}

func (t *TCharStream) SetRuneMap(runeMap TRuneMap) bool {
	t.runeMap = runeMap

	return true
}

func (t *TCharStream) TextFileOpen(fileName string) bool {
	var err error

	t.textFile, err = os.Open(fileName)
	t.textFileIsOpen = true

	t.textRunes = []rune{}
	t.textRunesPosition = 0

	t.runeString = ""
	t.runeStringPosition = 0

	if err == nil {
		t.textScanner = bufio.NewScanner(t.textFile)

		return true
	} else {
		return t.TextFileClose()
	}
}

func (t *TCharStream) TextFileClose() bool {
	if t.textFileIsOpen {
		err := t.textFile.Close()
		t.textFileIsOpen = false

		return err == nil
	} else {
		return false
	}
}

func (t *TCharStream) TextString(s string) bool {
	if t.textFileIsOpen {
		t.TextFileClose()
	}

	t.textRunes = []rune(s)
	t.textRunesPosition = 0

	t.runeString = ""
	t.runeStringPosition = 0

	return true
}

func (t *TCharStream) NextChar() bool {
	if t.runeStringPosition < len(t.runeString) {
		t.currCharacter = byte(t.runeString[t.runeStringPosition])
		t.runeStringPosition++

		return true
	} else if t.textRunesPosition < len(t.textRunes) {
		var mapped bool
	
		t.currCharacter = byte(t.textRunes[t.textRunesPosition])

		t.runeStringPosition = 0
		t.runeString, mapped = t.runeMap[t.textRunes[t.textRunesPosition]]

		t.textRunesPosition++

		return ! mapped || t.NextChar()
	} else if t.textFileIsOpen && t.textScanner.Scan() {
		t.currCharacter = NewlineChar

		t.textRunesPosition = 0
		t.textRunes = []rune(t.textScanner.Text())
		
		return true
	}

	return false
}

func (t *TCharStream) ThisChar() byte {
	return t.currCharacter
}

// At token level
//func (t *TCharStream) parseCommentsEty() bool {
//	for t.ThisChar() == t.CommentStart {
//		for t.ThisChar() != t.CommentEnd {
//			t.NextChar()
//			fmt.Printf("{" + string(t.ThisChar()) + "}")
//		}
//		t.NextChar()
//	}
//
//	return true
//}

//func (t *TCharStream) NextToken() bool {
//	result :=
//		t.NextChar() //&&
//		//t.parseCommentsEty()
//
//	return result
//}

//var MainStructureLexerControl, QuotedFieldLexerControl, BracketedFieldLexerControl TLexerControl

var bibTeX TCharStream
var count int

// ik denk dat ik "onderin" een "CharacterReader" maak die een string van runen inleest, en dan een "string to string" map die zaken zoals ü, é, meteen naar de juiste LaTeX zaken mapt. Die gemapte string wordt dan byte voor byte (wat dan plain ascii a-la-latex is) doorgegeven aan de lexer
// Lost meteen het probleem op dat sommige BiBTeX files (import van derden) utf8 of extended ascii zijn.

var runeMap TRuneMap

func main() {
	//	MainStructureLexerControl.Escapers.Add().TreatAsChar()
	//	MainStructureLexerControl.Delimiters.Add(' ', '\n', '\t', '\a', '\b', '\f', '\r', '\v').TreatAsChar()
	//	MainStructureLexerControl.Singletons.Add('@', '%', '{', '}', '#', '"').TreatAsChar()

	//	BracketedFieldLexerControl.Escapers.Add('\\').TreatAsChar()
	//	BracketedFieldLexerControl.Delimiters.Add(' ', '\n', '\t', '\a', '\b', '\f', '\r', '\v').TreatAsChar()
	//	BracketedFieldLexerControl.Singletons.Add('{', '}', '\\').TreatAsChar()

	//	QuotedFieldLexerControl.Escapers.Add('\\').TreatAsChar()
	//	QuotedFieldLexerControl.Delimiters.Add(' ', '\n', '\t', '\a', '\b', '\f', '\r', '\v').TreatAsChar()
	//	QuotedFieldLexerControl.Singletons.Add('{', '}', '\\', '"').TreatAsChar()

	//	fmt.Println(MainStructureLexerControl)
	//	fmt.Println(BracketedFieldLexerControl)
	//	fmt.Println(QuotedFieldLexerControl)

	//	bibTeX.Escape = '\\'
	//	bibTeX.CommentStart = '%'
	//	bibTeX.CommentEnd = '\n'

	/////// Should go into the struct definition ...
	//// so create this mapping outside, but assig it into the "object"

	runeMap = map[rune]string{
		'ü': "{\\\"u}",
		'é': "{\\'e}",
		'ñ': "{\\~n}",
	}

	fmt.Printf("Go ... \n")

	if bibTeX.TextFileOpen("Test.bib") && bibTeX.SetRuneMap(runeMap) {
		for bibTeX.NextChar() {
			count++
			fmt.Printf("[" + string(bibTeX.ThisChar()) + "]")
		}
	}
	fmt.Printf("\n")

	if bibTeX.TextString("@hallo\n ü") && bibTeX.SetRuneMap(runeMap) {
		for bibTeX.NextChar() {
			fmt.Printf("[" + string(bibTeX.ThisChar()) + "]")
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
