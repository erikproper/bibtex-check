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

var (
	TeXSpaces,
	WordLetters,
	TeXNoProtect,
	TeXSubTitlers,
	TeXDelimiters,
	TeXSingletons,
	UppercaseLetters TByteSet
	LaTeXMap TStringMap
)

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

func (t *TBibTeXTeX) CollectTokenSequencety(tokens *string, isOfferedProtection bool) {
	token := ""
	spacety := ""
	tokenNeedsProtection := false
	sequenceNextIsFirstTokenOfSubTitle := false

	for !t.ThisCharacterIs('}') &&
		/**/ t.CollectTeXSpacety(&spacety) &&
		/*  */ t.CollectTeXToken(&token, isOfferedProtection, &tokenNeedsProtection, &sequenceNextIsFirstTokenOfSubTitle) {
		if token != "" {
			if tokenNeedsProtection {
				*tokens += spacety + "{" + token + "}"

				tokenNeedsProtection = false
			} else {
				*tokens += spacety + token
			}
		}

		token = ""
		spacety = ""
	}
}

// //// SIMPLIFY???
func (t *TBibTeXTeX) CollectTeXTokenElement(s *string, isOfferedProtection bool, needsProtection, nextIsFirstTokenOfSubTitle *bool) bool {
	if t.EndOfStream() {
		return false
	} else {
		switch {

		case t.ThisCharacterWas('{'):
			groupElements := ""

			t.CollectTokenSequencety(&groupElements, true)

			*s += "{" + groupElements + "}"

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
	*needsProtection = false

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
			for !t.ThisCharacterIsIn(TeXSingletons) && !t.EndOfStream() && t.CollectTeXTokenElement(token, isOfferedProtection, needsProtection, nextIsFirstTokenOfSubTitle) {
				*nextIsFirstTokenOfSubTitle = false
			}
		}

		return true
	}
}

func ApplyLaTeXMap (s string) string {
	result := strings.ReplaceAll(strings.ReplaceAll(s, " - ", " -- "), "{-}", "-")

	for source, target := range LaTeXMap {
		result = strings.ReplaceAll(result, "{" + source + "}", target)
		result = strings.ReplaceAll(result, source, target)
	}
	
	return result
}

func NormaliseNamesString(l *TBibTeXLibrary, names string) string {
	name := ""
	token := ""
	andety := ""
	spacety := ""
	result := ""

	needsProtection := false
	nextIsFirstTokenOfSubTitle := false

	l.TBibTeXTeX.TextString(ApplyLaTeXMap(names))
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
	l.TBibTeXTeX.TextString(ApplyLaTeXMap(title))
	l.TBibTeXTeX.inWord = false
	l.TBibTeXTeX.TeXSpacety()

	result := ""
	l.TBibTeXTeX.CollectTokenSequencety(&result, false)

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

	// Insert the second set first, then add the { } option and then tne " - "
	LaTeXMap = TStringMap{
		"\\`{A}":    "{\\`A}",
		"\\'{A}":    "{\\'A}",
		"\\^{A}":    "{\\^A}",
		"\\~{A}":    "{\\~A}",
		"\\\"{A}":   "{\\\"A}",
		"\\c{C}":    "{\\c C}",
		"\\`{E}":    "{\\`E}",
		"\\'{E}":    "{\\'E}",
		"\\^{E}":    "{\\^E}",
		"\\\"{E}":   "{\\\"E}",
		"\\`{I}":    "{\\`I}",
		"\\'{I}":    "{\\'I}",
		"\\^{I}":    "{\\^I}",
		"\\\"{I}":   "{\\\"I}",
		"\\~{n}":    "{\\~n}",
		"\\~{N}":    "{\\~N}",
		"\\`{O}":    "{\\`O}",
		"\\'{O}":    "{\\'O}",
		"\\^{O}":    "{\\^O}",
		"\\~{O}":    "{\\~O}",
		"\\\"{O}":   "{\\\"O}",
		"\\`{U}":    "{\\`U}",
		"\\'{U}":    "{\\'U}",
		"\\^{U}":    "{\\^U}",
		"\\\"{U}":   "{\\\"U}",
		"\\`{a}":    "{\\`a}",
		"\\'{a}":    "{\\'a}",
		"\\^{a}":    "{\\^a}",
		"\\~{a}":    "{\\~a}",
		"\\\"{a}":   "{\\\"a}",
		"\\c{c}":    "{\\c c}",
		"\\`{e}":    "{\\`e}",
		"\\'{e}":    "{\\'e}",
		"\\^{e}":    "{\\^e}",
		"\\\"{e}":   "{\\\"e}",
		"\\`{\\i}":  "{\\`\\i}",
		"\\'{\\i}":  "{\\'\\i}",
		"\\^{\\i}":  "{\\^\\i}",
		"\\\"{\\i}": "{\\\"\\i}",
		"\\`{o}":    "{\\`o}",
		"\\'{o}":    "{\\'o}",
		"\\^{o}":    "{\\^o}",
		"\\~{o}":    "{\\~o}",
		"\\\"{o}":   "{\\\"o}",
		"\\`{u}":    "{\\`u}",
		"\\'{u}":    "{\\'u}",
		"\\^{u}":    "{\\^u}",
		"\\\"{u}":   "{\\\"u}",
		"\\'{y}":    "{\\'y}",
		"\\\"{y}":   "{\\\"y}",
		"\\={A}":    "{\\=A}",
		"\\={a}":    "{\\=a}",
		"\\'{C}":    "{\\'C}",
		"\\'{c}":    "{\\'c}",
		"\\.{C}":    "{\\.C}",
		"\\.{c}":    "{\\.c}",
		"\\v{C}":    "{\\v C}",
		"\\v{c}":    "{\\v c}",
		"\\={E}":    "{\\=E}",
		"\\={e}":    "{\\=e}",
		"\\.{E}":    "{\\.E}",
		"\\.{e}":    "{\\.e}",
		"\\k{E}":    "{\\k E}",
		"\\k{e}":    "{\\k e}",
		"\\v{E}":    "{\\v E}",
		"\\v{e}":    "{\\v e}",
		"\\v{G}":    "{\\v G}",
		"\\v{g}":    "{\\v g}",
		"\\.{G}":    "{\\.G}",
		"\\.{g}":    "{\\.g}",
		"\\~{I}":    "{\\~I}",
		"\\~{\\i}":  "{\\~\\i}",
		"\\={I}":    "{\\=I}",
		"\\={\\i}":  "{\\=\\i}",
		"\\k{I}":    "{\\k I}",
		"\\k{i}":    "{\\k i}",
		"\\'{N}":    "{\\'N}",
		"\\'{n}":    "{\\'n}",
		"\\v{N}":    "{\\v N}",
		"\\v{n}":    "{\\v n}",
		"\\={O}":    "{\\=O}",
		"\\={o}":    "{\\=o}",
		"\\v{R}":    "{\\v R}",
		"\\v{r}":    "{\\v r}",
		"\\'{S}":    "{\\'S}",
		"\\'{s}":    "{\\'s}",
		"\\c{S}":    "{\\c S}",
		"\\c{s}":    "{\\c s}",
		"\\v{S}":    "{\\v S}",
		"\\v{s}":    "{\\v s}",
		"\\~{U}":    "{\\~U}",
		"\\~{u}":    "{\\~u}",
		"\\={U}":    "{\\=U}",
		"\\={u}":    "{\\=u}",
		"\\r{U}":    "{\\r U}",
		"\\r{u}":    "{\\r u}",
		"\\H{U}":    "{\\H U}",
		"\\H{u}":    "{\\H u}",
		"\\v{W}":    "{\\v W}",
		"\\v{v}":    "{\\v v}",
		"\\^{Y}":    "{\\^Y}",
		"\\^{y}":    "{\\^y}",
		"\\\"{Y}":   "{\\\"Y}",
		"\\'{Y}":    "{\\'Y}",
		"\\'{z}":    "{\\'z}",
		"\\'{Z}":    "{\\'Z}",
		"\\.{Z}":    "{\\.Z}",
		"\\.{z}":    "{\\.z}",
		"\\v{Z}":    "{\\v Z}",
		"\\v{z}":    "{\\v z}",
		"\\v{A}":    "{\\v A}",
		"\\v{a}":    "{\\v a}",
		"\\v{I}":    "{\\v I}",
		"\\v{\\i}":  "{\\v\\i}",
		"\\v{O}":    "{\\v O}",
		"\\v{o}":    "{\\v o}",
		"\\v{U}":    "{\\v U}",
		"\\v{u}":    "{\\v u}",
		"\\H{o}":    "{\\H o}",
		"\\~{E}":    "{\\~E}",
		"\\~{e}":    "{\\~e}",
	}
}
