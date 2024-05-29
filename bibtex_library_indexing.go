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

func ISBNIndexer(input string) string {
	return strings.ReplaceAll(input, "-", "")
}

func TeXStringIndexer(input string) string {
	cleaned := input

	cleaned = strings.ReplaceAll(cleaned, "\\c ", "")
	cleaned = strings.ReplaceAll(cleaned, "\\k ", "")
	cleaned = strings.ReplaceAll(cleaned, "\\v ", "")
	cleaned = strings.ReplaceAll(cleaned, "\\r ", "")
	cleaned = strings.ReplaceAll(cleaned, "\\H ", "")
	cleaned = strings.ReplaceAll(cleaned, "\\AA", "aa")
	cleaned = strings.ReplaceAll(cleaned, "\\AE", "ae")
	cleaned = strings.ReplaceAll(cleaned, "\\OE", "oe")
	cleaned = strings.ReplaceAll(cleaned, "\\aa", "aa")
	cleaned = strings.ReplaceAll(cleaned, "\\ae", "ae")
	cleaned = strings.ReplaceAll(cleaned, "\\oe", "oe")
	cleaned = strings.ReplaceAll(cleaned, "\\i", "i")
	cleaned = strings.ReplaceAll(cleaned, "\\ss", "s")
	cleaned = strings.ReplaceAll(cleaned, "\\&", "&")
	cleaned = strings.ReplaceAll(cleaned, "{", "")
	cleaned = strings.ReplaceAll(cleaned, "}", "")
	cleaned = strings.ReplaceAll(cleaned, "~", "")
	cleaned = strings.ReplaceAll(cleaned, ".", "")
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	cleaned = strings.ReplaceAll(cleaned, "\"", "")
	cleaned = strings.ReplaceAll(cleaned, "'", "")
	cleaned = strings.ReplaceAll(cleaned, "`", "")
	cleaned = strings.ReplaceAll(cleaned, "^", "")
	cleaned = strings.ReplaceAll(cleaned, "*", "")
	cleaned = strings.ReplaceAll(cleaned, "=", "")
	cleaned = strings.ReplaceAll(cleaned, "!", "")
	cleaned = strings.ReplaceAll(cleaned, "?", "")
	cleaned = strings.ReplaceAll(cleaned, "_", "")
	cleaned = strings.ReplaceAll(cleaned, "-", "")
	cleaned = strings.ReplaceAll(cleaned, ":", "")
	cleaned = strings.ReplaceAll(cleaned, ";", "")
	cleaned = strings.ReplaceAll(cleaned, "/", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "\\", "")
	cleaned = strings.ToLower(cleaned)

	return cleaned
}
