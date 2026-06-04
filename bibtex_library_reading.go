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
type TWatchEntry struct {
	EntryType string
	Value     string
	Tag       string // unused in the new script format; kept for caller compatibility
}

// ReadWatchFile parses the watch script file (ErikProper.scripts/watch).
// Format is the same script style as .select files:
//
//	name  "Canonical Author Name";
//	orcid "0000-0001-2345-6789";
//
// Blank lines and lines starting with # are ignored.
func ReadWatchFile(path string) []TWatchEntry {
	if !FileExists(path) {
		return nil
	}
	var entries []TWatchEntry
	var badLines []string
	processFile(path, func(line string) {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ";"))
		if line == "" || strings.HasPrefix(line, "#") {
			return
		}
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
				entries = append(entries, TWatchEntry{EntryType: kind, Value: v})
			}
			rest = rest[end+1:]
		}
	})
	for _, bl := range badLines {
		fmt.Fprintf(os.Stderr, "WARNING: %s: unrecognised line (expected: name/orcid \"value\";): %q\n", path, bl)
	}
	return entries
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

func (l *TBibTeXLibrary) ReadCrossFieldMappingsFile() {
	normalisationChanged := loadCrossFieldMappingsFromDb(l)
	l.crossFieldMappingsModified = normalisationChanged
}

func (l *TBibTeXLibrary) ReadEntryFieldMappingsFile() {
	normalisationChanged := loadEntryFieldMappingsFromDb(l)
	l.entryFieldMappingsModified = normalisationChanged
}

func (l *TBibTeXLibrary) ReadGenericFieldMappingsFile() {
	normalisationChanged := loadGenericFieldMappingsFromDb(l)
	l.genericFieldMappingsModified = normalisationChanged
}

func (l *TBibTeXLibrary) ReadKeyOldiesFile() {
	loadKeyOldiesFromDb(l)
	l.keyOldiesModified = false
}

func (l *TBibTeXLibrary) ReadKeyHintsFile() {
	loadKeyHintsFromDb(l)
	l.keyHintsModified = false
}

func (l *TBibTeXLibrary) ReadKeyNonDoublesFile() {
	loadKeyNonDoublesFromDb(l)
	l.keyNonDoublesModified = false
}

func (l *TBibTeXLibrary) ReadDblpParentFile() {
	loadDblpParentFromDb(l)
	l.dblpParentModified = false
}

func (l *TBibTeXLibrary) ReadDblpWaivedFile() {
	loadDblpWaivedFromDb(l)
	l.dblpWaivedModified = false
}

func (l *TBibTeXLibrary) ReadURLsIgnoreFile() {
	loadURLsIgnoreFromDb(l)
}

func (l *TBibTeXLibrary) ReadEntryFlagsFile() {
	loadEntryFlagsFromDb(l)
	l.entryFlagsModified = false
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
	loadNameMappingsFromDb(l)
	l.nameMappingsModified = false
}
