package main

import (
	"bufio"
	"fmt"
	"os"
)

const errorCharacterNotIn = "expected character from %s"

type (
	TRuneMap      map[rune]string // Type for mappings from runes to TeX strings
	TCharacterMap [256]byte       // Type for mappings of characters to characters

	TCharacterStream struct {
		endOfStream        bool           // Set to true when we've reached the end of the stream
		textfile           *os.File       // The text file from which we're streaming
		textScanner        *bufio.Scanner // The scanner used to collect input from the file
		textfileIsOpen     bool           // Set to true if the text file is open
		textRunes          []rune         // The buffer of runes we're working from
		textRunesPosition  int            // The position of the current rune within the rune map
		runeMap            TRuneMap       // Map to map runes to underlying TeX strings
		runeString         string         // The string belonging to the runeMap-ed version of the current rune
		runeStringPosition int            // The position of the current character within the runeString
		currentCharacter   byte           // The actual current character
		linePosition       int            // The line within the original input, in terms of newlines
		runePosition       int            // Position within the present line within the original input
		TReporting                        // Error reporting channel
	}
)

func (c *TCharacterStream) NewCharacterStream(reporting TReporting) {
	c.textfileIsOpen = false
	c.endOfStream = true
	c.runeMap = TRuneMap{}
	c.TReporting = reporting
}

func (c *TCharacterStream) SetRuneMap(runeMap TRuneMap) bool {
	c.runeMap = runeMap

	return true
}

func (c *TCharacterStream) initializeStream(s string) {
	c.endOfStream = false

	c.textRunes = []rune(s)
	c.textRunesPosition = 0

	c.runeString = ""
	c.runeStringPosition = 0

	c.linePosition = 1
	c.runePosition = 0

	c.currentCharacter = ' '
}

func (c *TCharacterStream) positionReportety() string {
	if c.endOfStream {
		return ""
	} else {
		return fmt.Sprintf(" (L:%d, C:%d)", c.linePosition, c.runePosition)
	}
}

func (c *TCharacterStream) ReportError(message string, context ...any) bool {
	c.Error(message+c.positionReportety(), context...)

	return false
}

func (c *TCharacterStream) ReportWarning(message string, context ...any) bool {
	c.Warning(message+c.positionReportety(), context...)

	return false
}

func (c *TCharacterStream) TextfileOpen(fileName string) bool {
	var err error

	c.textfile, err = os.Open(fileName)
	c.textfileIsOpen = true

	c.initializeStream("")

	if err == nil {
		c.textScanner = bufio.NewScanner(c.textfile)

		return c.NextCharacter()
	} else {
		c.endOfStream = true

		return c.TextfileClose()
	}
}

func (c *TCharacterStream) ForcedTextfileOpen(fileName, errorMessage string) bool {
	return c.TextfileOpen(fileName) ||
		c.ReportError(errorMessage, fileName)
}

func (c *TCharacterStream) TextfileClose() bool {
	if c.textfileIsOpen {
		err := c.textfile.Close()

		c.textfileIsOpen = false
		c.endOfStream = true

		return err == nil
	} else {
		return false
	}
}

func (c *TCharacterStream) TextString(s string) bool {
	if c.textfileIsOpen {
		c.TextfileClose()
	}

	c.initializeStream(s)

	return c.NextCharacter()
}

func (c *TCharacterStream) EndOfStream() bool {
	return c.endOfStream
}

func (c *TCharacterStream) NextCharacter() bool {
	if c.endOfStream {
		return false
	} else if c.runeStringPosition < len(c.runeString) {
		c.currentCharacter = byte(c.runeString[c.runeStringPosition])
		c.runeStringPosition++

		return true
	} else if c.textRunesPosition < len(c.textRunes) {
		var mapped bool

		c.currentCharacter = byte(c.textRunes[c.textRunesPosition])

		// As we can be working with inputs from strings, newlines can occur
		// in the middle of strings. So, we need to check this to ensure the
		// positioning is right for error messages.
		c.runePosition++
		if c.currentCharacter == NewlineCharacter {
			c.linePosition++
			c.runePosition = 0
		}

		c.runeStringPosition = 0
		c.runeString, mapped = c.runeMap[c.textRunes[c.textRunesPosition]]

		c.textRunesPosition++

		return !mapped || c.NextCharacter()
	} else if c.textfileIsOpen && c.textScanner.Scan() {
		c.textRunesPosition = 0
		c.textRunes = []rune(c.textScanner.Text() + "\n")

		c.runeString = ""
		c.runeStringPosition = 0

		return c.NextCharacter()
	} else {
		c.endOfStream = true

		return false
	}
}

func (c *TCharacterStream) ThisCharacter() byte {
	return c.currentCharacter
}

func (c *TCharacterStream) AddCharacter(ch byte, s *string) bool {
	*s += string(ch)

	return true
}

func (c *TCharacterStream) CollectCharacter(s *string) bool {
	return c.AddCharacter(c.currentCharacter, s)
}

func (c *TCharacterStream) CollectCharacterThatWasThere(s *string) bool {
	return c.CollectCharacter(s) && c.NextCharacter()
}

func (c *TCharacterStream) ThisCharacterIsIn(S TByteSet) bool {
	return S.Contains(c.ThisCharacter())
}

func (c *TCharacterStream) ThisCharacterWasIn(S TByteSet) bool {
	return c.ThisCharacterIsIn(S) && c.NextCharacter()
}

func (c *TCharacterStream) CollectCharacterThatWasIn(S TByteSet, s *string) bool {
	return c.ThisCharacterIsIn(S) && c.CollectCharacter(s) && c.NextCharacter()
}

func (c *TCharacterStream) ThisCharacterWasNotIn(s TByteSet) bool {
	return (!c.ThisCharacterIsIn(s)) && c.NextCharacter()
}

func (c *TCharacterStream) CollectCharacterThatWasNot(ch byte, s *string) bool {
	return !c.ThisCharacterIs(ch) && c.CollectCharacter(s) && c.NextCharacter()
}

func (c *TCharacterStream) SkipToCharacter(ch byte) bool {
	for !c.ThisCharacterIs(ch) && !c.EndOfStream() {
		c.NextCharacter()
	}

	return c.ThisCharacterIs(ch)
}

func (c *TCharacterStream) ThisCharacterIs(ch byte) bool {
	return c.ThisCharacter() == ch
}

func (c *TCharacterStream) ThisCharacterWas(ch byte) bool {
	return c.ThisCharacterIs(ch) && c.NextCharacter()
}

func (c *TCharacterStream) CollectCharacterThatWas(ch byte, s *string) bool {
	return c.ThisCharacterIs(ch) && c.CollectCharacter(s) && c.NextCharacter()
}
