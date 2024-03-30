package main

import "fmt"
import "os"
import "bufio"

//import "io"
//import "strconv"

type TLexerControl struct {
	Escapers   TByteSet
	Delimiters TByteSet
	Singletons TByteSet
}

/// Make robust when file is not found, etc

/// CharMap from bytes to string
/// Initialise as map[i] = string(i)
/// CurrChar is based on Byte and Index in string

/// Positions (line + raw(!), so before CharMap, char position )

type Text struct {
	fileDescriptor *os.File
	textFile       *bufio.Reader
	currCharacter  byte

	// Tokenizing control
	Escape       byte
	CommentStart byte
	CommentEnd   byte

	currToken string
}

func (t *Text) TextFileOpen(fileName string) bool {
	var err error

	t.fileDescriptor, err = os.Open(fileName)

	if err == nil {
		t.textFile = bufio.NewReader(t.fileDescriptor)

		return true
	} else {
		// Make robust by setting FileIsOpen and blocking ReadBytes

		return false
	}
}

func (t *Text) NextChar() bool {
	character, err := t.textFile.ReadByte()

	t.currCharacter = character

	return err == nil
}

// Needed??
func (t *Text) ThisChar() byte {
	return t.currCharacter
}

// At token level
func (t *Text) parseCommentsEty() bool {
	for t.ThisChar() == t.CommentStart {
		for t.ThisChar() != t.CommentEnd {
			t.NextChar()
			fmt.Printf("{" + string(t.ThisChar()) + "}")
		}
		t.NextChar()
	}

	return true
}

func (t *Text) NextToken() bool {
	result :=
		t.NextChar() //&&
		//t.parseCommentsEty()

	return result
}

var MainStructureLexerControl, QuotedFieldLexerControl, BracketedFieldLexerControl TLexerControl

func main() {
	MainStructureLexerControl.Escapers.Add().TreatAsChar()
	MainStructureLexerControl.Delimiters.Add(' ', '\n', '\t', '\a', '\b', '\f', '\r', '\v').TreatAsChar()
	MainStructureLexerControl.Singletons.Add('@', '%', '{', '}', '#', '"').TreatAsChar()

	BracketedFieldLexerControl.Escapers.Add('\\').TreatAsChar()
	BracketedFieldLexerControl.Delimiters.Add(' ', '\n', '\t', '\a', '\b', '\f', '\r', '\v').TreatAsChar()
	BracketedFieldLexerControl.Singletons.Add('{', '}', '\\').TreatAsChar()

	QuotedFieldLexerControl.Escapers.Add('\\').TreatAsChar()
	QuotedFieldLexerControl.Delimiters.Add(' ', '\n', '\t', '\a', '\b', '\f', '\r', '\v').TreatAsChar()
	QuotedFieldLexerControl.Singletons.Add('{', '}', '\\', '"').TreatAsChar()

	fmt.Println(MainStructureLexerControl)
	fmt.Println(BracketedFieldLexerControl)
	fmt.Println(QuotedFieldLexerControl)

		bibTeX.Escape = '\\'
		bibTeX.CommentStart = '%'
		bibTeX.CommentEnd = '\n'

		if bibTeX.TextFileOpen("Test.bib") {
				for bibTeX.NextToken() {
					fmt.Printf("[" + string(bibTeX.ThisChar()) + "]")
				}
		}
	
	 fmt.Printf("\n")
	 fmt.Printf("Count: " + strconv.Itoa(count) + "\n")
}
