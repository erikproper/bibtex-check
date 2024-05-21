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
	if t.ThisCharacterWasIn(TeXSpaces) {
		for t.ThisCharacterWasIn(TeXSpaces) {
			t.inWord = false
		}

		return true
	}

	return !t.EndOfStream()
}

func (t *TBibTeXTeX) CollectTeXSpacety(s *string) bool {
	if t.ThisCharacterWasIn(TeXSpaces) {
		*s += " "

		t.TeXSpacety()

		return true
	}

	return !t.EndOfStream()
}

func (t *TBibTeXTeX) CollectTokenSequencety(tokens *string, isOfferedProtection bool, needsProtection *bool) {
	token := ""
	spacety := ""
	sequenceNeedsProtection := false
	sequenceNextIsFirstTokenOfSubTitle := false

	*needsProtection = false

	for !t.ThisCharacterIs('}') &&
		/**/ t.CollectTeXSpacety(&spacety) &&
		/*  */ t.CollectTeXToken(&token, isOfferedProtection, &sequenceNeedsProtection, &sequenceNextIsFirstTokenOfSubTitle) {
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
}

func (t *TBibTeXTeX) CollectTeXTokenElement(s *string, isOfferedProtection bool, needsProtection, nextIsFirstTokenOfSubTitle *bool) bool {
	if t.EndOfStream() {
		return false
	} else {
		switch {

		case t.ThisCharacterWas('{'):
			groupElements := ""
			groupNeedsProtection := false

			t.CollectTokenSequencety(&groupElements, true, &groupNeedsProtection)

			if groupElements != "" {
				*s += "{" + groupElements + "}"
			}

			*needsProtection = false

			t.ThisCharacterWas('}')

			return true

		case t.CollectCharacterThatIsIn(UppercaseLetters, s):
			if !*needsProtection && !isOfferedProtection && (t.inWord || *nextIsFirstTokenOfSubTitle) {
				*needsProtection = true
			}
			t.inWord = true

			t.NextCharacter()

			return true

		case t.CollectCharacterThatWasIn(WordLetters, s):
			t.inWord = true

			return true

		case t.CollectCharacterThatWas('\\', s):
			*needsProtection = !isOfferedProtection && !t.ThisCharacterIsIn(TeXNoProtect)
			t.inWord = false

			t.CollectCharacterThatWasThere(s)

			return true

		default:
			return t.CollectCharacterThatWasNotIn(TeXDelimiters, s)

		}
	}
}

func (t *TBibTeXTeX) CollectTeXToken(token *string, isOfferedProtection bool, needsProtection, nextIsFirstTokenOfSubTitle *bool) bool {
	t.inWord = false

	if t.EndOfStream() {
		return false
	} else {
		if t.ThisCharacterIsIn(TeXSingletons) {
			t.inWord = false

			if t.CollectCharacterThatWasIn(TeXSubTitlers, token) {
				*nextIsFirstTokenOfSubTitle = t.ThisCharacterIsIn(TeXSpaces)
			} else {
				t.CollectCharacterThatWasIn(TeXSingletons, token)
			}

		} else {
			*needsProtection = false

			for !t.ThisCharacterIsIn(TeXSingletons) && !t.EndOfStream() && t.CollectTeXTokenElement(token, isOfferedProtection, needsProtection, nextIsFirstTokenOfSubTitle) {
				*nextIsFirstTokenOfSubTitle = false
			}
		}

		return true
	}
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

	for l.CollectTeXSpacety(&spacety) && l.CollectTeXToken(&token, false, &needsProtection, &nextIsFirstTokenOfSubTitle) {
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

	return result
}

func NormaliseTitleString(l *TBibTeXLibrary, title string) string {
	needsProtection := false
	result := ""

	l.TBibTeXTeX.TextString(strings.ReplaceAll(title, " - ", " -- "))
	l.TBibTeXTeX.inWord = false
	l.TBibTeXTeX.TeXSpacety()
	l.TBibTeXTeX.CollectTokenSequencety(&result, false, &needsProtection)

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
