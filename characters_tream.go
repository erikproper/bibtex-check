//
// Module: character_stream
//
// This module is concerned with the (ASCII) character by character reading of files or strings, to enable the further parsing of the character stream.
// As we are in a TeX environment, and may potentially be confronted with "Runes" in general, this involves an automatic conversion of runes to LaTeX symbols.
//
// Creator: Henderik A. Proper (erikproper@gmail.com)
//
// Version of: 24.04.2024
//

package main

import (
	"bufio"
	"fmt"
	"os"
)

const (
	errorCharacterNotIn = "expected character from %s"
)

type (
	TRuneMap      map[rune]string // Type for mappings from runes to TeX strings
	TCharacterMap [256]byte       // Type for mappings of characters to characters

	// The definition of the actual character stream type
	TCharacterStream struct {
		TInteraction                      // Reporting of errors and warnings.
		endOfStream        bool           // Set to true when we've reached the end of the stream
		textfile           *os.File       // The text file from which we're streaming
		textScanner        *bufio.Scanner // The scanner used to collect input from the file
		textfileIsOpen     bool           // Set to true if the text file is open
		textRunes          []rune         // The buffer of runes we're working from
		textRunesPosition  int            // The position of the current rune within the rune map
		runeMap            TRuneMap       // Map to translate runes to underlying (TeX) strings
		runeString         string         // The string belonging to the runeMap-ed version of the current rune
		runeStringPosition int            // The position of the current character within the runeString
		currentCharacter   byte           // The actual current character
		linePosition       int            // The line within the original input, in terms of newlines
		runePosition       int            // Position within the present line within the original input
	}
	// Notes:
	// - We can be creating the character stream from a file, or from a string of textRunes.
	//   In the former case, textfile, textScanner, textfileIsOpen are used to manage the file, while textRunes is used as a reading buffer.
	//   In the latter case, textRunes is used to contain the string of textRunes, which may actually span multiple (\n separated) lines.
	//
	// - Where the textRunes slice contains the runes from the source file/string, the runeString is the actual sequence of characters.
	//   The latter may involve the possible translation of non-ASCII characters to TeX commands.
	//   The actual character stream is then based on this runeString.
	//
	// - The textRunesPosition and runeStringPosition variables are used to progress through the textRunes and runeString buffers.
	//   In contrast, the linePosition and runePosition are used to administer the reading position in terms of actual (\n separated) lines in the input.
	//   As mentioned above, when reading the character stream from a provided string, this string may contain multiple (\n separated) lines.
	//   As a result, the runePosition may not be the same as the textRunesPosition.
	//   Hence the need for this "double" administration.
)

// Basic initialisation
func (c *TCharacterStream) Initialise(reporting TInteraction) {
	c.textfileIsOpen = false
	c.endOfStream = true
	c.runeMap = TRuneMap{}
	c.TInteraction = reporting
}

// Set the translation table to map runes to TeX strings
func (c *TCharacterStream) SetRuneMap(runeMap TRuneMap) bool {
	c.runeMap = runeMap

	return true
}

// Internal function to initialise the character stream, after "opening" it.
func (c *TCharacterStream) initializeStream(s string) {
	c.endOfStream = false

	// Set the initial state of the textRunes buffer.
	// When reading the stream from a file, then s will be empty.
	// However, when reading the stream from a string, then s will need to contain that string.
	c.textRunes = []rune(s)
	c.textRunesPosition = 0

	c.runeString = ""
	c.runeStringPosition = 0

	c.linePosition = 1
	c.runePosition = 0

	// We must specify an initial character, so let's use a space.
	c.currentCharacter = ' '
}

// Enable the parser to create error messages.
func (c *TCharacterStream) ReportError(message string, context ...any) bool {
	c.Error(message+c.positionReportety(), context...)

	return false
}

// Enable the parser to issue warnings.
func (c *TCharacterStream) ReportWarning(message string, context ...any) bool {
	c.Warning(message+c.positionReportety(), context...)

	return false
}

// Enable the parser to report progress.
func (c *TCharacterStream) ReportProgress(message string, context ...any) bool {
	c.Progress(message+c.positionReportety(), context...)

	return false
}

// When the parser needs to report an error, or warning, we will try to include the position within the original file/string that is being parsed.
func (c *TCharacterStream) positionReportety() string {
	if c.endOfStream {
		return ""
	} else {
		return fmt.Sprintf(" (L:%d, C:%d)", c.linePosition, c.runePosition)
	}
}

// Close the opened textfile, if needed.
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

// Opening a textfile as character stream.
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

// Open a textfile, and if this fails, report an error.
// The "Forced" prefix follows the convention as used in the actual parser, when expecting the presence of a given (non)terminal of grammar.
func (c *TCharacterStream) ForcedTextfileOpen(fileName, errorMessage string) bool {
	return c.TextfileOpen(fileName) ||
		c.ReportError(errorMessage, fileName)
}

// Use the provided string as the base for the character stream
func (c *TCharacterStream) TextString(s string) bool {
	if c.textfileIsOpen {
		c.TextfileClose()
	}

	c.initializeStream(s)

	return c.NextCharacter()
}

// Returns the status of the stream.
func (c *TCharacterStream) EndOfStream() bool {
	return c.endOfStream
}

// Now getting to the heart of things.
// The NextCharacter function moves to the next character in the character stream.
// The function returns true, if we managed to find a next character.
func (c *TCharacterStream) NextCharacter() bool {
	if c.endOfStream {
		// If we have reached the end of the stream, we need to return false
		return false
	} else if c.runeStringPosition < len(c.runeString) {
		// If there are characters left in the current runeString, then we can move to the next character in the runeString.
		c.currentCharacter = byte(c.runeString[c.runeStringPosition])
		c.runeStringPosition++

		return true
	} else if c.textRunesPosition < len(c.textRunes) {
		// In this situation, there are no further characters left in the runeString.
		// However, we still have runes left in the textRunes buffer.
		// So we need to grab the next rune(!) from there, and possibly re-fill the runeString buffer with the character representation of that rune (when provided)

		// We start by assuming that the next rune is actually a byte-based character
		c.currentCharacter = byte(c.textRunes[c.textRunesPosition])

		// As we can be working with inputs from strings, newlines can occur in the middle of strings.
		// So, we need to check this to ensure the positioning is right for error/warning/progress messages.
		c.runePosition++
		if c.currentCharacter == NewlineCharacter {
			c.linePosition++
			c.runePosition = 0
		}

		// Possibly fill the runeString based on the currently focussed rune at its translation to characters.
		c.runeStringPosition = 0
		var mapped bool
		c.runeString, mapped = c.runeMap[c.textRunes[c.textRunesPosition]]

		// Now already move the textRunesPosition to the next rune
		c.textRunesPosition++

		// If no translation of the current rune was provided, the runeString will be empty.
		// In that case, the rune we just read as byte into the currentCharacter is indeed taken as the currentCharacter.
		// If a translation of the current rune was provided, we now need to call NextCharacter again, to get the first character in the runeString.
		return !mapped || c.NextCharacter()
	} else if c.textfileIsOpen && c.textScanner.Scan() {
		// In this case, we have reached the end of the present textRunes buffer.
		// When we're streaming from a file, then we can now try and read the next line of textRunes.
		c.textRunesPosition = 0
		c.textRunes = []rune(c.textScanner.Text() + "\n")
		// Note that we need to add the newline (\n) to also pass this on to the character streaming.

		// Also reset the runeString
		c.runeString = ""
		c.runeStringPosition = 0

		// Now we call NextCharacter again, to indeed select the new current character.
		return c.NextCharacter()
	} else {
		// If we reach this point, we have nothing more to stream.
		// So ... end of stream
		c.endOfStream = true

		return false
	}
}

// Returns the currently selected character.
func (c *TCharacterStream) ThisCharacter() byte {
	return c.currentCharacter
}

// Adds the provided character to the provided string.
func (c *TCharacterStream) AddCharacter(ch byte, s *string) bool {
	*s += string(ch)

	return true
}

// Adds the currently selected character to the providing string.
func (c *TCharacterStream) CollectCharacter(s *string) bool {
	return c.AddCharacter(c.currentCharacter, s)
}

// Adds the currently selected character to the providing string, and moves to the next character.
func (c *TCharacterStream) CollectCharacterThatWasThere(s *string) bool {
	return c.CollectCharacter(s) && c.NextCharacter()
}

// Tests if the currently selected character is (as byte) the provided character.
func (c *TCharacterStream) ThisCharacterIs(ch byte) bool {
	return c.currentCharacter == ch
}

// Tests if the currently selected character is (as byte) the provided character, and if so, moves to the next character.
func (c *TCharacterStream) ThisCharacterWas(ch byte) bool {
	return c.ThisCharacterIs(ch) && c.NextCharacter()
}

// Tests if the currently selected character is (as byte) the provided character, and if so, adds it to the provided string as well as moves to the next character.
func (c *TCharacterStream) CollectCharacterThatWas(ch byte, s *string) bool {
	return c.ThisCharacterIs(ch) && c.CollectCharacter(s) && c.NextCharacter()
}

// Tests if the currently selected character (as byte) is NOT in the provided byte set, and if so, adds it to the provided string as well as moves to the next character.
func (c *TCharacterStream) CollectCharacterThatWasNot(ch byte, s *string) bool {
	return !c.ThisCharacterIs(ch) && c.CollectCharacter(s) && c.NextCharacter()
}

// Tests if the currently selected character (as byte) is in the provided byte set
func (c *TCharacterStream) ThisCharacterIsIn(S TByteSet) bool {
	return S.Contains(c.ThisCharacter())
}

// Tests if the currently selected character (as byte) is in the provided byte set, and if so, moves to the next character.
func (c *TCharacterStream) ThisCharacterWasIn(S TByteSet) bool {
	return c.ThisCharacterIsIn(S) && c.NextCharacter()
}

// Tests if the currently selected character (as byte) is in the provided byte set, and if so, adds it to the provided string as well as moves to the next character.
func (c *TCharacterStream) CollectCharacterThatWasIn(S TByteSet, s *string) bool {
	return c.ThisCharacterIsIn(S) && c.CollectCharacter(s) && c.NextCharacter()
}

// Tests if the currently selected character (as byte) is NOT in the provided byte set, and if so, moves to the next character.
func (c *TCharacterStream) ThisCharacterWasNotIn(s TByteSet) bool {
	return (!c.ThisCharacterIsIn(s)) && c.NextCharacter()
}

// Ignores all characters. until we arrive at the specified character.
func (c *TCharacterStream) SkipToCharacter(ch byte) bool {
	for !c.ThisCharacterIs(ch) && !c.EndOfStream() {
		c.NextCharacter()
	}

	return c.ThisCharacterIs(ch)
}
