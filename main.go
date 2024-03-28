package main

import "fmt"
import "os"

//import "io"
import "bufio"

//import "strconv"

/////// PASCALIFY parameters

func IsEven(Number int) bool {
	return Number%2 == 0
}

//
// Sets of named strings
//

type TNamedStringsSet map[string]string

func (Set TNamedStringsSet) AddRange(Items []string) TNamedStringsSet {
	// Takes a set of strings as input.
	// This set should actually be pairs of strings and their names.
	// As such, it should always be an even range
	var s string
	var l int

	for i, Item := range Items {
		l = i

		if IsEven(i) {
			s = Item
		} else {
			Set[s] = Item
		}
	}

	// We should never have this situation ... can we throw an exception and bring down the app?
	if IsEven(l) {
		fmt.Println("ERROR: We are missing a name for a range of named strings: ")
		fmt.Println(" - Set  :", Set)
		fmt.Println(" - Items:", Items)
	}

	return Set
}

func NewNamedStringsSet(Items ...string) TNamedStringsSet {
	Set := TNamedStringsSet{}

	return Set.AddRange(Items)
}

func (Set TNamedStringsSet) Add(Items ...string) TNamedStringsSet {
	return Set.AddRange(Items)
}

func (Set TNamedStringsSet) Remove(Items ...string) TNamedStringsSet {
	for _, Item := range Items {
		delete(Set, Item)
	}

	return Set
}

func (Set TNamedStringsSet) Contains(Items ...string) bool {
	Contains := true
	ContainsItems := true

	for _, Item := range Items {
		_, Contains = Set[Item]

		ContainsItems = ContainsItems && Contains

		// We are done as soon as we come across an Item that is not contained in the set
		if !ContainsItems {
			return ContainsItems
		}
	}

	return ContainsItems
}

//////

type TLexerControl struct {
	Escapers   TNamedStringsSet
	Delimiters TNamedStringsSet
	Singletons TNamedStringsSet
}

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
		t.NextChar() &&
			t.parseCommentsEty()

	return result
}

///////////////////

func main() {
	var LexerControl TLexerControl
	//	var bibTeX Text
	//	var count int

	LexerControl.Escapers = NewNamedStringsSet(
		"\\", "slash")

	LexerControl.Delimiters = NewNamedStringsSet(
		" ", "space")

	LexerControl.Singletons = NewNamedStringsSet(
		"@", "at",
		"%", "percent",
		"{", "left curly bracket",
		"}", "right curly bracket",
		"\"", "double quote")

	fmt.Println(LexerControl)

	//	bibTeX.Escape = '\\'
	//	bibTeX.CommentStart = '%'
	//	bibTeX.CommentEnd = '\n'

	//	if bibTeX.TextFileOpen("Test.bib") {
	//		for bibTeX.NextToken() {
	//			count++
	//			fmt.Printf("[" + string(bibTeX.ThisChar()) + "]")
	//		}
	//	}
	//
	// fmt.Printf("\n")
	// fmt.Printf("Count: " + strconv.Itoa(count) + "\n")
}
