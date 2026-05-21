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
// normaliseUnicodeMap is populated by loadUnicodeMap from unicode_map.csv at startup.
// Code points not listed here are kept as \unicode{N} by normaliseUnicodeRune so that the
// bib file remains valid LaTeX (callers supply \usepackage{unicode} or \def\unicode#1{}).
var normaliseUnicodeMap map[rune]string

// normaliseUnicodeRune looks up r in normaliseUnicodeMap and returns the mapped
// string when found. ASCII code points not in the map are returned as-is.
// Unmapped non-ASCII code points are kept as \unicode{N} so the bib file remains
// valid LaTeX (callers supply \usepackage{unicode} or \def\unicode#1{}).
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

		// A dash-only token between spaces (em-dash pattern) acts as a subtitle
		// separator: the next capital letter needs protection just like after ':' or '?'.
		if spacety != "" && strings.Trim(token, "-") == "" && token != "" && t.ThisCharacterIsIn(TeXSpaces) {
			sequenceNextIsTokenOfSubTitle = true
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
			// A '-' in a token that started with an uppercase letter resets inWord so
			// that the next capital (e.g. 'D' in Value-Driven) is not treated as a
			// mid-word uppercase and does not trigger protection.
			if t.ThisCharacterIs('-') && len(*s) > 0 && UppercaseLetters.Contains((*s)[0]) {
				t.inWord = false
			}
			return t.CollectCharacterThatWasNotIn(TeXDelimiters, s)

		}
	}
}

// isIdentifierLikeToken returns true when s starts with an uppercase letter and
// no segment (split by '-') contains any lowercase letter. Used to gate
// uppercase-in-tail protection: S-P qualifies (both segments all-uppercase),
// Value-Driven does not (segments contain lowercase).
func isIdentifierLikeToken(s string) bool {
	if len(s) == 0 || !UppercaseLetters.Contains(s[0]) {
		return false
	}
	for _, segment := range strings.Split(s, "-") {
		for _, ch := range segment {
			if ch >= 'a' && ch <= 'z' {
				return false
			}
		}
	}
	return true
}

// isPunctuatedAcronymPrefix returns true when s consists entirely of single
// uppercase letters separated by '.', e.g. "U", "U.S", "U.S.A". Used to
// detect capitalised abbreviations like U.S. so their final '.' can be
// absorbed into the token rather than acting as a subtitle trigger.
func isPunctuatedAcronymPrefix(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, part := range strings.Split(s, ".") {
		if len(part) != 1 || !UppercaseLetters.Contains(part[0]) {
			return false
		}
	}
	return true
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

			// Punctuated-acronym detection: when the token so far is a sequence of
			// single uppercase letters separated by '.' (e.g. "U" or "U.S"), consume
			// the '.' into the token and continue. The final '.' is absorbed so it
			// never fires as a subtitle trigger. X.Y: still works because ':' is left
			// as the current character and claims its subtitle right normally.
			if !isOfferedProtection {
				for t.ThisCharacterIs('.') && isPunctuatedAcronymPrefix(*token) {
					*token += "."
					t.NextCharacter()
					*needsProtection = true
					if !t.ThisCharacterIsIn(UppercaseLetters) {
						break
					}
					for !t.ThisCharacterIsIn(TeXSingletons) && !t.EndOfStream() && t.CollectTeXTokenElement(token, isOfferedProtection, needsProtection, nextIsTokenOfSubTitle) {
						*nextIsTokenOfSubTitle = false
					}
				}
			}

			// Also protect tokens whose first character is uppercase and whose tail
			// contains a digit or '+' (e.g. L1, B2a, C++), or where the token is
			// identifier-like (no lowercase in any segment) and the tail contains an
			// uppercase letter (e.g. S-P). Value-Driven is excluded because its
			// segments contain lowercase.
			if !*needsProtection && !isOfferedProtection && len(*token) > 1 && UppercaseLetters.Contains((*token)[0]) {
				isIdent := isIdentifierLikeToken(*token)
				for _, ch := range (*token)[1:] {
					if (ch >= '0' && ch <= '9') || ch == '+' || (isIdent && UppercaseLetters.Contains(byte(ch))) {
						*needsProtection = true
						break
					}
				}
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

	result = strings.ReplaceAll(result, "{ }", " ")        // brace-protected space → regular space
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
			// Follow any existing alias chain for the derived canonical so we
			// point to the true canonical rather than an intermediate. Without
			// this, "First Last" → "Last, First" creates a cycle when the name
			// mappings already contain "Last, First" → "First Last".
			visited := map[string]bool{canonical: true}
			for {
				redirected, isAlias := l.NameAliasToName[canonical]
				if !isAlias {
					break
				}
				if redirected == name || visited[redirected] {
					return // would create a cycle; skip entirely
				}
				visited[redirected] = true
				canonical = redirected
			}
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
	TeXSubTitlers.AddString("!?:;.").TreatAsCharacters()
	TeXSingletons.AddString("():;.,!?").TreatAsCharacters()
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
