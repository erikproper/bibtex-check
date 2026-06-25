/*
 *
 * Module: bibtex_library_repair
 *
 * Garbled-name detection predicates used as guards throughout the codebase.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 */

package main

import (
	"regexp"
	"strings"
)

// incompleteTeXAccentAtEnd matches a brace group containing only a single
// lowercase TeX command letter at the end of a string (e.g. "{\c}", "{\u}").
// This pattern indicates a name that was truncated at the space inside a
// TeX accent group like {\c c} — the {\c} fragment is left with no argument.
// Excludes i, j, l, o: these are complete standalone LaTeX character commands
// (\i = dotless i, \j = dotless j, \l = ł, \o = ø) and need no argument.
var incompleteTeXAccentAtEnd = regexp.MustCompile(`\{\\[a-hkm-np-z]\}$`)

// knownSuffixes is the set of BibTeX name suffixes recognised in the
// "Last, Jr, First" three-part format.  A 2-comma name whose middle part
// is in this set is a valid suffix form, NOT garbled.
var knownSuffixes = map[string]bool{
	"Jr": true, "Jr.": true,
	"Sr": true, "Sr.": true,
	"II": true, "III": true, "IV": true, "V": true,
}

// hasSingleLetterSurname returns true when any name in a BibTeX "and"-list
// has a single-letter surname (e.g. "A, Prajith C." from auto-harvesting
// "Prajith C. A").  Kept separate from hasGarbledName because single-letter
// name parts are legitimate in some conventions and should not be flagged
// during normal operation — only rolled back during an explicit repair run
// when a reference bib is available.
func hasSingleLetterSurname(names string) bool {
	for _, name := range strings.Split(names, " and ") {
		name = strings.TrimSpace(name)
		if idx := strings.Index(name, ","); idx >= 0 {
			if len(strings.TrimRight(strings.TrimSpace(name[:idx]), ".")) <= 1 {
				return true
			}
		}
	}
	return false
}

// hasGarbledName returns true when any individual name in a BibTeX "and"-list
// is garbled.  Three patterns are detected:
//   - ≥3 commas (e.g. "A., Bubenko, Jr, Janis")
//   - exactly 2 commas where the middle part is not a known suffix
//     (ruling out the valid "Bubenko, Jr, Janis A." form)
//   - "ss, ff" form where ff ends with an incomplete TeX accent group like {\c}
//     indicating truncation at the space inside {\c c} (Preguiça-type garbling)
//
// Single-letter surname cases (e.g. "A, Prajith C.") are intentionally NOT
// detected here: single-letter name parts occur in legitimate naming conventions
// and those entries require manual review rather than auto-correction.
func hasGarbledName(names string) bool {
	for _, name := range strings.Split(names, " and ") {
		name = strings.TrimSpace(name)
		if hasUnbalancedBraces(name) {
			return true
		}
		commas := strings.Count(name, ",")
		if commas >= 3 {
			return true
		}
		if commas == 2 {
			parts := strings.SplitN(name, ",", 3)
			if !knownSuffixes[strings.TrimSpace(parts[1])] {
				return true
			}
		}
		if commas == 1 {
			idx := strings.Index(name, ", ")
			if idx >= 0 && incompleteTeXAccentAtEnd.MatchString(name[idx+2:]) {
				return true
			}
		}
	}
	return false
}

// hasStrayBrace returns true when s has brace characters that are not valid
// TeX encoding.  Two patterns are detected:
//   - s starts with "{" and the first group contains a comma or is empty
//     (whole-name wrapping, e.g. "{Bubenko, Jr. J. A.}")
//   - a "}" brings the running brace depth below zero (a stray closing brace,
//     e.g. "Jr. J. A.} {Bubenko" from a malformed name inversion)
//
// TeX accent groups such as {\"O}, {\c c}, {\AA} have no comma inside and do
// not make depth go negative, so names like {\"O}v{\"u}n{\c c} {\c C}etin are
// correctly accepted.
func hasStrayBrace(s string) bool {
	if strings.HasPrefix(s, "{") {
		closingBrace := strings.Index(s, "}")
		if closingBrace <= 1 || strings.Contains(s[1:closingBrace], ",") {
			return true
		}
	}
	depth := 0
	for _, ch := range s {
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				return true
			}
		}
	}
	return false
}

// hasUnbalancedBraces returns true when the number of opening braces does not
// equal the number of closing braces in s.  Used to detect name fragments that
// resulted from a naive " and " split inside a brace group.
func hasUnbalancedBraces(s string) bool {
	depth := 0
	for _, ch := range s {
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	return depth != 0
}

// isGarbledContributorName returns true when name is not a valid single-person
// BibTeX name.  Catches:
//   - any occurrence of " and " (person names never contain " and ", even inside
//     brace groups — the brace may have absorbed an entire author list)
//   - names with ≥3 commas or an ambiguous 2-comma form
//   - names starting with "{" or containing "} {" (stray braces)
//   - "ss, ff" names where ff ends with an incomplete TeX accent group like {\c}
//     indicating truncation at the space inside a group like {\c c}
func isGarbledContributorName(name string) bool {
	if strings.Contains(name, " and ") {
		return true
	}
	if hasGarbledName(name) {
		return true
	}
	if hasStrayBrace(name) {
		return true
	}
	if idx := strings.Index(name, ", "); idx >= 0 {
		ff := name[idx+2:]
		if incompleteTeXAccentAtEnd.MatchString(ff) {
			return true
		}
	}
	return false
}
