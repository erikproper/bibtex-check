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

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	unicodeEscapeRE       = regexp.MustCompile(`\\unicode\{(x[0-9A-Fa-f]+|\d+)\}`)
	bracedUnicodeEscapeRE = regexp.MustCompile(`\{\\unicode\{(x[0-9A-Fa-f]+|\d+)\}\}`)
)

// normaliseUnicodeMap maps known Unicode code points to their BibTeX-normalised strings.
// ASCII-equivalent mappings are preferred so that later passes (e.g. {-}→-) fire correctly.
// Unknown non-ASCII code points are NOT listed here; normaliseUnicodeRune returns the
// original \unicode{N} escape for them so that NormaliseFieldValue can emit a warning.
var normaliseUnicodeMap = map[rune]string{
	0x2006: " ", 0x2009: " ", 0x2028: " ", 0x00A0: " ", 0x3000: " ",
	0x00AD: "", 0x200B: "", 0x200C: "", 0x200D: "", 0x2060: "",
	0x2010: "-", 0x2011: "-", 0x2212: "-",
	0x2012: "--", 0x2013: "--", 0x2500: "--",
	0x2014: "---",
	0x2018: "'", 0x2019: "'",
	0x201C: "``",
	0x201D: "''",
	0x3001: ",", 0xFF0C: ",",
	0xFF0E: ".",
	0xFF1A: ":",
	0xFF1B: ";",
	0xFF01: "!",
	0xFF1F: "? ",
	0xFF08: "(",
	0xFF09: ")",
	0xFF3B: "{", 0x3015: "{",
	0xFF3D: "}", 0x3016: "}", 0xFF5D: "}",
	0xFF5B: "{",
	0x2751: "*",
	0x2026: "...",
	0xFF41: "a",
}

// normaliseUnicodeRune looks up r in normaliseUnicodeMap.
// For unknown non-ASCII code points it returns the original \unicode{N} escape so that
// NormaliseFieldValue can detect and warn about unresolved escapes.
func normaliseUnicodeRune(r rune) string {
	if mapped, ok := normaliseUnicodeMap[r]; ok {
		return mapped
	}
	if r < 128 {
		return string(r)
	}
	return `\unicode{` + strconv.Itoa(int(r)) + `}`
}

// parseUnicodeArg parses a \unicode argument that is either decimal ("8220") or
// hex with an x-prefix ("x201C") and returns the code point value.
func parseUnicodeArg(arg string) (rune, bool) {
	if strings.HasPrefix(arg, "x") || strings.HasPrefix(arg, "X") {
		n, err := strconv.ParseInt(arg[1:], 16, 32)
		if err != nil || n < 0 {
			return 0, false
		}
		return rune(n), true
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n < 0 {
		return 0, false
	}
	return rune(n), true
}

// replaceUnicodeEscapes converts \unicode{N} (and {\unicode{N}}) to the normalised
// string for code point N. N may be decimal or hex (x-prefixed). The braced form is
// handled first so that surrounding braces are dropped and the mapped value is inserted
// directly into the surrounding text.
func replaceUnicodeEscapes(s string) string {
	applyMap := func(re *regexp.Regexp, match string) string {
		sub := re.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		r, ok := parseUnicodeArg(sub[1])
		if !ok {
			return match
		}
		return normaliseUnicodeRune(r)
	}
	s = bracedUnicodeEscapeRE.ReplaceAllStringFunc(s, func(m string) string { return applyMap(bracedUnicodeEscapeRE, m) })
	return unicodeEscapeRE.ReplaceAllStringFunc(s, func(m string) string { return applyMap(unicodeEscapeRE, m) })
}

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
	sequenceNextIsTokenOfSubTitle := false

	for !t.ThisCharacterIs('}') &&
		/**/ t.CollectTeXSpacety(&spacety) &&
		/*  */ t.CollectTeXToken(&token, isOfferedProtection, &tokenNeedsProtection, &sequenceNextIsTokenOfSubTitle) {
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
func (t *TBibTeXTeX) CollectTeXTokenElement(s *string, isOfferedProtection bool, needsProtection, nextIsTokenOfSubTitle *bool) bool {
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
			if !*needsProtection && !isOfferedProtection && (t.inWord || *nextIsTokenOfSubTitle) {
				*needsProtection = true
			}
			t.inWord = true

			t.NextCharacter()

			return true

		case t.CollectCharacterThatWasIn(WordLetters, s):
			t.inWord = true

			return true

		case t.CollectCharacterThatWas('\\', s):
			// Only set needsProtection; never clear it — uppercase letters before \& must stay protected.
			if !isOfferedProtection && !t.ThisCharacterIsIn(TeXNoProtect) {
				*needsProtection = true
			}
			t.inWord = false

			t.CollectCharacterThatWasThere(s)

			return true

		default:
			return t.CollectCharacterThatWasNotIn(TeXDelimiters, s)

		}
	}
}

func (t *TBibTeXTeX) CollectTeXToken(token *string, isOfferedProtection bool, needsProtection, nextIsTokenOfSubTitle *bool) bool {
	t.inWord = false
	*needsProtection = false

	if t.EndOfStream() {
		return false
	} else {
		if t.ThisCharacterIsIn(TeXSingletons) {
			t.inWord = false

			if t.CollectCharacterThatWasIn(TeXSubTitlers, token) {
				*nextIsTokenOfSubTitle = t.ThisCharacterIsIn(TeXSpaces)
			} else {
				t.CollectCharacterThatWasIn(TeXSingletons, token)
			}

		} else {
			for !t.ThisCharacterIsIn(TeXSingletons) && !t.EndOfStream() && t.CollectTeXTokenElement(token, isOfferedProtection, needsProtection, nextIsTokenOfSubTitle) {
				*nextIsTokenOfSubTitle = false
			}
		}

		return true
	}
}

func ApplyLaTeXMap(s string) string {
	s = replaceUnicodeEscapes(s)
	result := strings.ReplaceAll(s, "{\\-}", "")  // LaTeX hyphenation hint (braced)
	result = strings.ReplaceAll(result, "\\-", "") // LaTeX hyphenation hint (bare)
	result = strings.ReplaceAll(result, "{---}", "---")
	result = strings.ReplaceAll(result, "{--}", "--")
	result = strings.ReplaceAll(result, "{-}", "-")
	result = strings.ReplaceAll(result, "{?}", "?")
	result = strings.ReplaceAll(result, "{\\&}", "\\&")
	// Need to do this twice ... to deal with " - - - "
	result = strings.ReplaceAll(result, " - ", " -- ")
	result = strings.ReplaceAll(result, " - ", " -- ")
	result = strings.ReplaceAll(result, " : ", ": ")

	for source, target := range LaTeXMap {
		result = strings.ReplaceAll(result, "{"+source+"}", target)
		result = strings.ReplaceAll(result, source, target)
	}

	result = strings.ReplaceAll(result, "{}_", "\x00sub") // protect subscript base
	result = strings.ReplaceAll(result, "{}^", "\x00sup") // protect superscript base
	result = strings.ReplaceAll(result, "{}", "")          // strip standalone empty braces
	result = strings.ReplaceAll(result, "\x00sub", "{}_") // restore subscript base
	result = strings.ReplaceAll(result, "\x00sup", "{}^") // restore superscript base

	return result
}

func (l *TBibTeXLibrary) maybeAddSimpleName(parts int, firstName, lastName string) {
	if !l.harvestNameAliases {
		return
	}
	if parts == 2 && !strings.Contains(firstName, ",") && !strings.Contains(lastName, ",") {
		name := firstName + " " + lastName
		if _, isMapped := l.NameAliasToName[name]; !isMapped {
			canonical := lastName + ", " + firstName
			l.AddAlias(name, canonical, &l.NameAliasToName, &l.NameToAliases, false)
			l.FindAliases(canonical, name)
			l.FindAliases(canonical, canonical)
			l.nameMappingsModified = true
		}
	}
}

func NormaliseNamesString(l *TBibTeXLibrary, names string) string {
	name := ""
	token := ""
	namePartsCount := 0
	previousFirstName := ""
	previousLastName := ""
	andety := ""
	spacety := ""
	result := ""

	needsProtection := false
	nextIsTokenOfSubTitle := false

	l.TBibTeXTeX.TextString(ApplyLaTeXMap(names))
	l.TBibTeXTeX.TeXSpacety()

	for l.CollectTeXSpacety(&spacety) && l.CollectTeXToken(&token, false, &needsProtection, &nextIsTokenOfSubTitle) {
		if token != "" {
			if strings.ToLower(token) == "and" {
				if name != "" {
					l.maybeAddSimpleName(namePartsCount, previousFirstName, previousLastName)

					normalised := NormalisePersonNameValue(l.TBibTeXTeX.library, name)
					if l.harvestNameAliases && strings.Contains(normalised, ", ") {
						if l.FindAliases(normalised, normalised) {
							l.nameMappingsModified = true
						}
					}
					result += andety + normalised
					name = ""
					token = ""
					previousFirstName = ""
					previousLastName = ""
					namePartsCount = 0
					andety = " and "

					l.TeXSpacety()
				}
			} else {
				name += spacety + token
				namePartsCount++
				previousFirstName = previousLastName
				previousLastName = token
				token = ""
			}
		}
		spacety = ""
	}

	if name != "" {
		l.maybeAddSimpleName(namePartsCount, previousFirstName, previousLastName)

		normalised := NormalisePersonNameValue(l.TBibTeXTeX.library, name)
		if l.harvestNameAliases && strings.Contains(normalised, ", ") {
			if l.FindAliases(normalised, normalised) {
				l.nameMappingsModified = true
			}
		}
		result += andety + normalised
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
