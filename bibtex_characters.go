//
// Module: bibtexcharacters
//
// This module defines characters and character sets that are needed for the parsing of BibTeX files.
//
// Creator: Henderik A. Proper (erikproper@fastmail.com)
//
// Version of: 24.04.2024
//

package main

// Special characters
const (
	SpaceCharacter          = ' '
	BackspaceCharacter      = '\b'
	BellCharacter           = '\a'
	CarriageReturnCharacter = '\r'
	FormFeedCharacter       = '\f'
	NewlineCharacter        = '\n'
	TabCharacter            = '\t'
	VerticalTabCharacter    = '\v'
)

// Variables that should be treated as constants
var (
	NumberCharacters,
	SpecialCharacters TByteSet
)

// Initialising the variables
func init() {
	SpecialCharacters = TByteSetNew()
	NumberCharacters = TByteSetNew()

	NumberCharacters.AddString("0123456789").TreatAsCharacters()
	SpecialCharacters.Add(
		BackspaceCharacter, BellCharacter, CarriageReturnCharacter, FormFeedCharacter,
		NewlineCharacter, TabCharacter, VerticalTabCharacter).TreatAsCharacters()
}
