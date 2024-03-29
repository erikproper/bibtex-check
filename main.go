package main

import "fmt"
import "os"

//import "io"
import "bufio"
import "bytes"

//import "strconv"

type TLexerControl struct {
	Escapers   TByteSet
	Delimiters TByteSet
	Singletons TByteSet
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
		t.NextChar() //&&
		//t.parseCommentsEty()

	return result
}

///////////////////

type TByteSet struct {
	words        [4]uint64
	treatAsChar bool
}

func (s *TByteSet) split(b byte) (byte, uint64) {
	return b / 64, 1 << byte(b%64)
}

func ByteSet(elements ...byte) TByteSet {
	var newSet TByteSet

	newSet.add(elements)

	return newSet
}

func (s *TByteSet) TreatAsChar() *TByteSet {
	s.treatAsChar = true

	return s
}

func (s *TByteSet) add(elements []byte) *TByteSet {
	for _, element := range elements {
		word, bit := s.split(element)

		s.words[word] += bit
	}

	return s
}

func (s *TByteSet) Add(elements ...byte) *TByteSet {
	return s.add(elements)
}

func (s *TByteSet) delete(elements []byte) *TByteSet {
	for _, element := range elements {
		word, bit := s.split(element)

		s.words[word] &^= bit
	}

	return s
}

func (s *TByteSet) Delete(elements ...byte) *TByteSet {
	return s.delete(elements)
}

func (s *TByteSet) Unite(t TByteSet) *TByteSet {
	s.words[0] |= t.words[0]
	s.words[1] |= t.words[1]
	s.words[2] |= t.words[2]
	s.words[3] |= t.words[3]

	return s
}

func (s *TByteSet) Intersect(t TByteSet) *TByteSet {
	s.words[0] &= t.words[0]
	s.words[1] &= t.words[1]
	s.words[2] &= t.words[2]
	s.words[3] &= t.words[3]

	return s
}

func (s *TByteSet) Subtract(t TByteSet) *TByteSet {
	s.words[0] &^= t.words[0]
	s.words[1] &^= t.words[1]
	s.words[2] &^= t.words[2]
	s.words[3] &^= t.words[3]

	return s
}

func (s TByteSet) Eq(t TByteSet) bool {
	return s.words[0] == t.words[0] && s.words[1] == t.words[1] && s.words[2] == t.words[2] && s.words[3] == t.words[3]
}

func (s TByteSet) SubsetEq(t TByteSet) bool {
	return s.words[0]&^t.words[0] == 0 && (s.words[1]&^t.words[1] == 0) && (s.words[2]&^t.words[2] == 0) && (s.words[3]&^t.words[3] == 0)
}

func (s TByteSet) Subset(t TByteSet) bool {
	return s.SubsetEq(t) && !s.Eq(t)
}

func (s TByteSet) SupersetEq(t TByteSet) bool {
	return t.SubsetEq(s)
}

func (s TByteSet) Superset(t TByteSet) bool {
	return s.SupersetEq(t) && !s.Eq(t)
}

func (s TByteSet) Contains(elements ...byte) bool {
	var elementSet TByteSet

	elementSet.add(elements)

	return true
}


var ByteToString [255]string

func (s TByteSet) String() string {
	var buf bytes.Buffer
	var item byte

	fmt.Fprint(&buf, "{")

	for w := 0; w < 4; w++ {
		for b := 0; b < 64; b++ {
			if (s.words[w] & (1 << b)) > 0 {
				if buf.Len() > 1 {
					fmt.Fprint(&buf, " ,")
				}

				item = byte(w*64 + b)
				if s.treatAsChar {
					fmt.Fprint(&buf, "'", ByteToString[item], "'")
				} else {
					fmt.Fprintf(&buf, "%d", item)
				}
			}
		}
	}

	fmt.Fprint(&buf, "}")

	return buf.String()
}

func init() {
	for i := 0; i < 255; i++ {
		ByteToString[i] = string(i)
	}
	ByteToString['\\'] = "\\\\"
	ByteToString['\a'] = "\\a"
	ByteToString['\b'] = "\\b"
	ByteToString['\f'] = "\\f"
	ByteToString['\n'] = "\\n"
	ByteToString['\r'] = "\\r"
	ByteToString['\t'] = "\\t"
	ByteToString['\v'] = "\\v"
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

	//	bibTeX.Escape = '\\'
	//	bibTeX.CommentStart = '%'
	//	bibTeX.CommentEnd = '\n'

	//	if bibTeX.TextFileOpen("Test.bib") {
	//			for bibTeX.NextToken() {
	//				fmt.Printf("[" + string(bibTeX.ThisChar()) + "]")
	//			}
	//	}
	//
	// fmt.Printf("\n")
	// fmt.Printf("Count: " + strconv.Itoa(count) + "\n")
}
