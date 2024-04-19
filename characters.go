package main

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

var (
	SpecialCharacters,
	NumberCharacters,
	UpperCaseASCIILetterCharacters,
	LowerCaseASCIILetterCharacters TByteSet
)

func init() {
	SpecialCharacters = TByteSet{}
	NumberCharacters = TByteSet{}
	UpperCaseASCIILetterCharacters = TByteSet{}
	LowerCaseASCIILetterCharacters = TByteSet{}

	NumberCharacters.AddString("0123456789").TreatAsCharacters()
	UpperCaseASCIILetterCharacters.AddString("ABCDEFGHIJKLMNOPQRSTUVWXYZ").TreatAsCharacters()
	LowerCaseASCIILetterCharacters.AddString("abcdefghijklmnopqrstuvwxyz").TreatAsCharacters()
	SpecialCharacters.Add(
		BackspaceCharacter, BellCharacter, CarriageReturnCharacter, FormFeedCharacter,
		NewlineCharacter, TabCharacter, VerticalTabCharacter).TreatAsCharacters()
}
