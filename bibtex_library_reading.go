/*
 *
 * Module: bibtex_library_writing
 *
 * This module is adds the functionality (for TBibTeXLibrary) to write out BibTeX and associated files
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 24.04.2024
 *
 */

package main

import (
	"fmt"
	"os"
	"strings"
)

// TWatchEntry is one entry from the watch script file describing a person or ORCID to watch.
// EntryType is "name" (canonical DBLP author name) or "orcid".
// Value is the name or ORCID string.
// Comment holds any inline # annotation (e.g. the person's name for orcid entries).
type TWatchEntry struct {
	EntryType string
	Value     string
	Tag       string // unused in the new script format; kept for caller compatibility
	Comment   string // inline annotation after #, e.g. "Proper, Henderik A."
}

// ReadWatchFile parses the watch script file (ErikProper.scripts/watch).
// Format is the same script style as .select files:
//
//	name  "Canonical Author Name";
//	orcid "0000-0001-2345-6789"; # Canonical Author Name
//
// Blank lines and lines starting with # are ignored.
// Inline # comments are stripped before parsing and stored in TWatchEntry.Comment.
func ReadWatchFile(path string) []TWatchEntry {
	if !FileExists(path) {
		return nil
	}
	var entries []TWatchEntry
	var badLines []string
	processFile(path, func(line string) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return
		}
		// Strip inline comment, preserving it for the entry.
		comment := ""
		if ci := strings.Index(line, "#"); ci >= 0 {
			comment = strings.TrimSpace(line[ci+1:])
			line = strings.TrimSpace(line[:ci])
		}
		line = strings.TrimSuffix(line, ";")
		idx := strings.IndexByte(line, '"')
		if idx < 0 {
			badLines = append(badLines, line)
			return
		}
		kind := strings.TrimSpace(line[:idx])
		if kind != "name" && kind != "orcid" {
			badLines = append(badLines, line)
			return
		}
		rest := line[idx:]
		for {
			start := strings.IndexByte(rest, '"')
			if start < 0 {
				break
			}
			rest = rest[start+1:]
			end := strings.IndexByte(rest, '"')
			if end < 0 {
				break
			}
			v := strings.TrimSpace(rest[:end])
			if v != "" {
				entries = append(entries, TWatchEntry{EntryType: kind, Value: v, Comment: comment})
			}
			rest = rest[end+1:]
		}
	})
	for _, bl := range badLines {
		fmt.Fprintf(os.Stderr, "WARNING: %s: unrecognised line (expected: name/orcid \"value\";): %q\n", path, bl)
	}
	return entries
}

// WriteWatchFile writes entries back to the watch file, preserving inline name comments.
func WriteWatchFile(path string, entries []TWatchEntry) {
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(e.EntryType)
		sb.WriteString(` "`)
		sb.WriteString(e.Value)
		sb.WriteString(`";`)
		if e.Comment != "" {
			sb.WriteString(" # ")
			sb.WriteString(e.Comment)
		}
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: could not write watch file %s: %s\n", path, err)
	}
}

// Read bib files
func (l *TBibTeXLibrary) ReadBib(filePath string) bool {
	FullFilePath := l.FilesRoot + l.BaseName + BibFileExtension
	l.Progress(ProgressReadingBibFile, FullFilePath)
	l.harvestNameAliases = true
	defer func() { l.harvestNameAliases = false }()
	return l.ParseBibFile(FullFilePath)
}

// Generic function to read library related files
func (l *TBibTeXLibrary) readFile(fullFilePath, message string, reading func(string)) bool {
	if message != "" {
		l.Progress(message, fullFilePath)
	}

	return processFile(fullFilePath, reading)
}

func (l *TBibTeXLibrary) ForEachChildOfDBLPKey(DBLPKey string, work func(string)) {
	for _, child := range readDblpCrossrefChildren(DBLPKey) {
		work(child)
	}
}

func (l *TBibTeXLibrary) MaybeGetDBLPCrossref(DBLPKey string) string {
	je := readDblpJSONEntry(DBLPKey)
	if je == nil {
		return ""
	}
	return je.Fields["crossref"]
}

// Generic function to read library related files
func (l *TBibTeXLibrary) readLibraryFile(fileExtension, message string, reading func(string)) bool {
	return l.readFile(l.FilesRoot+l.BaseName+fileExtension, message, reading)
}

func (l *TBibTeXLibrary) ReadFieldMappingsFile() {
	loadFieldMappingsFromDb(l)
}

func (l *TBibTeXLibrary) ReadEntryFieldMappingsFile() {
	loadEntryFieldMappingsFromDb(l)
}

func (l *TBibTeXLibrary) ReadKeyOldiesFile() {
	l.KeyOldies.Load()
	l.Progress("Key oldies: %d entries", l.KeyOldies.Len())
}

func (l *TBibTeXLibrary) ReadKeyHintsFile() {
	l.HintToKey.Load()
	l.Progress("Key hints: %d entries", l.HintToKey.Len())
}

func (l *TBibTeXLibrary) ReadKeyNonDoublesFile() {
	loadKeyNonDoublesFromDb(l)
	l.Progress("Key non-doubles: %d entries", len(l.NonDoubleEntries))
}

func (l *TBibTeXLibrary) ReadDblpParentFile() {
	l.DblpParent.Load()
	l.Progress("DBLP parent overrides: %d entries", l.DblpParent.Len())
}

func (l *TBibTeXLibrary) ReadDblpWaivedFile() {
	l.DblpWaived.Load()
	l.Progress("DBLP waived: %d entries", l.DblpWaived.Len())
}

func (l *TBibTeXLibrary) ReadURLsIgnoreFile() {
	loadURLsIgnoreFromDb(l)
}

func (l *TBibTeXLibrary) ReadEntryFlagsFile() {
	loadEntryFlagsFromDb(l)
	total := 0
	for _, s := range l.EntryFlags {
		total += len(s.Elements())
	}
	l.Progress("Entry flags: %d flags on %d entries", total, len(l.EntryFlags))
}

func (l *TBibTeXLibrary) ReadAddressMappings() {
	maybeBootstrapStateNamesTable()
	maybeBootstrapStateCountriesTable()
	maybeBootstrapCountryNamesTable()
	maybeBootstrapBooktitleCountryNamesTable()
	loadStateNamesFromDb(l)
	loadStateCountriesFromDb(l)
	loadCountryNamesFromDb(l)
	loadBooktitleCountryNamesFromDb(l)
}

func (l *TBibTeXLibrary) ReadNameMappingsFile() {
	maybeMergeSpuriousContributors()
	loadContributorsFromDb(l)
	loadContributorIDOldiesFromDB(l)
}
