/*
 *
 * Module: bibtex_library_indexing
 *
 * This module is adds the functionality (for TBibTeXLibrary) related to the indexing of entries based on the fields
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 21.05.2024
 *
 */

package main

import (
	//	"fmt"
	"strings"
	// "os"
)

// Definition of the map for field processors
type TFieldIndexers = map[string]func(string) string

var fieldIndexers TFieldIndexers

func ISBNIndexer(input string) string {
	return strings.ReplaceAll(input, "-", "")
}

func TeXStringIndexer(input string) string {
	s := input

	// Remove \unicode{N} escapes at the very start, before any structural stripping,
	// so the entire token is removed as a unit rather than leaving partial artefacts.
	s = unicodeEscapeRE.ReplaceAllString(s, "")

	// Pre-phase: specific multi-character command-to-text mappings applied before
	// any structural stripping, so the resulting characters survive Phase 1.
	s = strings.ReplaceAll(s, `\c `, "")
	s = strings.ReplaceAll(s, `\k `, "")
	s = strings.ReplaceAll(s, `\v `, "")
	s = strings.ReplaceAll(s, `\r `, "")
	s = strings.ReplaceAll(s, `\H `, "")
	s = strings.ReplaceAll(s, `\AA`, "aa")
	s = strings.ReplaceAll(s, `\AE`, "ae")
	s = strings.ReplaceAll(s, `\OE`, "oe")
	s = strings.ReplaceAll(s, `\aa`, "aa")
	s = strings.ReplaceAll(s, `\ae`, "ae")
	s = strings.ReplaceAll(s, `\oe`, "oe")
	s = strings.ReplaceAll(s, `\i`, "i")
	s = strings.ReplaceAll(s, `\ss`, "s")
	s = strings.ReplaceAll(s, `\ell`, "l")
	s = strings.ReplaceAll(s, `\&`, "&")
	s = strings.ReplaceAll(s, `\vert`, "|")

	// Phase 1: strip grouping chars, math-mode delimiters, $ signs, and
	// punctuation. Backslashes are deliberately left for Phase 2.
	for _, ch := range []string{
		"{", "}", "(", ")", "$",
		"~", ".", ",", `"`, "'", "`", "^", "*", "=", "!",
		"?", "_", "-", ":", ";", "/", "|", " ",
	} {
		s = strings.ReplaceAll(s, ch, "")
	}

	// Phase 1.5: apply latex_indexer.csv macro approximations (entries sorted
	// longest-first to prevent shorter macros shadowing longer prefixes).
	for _, e := range latexIndexerEntries {
		s = strings.ReplaceAll(s, `\`+e.macro, e.replacement)
	}

	// Phase 2: iteratively resolve \backslash / \textbackslash back to a real \,
	// then strip transparent wrapper macros (command name removed, content kept).
	// Longer commands are listed before shorter ones to prevent partial matches
	// (e.g. \textit must be stripped before \text).
	// Repeat until the string stabilises.
	for {
		prev := s
		s = strings.ReplaceAll(s, `\backslash`, `\`)
		s = strings.ReplaceAll(s, `\textbackslash`, `\`)
		for _, cmd := range []string{
			`\mathscr`, `\mathcal`,
			`\textit`, `\textbf`,
			`\mathbb`, `\mathbf`, `\mathit`, `\mathrm`,
			`\mbox`, `\hbox`, `\emph`, `\text`,
		} {
			s = strings.ReplaceAll(s, cmd, "")
		}
		if s == prev {
			break
		}
	}

	// Phase 3: strip remaining backslashes and lowercase everything.
	s = strings.ReplaceAll(s, `\`, "")
	s = strings.ToLower(s)

	return s
}

func (l *TBibTeXLibrary) IndexEntryFieldValue(key, field, value string) string {
	indexedValue := l.MapNormalisedEntryFieldValue(key, field, value)

	valueIndexer, hasIndexer := fieldIndexers[field]
	if hasIndexer {
		valueIndexer(indexedValue)
	}

	return indexedValue
}

func init() {
	// Define the processing functions.
	fieldIndexers = TFieldIndexers{}

	fieldIndexers["booktitle"] = TeXStringIndexer
	fieldIndexers[TitleField] = TeXStringIndexer
	fieldIndexers["isbn"] = ISBNIndexer
}
