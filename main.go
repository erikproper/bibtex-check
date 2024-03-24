package main

import "fmt"
import "os"
//import "io"
import "bufio"
import "strconv"

//// Tokenizer
//// - Escapers
//// - CommentStarters
//// - CommentEnders
//// - Delimiters
//// - SingletonTokens
////
///////////////////

type Text struct {
	fileDescriptor	*os.File
	textFile 		*bufio.Reader
	currCharacter	byte

	// Tokenizing control
	Escape			byte
	CommentStart	byte
	CommentEnd		byte
	
	currToken		string
}

func (t *Text) TextFileOpen(fileName string) bool {
	var err error
	
	t.fileDescriptor, err = os.Open(fileName)
	
	if err == nil {
		t.textFile = bufio.NewReader(t.fileDescriptor)
		
		return true
	} else {
		return false
	}
}

func (t *Text) NextChar() bool {
	character, err := t.textFile.ReadByte()

	t.currCharacter = character

	return err == nil
}

/////// Needed??
func (t *Text) ThisChar() byte {
	return t.currCharacter
}

//// At token level
func (t *Text) Comments() bool {
	for t.ThisChar() == t.CommentStart {
		for t.ThisChar() != t.CommentEnd  { 
			t.NextChar() 
			fmt.Printf("{" + string(t.ThisChar()) + "}") 
		}
		t.NextChar()
	}

	return true
}

func (t *Text) NextToken() bool {
	return (
		t.NextChar() &&
		  ( t.Comments() && 
		      true ) )
}

///////////////////

func main() {
	var bibTeX Text

	bibTeX.Escape       = '\\'
	bibTeX.CommentStart = '%'
	bibTeX.CommentEnd   = '\n'
	  
	count := 0
	if bibTeX.TextFileOpen("Test.bib") {
		for bibTeX.NextToken() {
			count = count + 1
			fmt.Printf("[" + string(bibTeX.ThisChar()) + "]")
		}
	}
	
	fmt.Printf("\n")	
	fmt.Printf("Count: " + strconv.Itoa(count) + "\n")
}























