package main

import (
	"bufio"
	"fmt"
	"os"
)

// Negation of ForcedXXX to trigger error message.

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
		reporting          TReporting     // Error reporting channel
	}
)

func (t *TCharacterStream) NewCharacterStream(reporting TReporting) {
	t.textfileIsOpen = false
	t.endOfStream = true
	t.runeMap = TRuneMap{}
	t.reporting = reporting
}

func (t *TCharacterStream) SetRuneMap(runeMap TRuneMap) bool {
	t.runeMap = runeMap

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

func (t *TCharacterStream) positionReportety() string {
	if t.endOfStream {
		return ""
	} else {
		return fmt.Sprintf(" (L:%d, C:%d)", t.linePosition, t.runePosition)
	}
}

func (t *TCharacterStream) ReportError(message string, context ...any) bool {
	t.reporting.Error(message+t.positionReportety(), context...)

	return false
}

func (t *TCharacterStream) ReportWarning(message string, context ...any) bool {
	t.reporting.Warning(message+t.positionReportety(), context...)

	return false
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

func (t *TCharacterStream) ForcedTextfileOpen(fileName, errorMessage string) bool {
	return t.TextfileOpen(fileName) || t.ReportError(errorMessage, fileName)
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
		t.runeString, mapped = t.runeMap[t.textRunes[t.textRunesPosition]]

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

func (t *TCharacterStream) ForcedThisCharacterWasIn(S TByteSet) bool {
	return t.ThisCharacterWasIn(S) || t.ReportError(errorCharacterNotIn, S.String())
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

func (t *TCharacterStream) SkipToCharacter(c byte) bool {
	for !t.ThisCharacterIs(c) && !t.EndOfStream() {
		t.NextCharacter()
	}

	return t.ThisCharacterIs(c)
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
