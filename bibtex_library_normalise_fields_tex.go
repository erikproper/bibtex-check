/*
 *
 * Module: bibtex_library_normalise_fields_tex
 *
 * This module is concerned with the normalisation of TeX based titles and names
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 09.05.2024
 *
 */

package main

import "strings"

//import "fmt"

var TeXSpaces,
	WordLetters,
	TeXNoProtect,
	TeXSubTitlers,
	TeXDelimiters,
	TeXSingletons,
	UppercaseLetters TByteSet

type TBibTeXTeX struct {
	TCharacterStream                 // The underlying stream of characters.
	library          *TBibTeXLibrary // The BibTeX Library this parser will contribute to.
	inWord           bool
}

func (t *TBibTeXTeX) TeXSpacety() bool {
	for t.ThisCharacterWasIn(TeXSpaces) {
		t.inWord = false
	}

	return true
}

func (t *TBibTeXTeX) CollectTeXSpacety(s *string) bool {
	if t.ThisCharacterWasIn(TeXSpaces) {
		*s += " "
	}

	return t.TeXSpacety()
}

func (t *TBibTeXTeX) CollectTokenSequence(tokens *string, isOfferedProtection bool, needsProtection *bool) bool {
	token := ""
	spacety := ""
	sequenceNeedsProtection := false
	sequenceNextIsFirstTokenOfSubTitle := false

	*needsProtection = false

	for !t.EndOfStream() && !t.ThisCharacterIs('}') && t.CollectTeXSpacety(&spacety) &&
		/**/ t.CollectTeXToken(&token, isOfferedProtection, &sequenceNeedsProtection, &sequenceNextIsFirstTokenOfSubTitle) {
		if token != "" {
			if sequenceNeedsProtection {
				*tokens += spacety + "{" + token + "}"
				sequenceNeedsProtection = false
			} else {
				*tokens += spacety + token
			}
		}

		token = ""
		spacety = ""
	}

	return true
}

func (t *TBibTeXTeX) CollectTeXTokenElement(s *string, isOfferedProtection bool, needsProtection, nextIsFirstTokenOfSubTitle *bool) bool {
	switch {

	case t.ThisCharacterWas('{'):
		groupElements := ""
		groupNeedsProtection := false

		t.CollectTokenSequence(&groupElements, true, &groupNeedsProtection)

		if groupElements != "" {
			*s += "{" + groupElements + "}"
		}

		*needsProtection = false

		return t.ThisCharacterWas('}')

	case t.CollectCharacterThatIsIn(UppercaseLetters, s):
		if !*needsProtection && !isOfferedProtection && (t.inWord || *nextIsFirstTokenOfSubTitle) {
			*needsProtection = true
		}
		t.inWord = true

		return t.NextCharacter()

	case t.CollectCharacterThatWasIn(WordLetters, s):
		t.inWord = true

		return true

	case t.CollectCharacterThatWas('\\', s):
		*needsProtection = !isOfferedProtection && !t.ThisCharacterIsIn(TeXNoProtect)
		t.inWord = false

		return t.CollectCharacterThatWasThere(s)
	}

	return t.CollectCharacterThatWasNotIn(TeXDelimiters, s)
}

func (t *TBibTeXTeX) CollectTeXToken(token *string, isOfferedProtection bool, needsProtection, nextIsFirstTokenOfSubTitle *bool) bool {
	t.inWord = false

	if t.ThisCharacterIsIn(TeXSingletons) {
		t.inWord = false

		if t.CollectCharacterThatWasIn(TeXSubTitlers, token) {
			*nextIsFirstTokenOfSubTitle = t.ThisCharacterIsIn(TeXSpaces)
		} else {
			t.CollectCharacterThatWasIn(TeXSingletons, token)
		}
	} else {
		*needsProtection = false

		for !t.ThisCharacterIsIn(TeXSingletons) && t.CollectTeXTokenElement(token, isOfferedProtection, needsProtection, nextIsFirstTokenOfSubTitle) {
			*nextIsFirstTokenOfSubTitle = false
		}
	}

	return true
}

func NormaliseNamesString(l *TBibTeXLibrary, names string) string {
	name := ""
	token := ""
	andety := ""
	spacety := ""
	result := ""

	needsProtection := false
	nextIsFirstTokenOfSubTitle := false

	l.TBibTeXTeX.TextString(names)
	l.TBibTeXTeX.TeXSpacety()

	for !l.TBibTeXTeX.EndOfStream() && l.CollectTeXSpacety(&spacety) && l.CollectTeXToken(&token, false, &needsProtection, &nextIsFirstTokenOfSubTitle) {
		if token != "" {
			if strings.ToLower(token) == "and" {
				if name != "" {
					result += andety + NormalisePersonNameValue(l.TBibTeXTeX.library, name)
					name = ""
					token = ""
					andety = " and "

					l.TeXSpacety()
				}
			} else {
				name += spacety + token
				token = ""
			}
		}
		spacety = ""
	}

	if name != "" {
		result += andety + NormalisePersonNameValue(l.TBibTeXTeX.library, name)
	}

	//fmt.Println("------------------")
	//fmt.Println(names)
	//fmt.Println(result)

	return result
}

func NormaliseTitleString(l *TBibTeXLibrary, title string) string {
	needsProtection := false
	result := ""

	l.TBibTeXTeX.TextString(strings.ReplaceAll(strings.TrimRight(title, ".,"), " - ", " -- "))
	l.TBibTeXTeX.inWord = false
	l.TBibTeXTeX.TeXSpacety()
	l.TBibTeXTeX.CollectTokenSequence(&result, false, &needsProtection)

	return result
}

func init() {
	TeXSpaces.AddString(" ").TreatAsCharacters()
	TeXNoProtect.AddString("&").TreatAsCharacters()
	TeXSubTitlers.AddString("!?:;-").TreatAsCharacters()
	TeXSingletons.AddString("():;.,-").TreatAsCharacters()
	TeXDelimiters.AddString("{}").Unite(TeXSpaces).TreatAsCharacters()
	UppercaseLetters.AddString("ABCDEFGHIJKLMNOPQRSTUVWXYZ").TreatAsCharacters()
	WordLetters.AddString("0123456789abcdefghijklmnopqrstuvwxyz").Unite(UppercaseLetters).TreatAsCharacters()
}
