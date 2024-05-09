package main

import "fmt"
import "testing"
import "strings"

var TeXSpaces,
	TeXDelimiters TByteSet

type TBibTeXNames struct {
		TCharacterStream                     // The underlying stream of characters.
		library              *TBibTeXLibrary // The BibTeX Library this parser will contribute to.
	}

func (t *TBibTeXNames) Show() bool {
	fmt.Print("(", string(t.ThisCharacter()), ")")

	return true
}

func (t *TBibTeXNames) Ping(s string) bool {
	fmt.Println("P[", s, "]")

	return true
}

func (t *TBibTeXNames) TeXSpacety() bool {
	for t.ThisCharacterWasIn(TeXSpaces) {
		// Skip
	}
	
	return true
}	

func (t *TBibTeXNames) CollectTeXSpacety(s *string) bool {
	if t.ThisCharacterWasIn(TeXSpaces) {
		*s += " "
	}

	return t.TeXSpacety()
}

func (t *TBibTeXNames) CollectTeXTokenElement(s *string) bool {
	switch {

	case t.CollectCharacterThatWas('{', s):
		for t.CollectTeXSpacety(s) && t.CollectTeXTokenElement(s) {
			// Skip
		}		
		return t.CollectCharacterThatWas('}', s)

	case t.CollectCharacterThatWas('\\', s):
		return t.CollectCharacterThatWasThere(s)

	}

	return t.CollectCharacterThatWasNotIn(TeXDelimiters, s)
}

func (t *TBibTeXNames) CollectTeXToken(s *string) bool {	
	for t.CollectTeXTokenElement(s) {
		// Skip
	}

	return true
}

func (t *TBibTeXNames) NormaliseNameSequencety() string {
	names := ""
	name := ""
	token := ""
	andety := ""
	spacety := ""

	t.TeXSpacety()
	for t.CollectTeXSpacety(&spacety) && t.CollectTeXToken(&token) && !t.EndOfStream() {
		if token != "" {
			if strings.ToLower(token) == "and" {
				if name != "" {
					names += andety + t.library.NormalisePersonName(name)
					name = ""
					token = ""
					andety = " #and# "

					t.TeXSpacety()
				}
			} else {
				name += spacety + token
				token = ""
			}
		}
		spacety = ""
	}
				
	return names
}

// Opening a string with BibTeX entries, and then parse it (and add it to the selected Library.)
func (t *TBibTeXNames) NormaliseNamesString(names string) string {
	
	t.TextString(names)
	
	return t.NormaliseNameSequencety()
}

func TestStringMaps(t *testing.T) {
	TeXSpaces.AddString(" ").TreatAsCharacters()
	TeXDelimiters.AddString("{}").Unite(TeXSpaces).TreatAsCharacters()

	Library = TBibTeXLibrary{}
	InitialiseMainLibrary()
	OpenMainBibFile()
	
	tt := TBibTeXNames{}
	tt.library = &Library
	fmt.Println(tt.NormaliseNamesString(" {Petrevska Nechkoska}, Renata and Mart{\\'i}n, D. AND P{ie rre}, \\hello\\there\\{     AND {h i}  "))

	//	fmt.Println(normalisePagesValue(&Library, "1:1--1:8, 3:2, 4-10"))
	//	fmt.Println(normalisePagesValue(&Library, "1:1--2:8"))
	//	fmt.Println(normalisePagesValue(&Library, "1:1---2:8"))

	//	strings.TrimSpace
	// Play
	// TITLES
	// Macro calls always protected.
	// { => nest
	// \ => in macro name to next space
	// \{, \&, => no protection needed
	// \', \^, etc ==> no space to next char needed
	// \x Y ==> keep space
	// " -- " ==> Sub title mode
	// ": " ==> Sub title mode
	// [nonspace]+[A-Z]+[nonspace]* => protect
	//
	//		Titles("{Hello {{World}}   HOW {aRe} Things}")
	//		Titles("{ Hello {{World}} HOW   a{R}e Things}")
	//		Titles("{Hello {{World}} HOW a{R}e Things")
	//		Titles("Hello { { Wo   rld}} HOW a{R}e Things")
	// Braces can prevent kerning between letters, so it is in general preferable to enclose entire words and not just single letters in braces to protect them.
}
