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
	maybeReloadCrossFieldMappingsDb()
	if !crossFieldMappingsFileWritingAllowed {
		l.NoCrossFieldMappingsFileWriting = true
	}
	normalisationChanged := loadCrossFieldMappingsFromDb(l)
	l.crossFieldMappingsModified = normalisationChanged
}

// Read key field challenge file
func (l *TBibTeXLibrary) ReadEntryFieldMappingsFile() {
	maybeReloadEntryFieldMappingsDb()
	if !entryFieldMappingsFileWritingAllowed {
		l.NoEntryFieldMappingsFileWriting = true
	}
	normalisationChanged := loadEntryFieldMappingsFromDb(l)
	l.entryFieldMappingsModified = normalisationChanged
}

// Read generic field challenge file
func (l *TBibTeXLibrary) ReadGenericFieldMappingsFile() {
	maybeReloadGenericFieldMappingsDb()
	if !genericFieldMappingsFileWritingAllowed {
		l.NoGenericFieldMappingsFileWriting = true
	}
	normalisationChanged := loadGenericFieldMappingsFromDb(l)
	l.genericFieldMappingsModified = normalisationChanged
}

func (l *TBibTeXLibrary) ReadKeyOldiesFile() {
	maybeReloadKeyOldiesDb()
	if !keyOldiesFileWritingAllowed {
		l.NoKeyOldiesFileWriting = true
	}
	loadKeyOldiesFromDb(l)
	l.keyOldiesModified = false
}

func (l *TBibTeXLibrary) ReadKeyHintsFile() {
	maybeReloadKeyHintsDb()
	if !keyHintsFileWritingAllowed {
		l.NoKeyHintsFileWriting = true
	}
	loadKeyHintsFromDb(l)
	l.keyHintsModified = false
}

func (l *TBibTeXLibrary) ReadKeyNonDoublesFile() {
	maybeReloadKeyNonDoublesDb()
	if !keyNonDoublesFileWritingAllowed {
		l.NoKeyNonDoublesFileWriting = true
	}
	loadKeyNonDoublesFromDb(l)
	l.keyNonDoublesModified = false
}

func (l *TBibTeXLibrary) ReadDblpParentFile() {
	maybeReloadDblpParentDb()
	if !dblpParentFileWritingAllowed {
		l.NoDblpParentFileWriting = true
	}
	loadDblpParentFromDb(l)
	l.dblpParentModified = false
}

func (l *TBibTeXLibrary) ReadDblpWaivedFile() {
	maybeReloadDblpWaivedDb()
	if !dblpWaivedFileWritingAllowed {
		l.NoDblpWaivedFileWriting = true
	}
	loadDblpWaivedFromDb(l)
	l.dblpWaivedModified = false
}


func (l *TBibTeXLibrary) ReadURLsIgnoreFile() {
	maybeReloadURLsIgnoreDb()
	loadURLsIgnoreFromDb(l)
}

func (l *TBibTeXLibrary) ReadEntryFlagsFile() {
	maybeReloadEntryFlagsDb()
	if !entryFlagsFileWritingAllowed {
		l.NoEntryFlagsFileWriting = true
	}
	loadEntryFlagsFromDb(l)
	l.entryFlagsModified = false
}

func (l *TBibTeXLibrary) ReadAddressMappings() {
	maybeWriteDefaultCsv(bibTeXFolder+bibTeXBaseName+StateNamesFilePath, defaultStateNames)
	maybeWriteDefaultCsv(bibTeXFolder+bibTeXBaseName+StateCountriesFilePath, defaultStateCountries)
	maybeWriteDefaultCsv(bibTeXFolder+bibTeXBaseName+CountryNamesFilePath, defaultCountryNames)
	maybeWriteDefaultCsv(bibTeXFolder+bibTeXBaseName+BooktitleCountryNamesFilePath, defaultBooktitleCountryNames)
	addressReloaded := maybeReloadStateNamesDb()
	loadStateNamesFromDb(l)
	addressReloaded = maybeReloadStateCountriesDb() || addressReloaded
	loadStateCountriesFromDb(l)
	addressReloaded = maybeReloadCountryNamesDb() || addressReloaded
	loadCountryNamesFromDb(l)
	addressReloaded = maybeReloadBooktitleCountryNamesDb() || addressReloaded
	loadBooktitleCountryNamesFromDb(l)
	if addressReloaded {
		setTableDate("filter_cross_field_mappings", 0)
		setTableDate("filter_entry_field_mappings", 0)
		setTableDate("filter_generic_field_mappings", 0)
	}
}

func (l *TBibTeXLibrary) ReadNameMappingsFile() {
	maybeReloadNameMappingsDb()
	if !nameMappingsFileWritingAllowed {
		l.NoNameMappingsFileWriting = true
	}
	loadNameMappingsFromDb(l)
	l.nameMappingsModified = false
}
