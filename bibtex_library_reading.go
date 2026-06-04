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

import "strings"

// TWatchEntry is one line from watch.csv describing a person or ORCID to watch.
// EntryType is "name" (canonical DBLP author name) or "orcid".
// Value is the name or ORCID string. Tag is a human-readable label.
type TWatchEntry struct {
	EntryType string
	Value     string
	Tag       string
}

// ReadWatchFile parses watch.csv, skipping blank lines and lines starting with '#'.
// Each data line must be: tag;type;value  (type = "name" or "orcid").
func ReadWatchFile(path string) []TWatchEntry {
	data := readIndexFile(path)
	var entries []TWatchEntry
	for _, line := range data {
		if strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ";", 3)
		if len(parts) < 3 {
			continue
		}
		tag := strings.TrimSpace(parts[0])
		wType := strings.TrimSpace(parts[1])
		if wType != "name" && wType != "orcid" {
			continue
		}
		value := strings.TrimSpace(parts[2])
		if value == "" {
			continue
		}
		entries = append(entries, TWatchEntry{EntryType: wType, Value: value, Tag: tag})
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
	repairCorruptNameMappings() // GO_REVISIT: remove after production deployment confirms blob corruption is gone
	loadNameMappingsFromDb(l)
	l.nameMappingsModified = false
}
