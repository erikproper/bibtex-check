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
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// Read bib files
func (l *TBibTeXLibrary) ReadBib(filePath string) bool {
	FullFilePath := l.FilesRoot + l.BaseName + BibFileExtension
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

func (l *TBibTeXLibrary) readDBLPKeyFile(DBLPKey, fileName string, reading func(string)) bool {
	return l.readFile(l.FilesRoot+"DBLPScraper/bib/"+DBLPKey+"/"+fileName, "", reading)
}

func (l *TBibTeXLibrary) ForEachChildOfDBLPKey(DBLPKey string, work func(string)) {
	l.readDBLPKeyFile(DBLPKey, "children", work)
}

func (l *TBibTeXLibrary) MaybeGetDBLPCrossref(DBLPKey string) string {
	crossrefDBLPKey := ""

	l.readDBLPKeyFile(DBLPKey, "crossref", func(key string) {
		if key != "" {
			crossrefDBLPKey = key
		}
	})

	return crossrefDBLPKey
}

// Generic function to read library related files
func (l *TBibTeXLibrary) readLibraryFile(fileExtension, message string, reading func(string)) bool {
	return l.readFile(l.FilesRoot+l.BaseName+fileExtension, message, reading)
}

func (l *TBibTeXLibrary) ValidCache() bool {
	FileBase := l.FilesRoot + l.BaseName

	FieldsCacheFile := FileBase + FieldsCacheExtension
	CommentsCacheFile := FileBase + CommentsCacheExtension

	// Maybe do this via a set?
	if FileExists(FieldsCacheFile) && FileExists(CommentsCacheFile) {
		CacheModTime := fileModTime(FieldsCacheFile)

		// Maybe do this via a set?
		return CacheModTime > fileModTime(FileBase+BibFileExtension) &&
			CacheModTime > fileModTime(FileBase+NameMappingsFilePath) &&
			CacheModTime > fileModTime(FileBase+KeyOldiesFilePath) &&
			CacheModTime > fileModTime(FileBase+EntryFieldMappingsFilePath) &&
			CacheModTime > fileModTime(FileBase+GenericFieldMappingsFilePath) &&
			CacheModTime > fileModTime(FileBase+CrossFieldMappingsFilePath) &&
			CacheModTime > fileModTime(FileBase+KeyNonDoublesFilePath)
	} else {
		return false
	}
}

func (l *TBibTeXLibrary) ReadCache() {
	l.ReadFieldsCache()
	l.ReadGroupsCache()
	l.ReadCommentsCache()
}

func (l *TBibTeXLibrary) ReadFieldsCache() {
	l.readLibraryFile(FieldsCacheExtension, ProgressReadingFieldsCache, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 3 {
			l.Warning("SOME WARNING %s", line)
			return
		}

		key := elements[0]
		field := elements[1]
		value := elements[2]

		l.ProcessEntryFieldValue(key, field, value)
	})
}

func (l *TBibTeXLibrary) ReadGroupsCache() {
	l.readLibraryFile(GroupsCacheExtension, ProgressReadingGroupsCache, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 2 {
			l.Warning("SOME WARNING %s", line)
			return
		}

		l.GroupEntries.AddValueToStringSetMap(elements[1], elements[0])
	})
}

func (l *TBibTeXLibrary) ReadCommentsCache() {
	l.readLibraryFile(CommentsCacheExtension, ProgressReadingCommentsCache, func(line string) {
		if line == CacheCommentsSeparator {
			l.Comments = append(l.Comments, "")
		} else {
			index := len(l.Comments) - 1
			if l.Comments[index] == "" {
				l.Comments[index] = line
			} else {
				l.Comments[index] += "\n" + line
			}
		}
	})
}

func (l *TBibTeXLibrary) ReadCrossFieldMappingsFile() {
	maybeReloadCrossFieldMappingsDb()
	if !crossFieldMappingsFileWritingAllowed {
		l.NoCrossFieldMappingsFileWriting = true
	}
	loadCrossFieldMappingsFromDb(l)
}

// Read key field challenge file
func (l *TBibTeXLibrary) ReadEntryFieldMappingsFile() {
	maybeReloadEntryFieldMappingsDb()
	if !entryFieldMappingsFileWritingAllowed {
		l.NoEntryFieldMappingsFileWriting = true
	}
	loadEntryFieldMappingsFromDb(l)
}

// Read generic field challenge file
func (l *TBibTeXLibrary) ReadGenericFieldMappingsFile() {
	maybeReloadGenericFieldMappingsDb()
	if !genericFieldMappingsFileWritingAllowed {
		l.NoGenericFieldMappingsFileWriting = true
	}
	loadGenericFieldMappingsFromDb(l)
}

func (l *TBibTeXLibrary) ReadKeyOldiesFile() {
	maybeReloadKeyOldiesDb()
	if !keyOldiesFileWritingAllowed {
		l.NoKeyOldiesFileWriting = true
	}
	loadKeyOldiesFromDb(l)
}

func (l *TBibTeXLibrary) ReadKeyHintsFile() {
	maybeReloadKeyHintsDb()
	if !keyHintsFileWritingAllowed {
		l.NoKeyHintsFileWriting = true
	}
	loadKeyHintsFromDb(l)
}

func (l *TBibTeXLibrary) ReadKeyNonDoublesFile() {
	maybeReloadKeyNonDoublesDb()
	if !keyNonDoublesFileWritingAllowed {
		l.NoKeyNonDoublesFileWriting = true
	}
	loadKeyNonDoublesFromDb(l)
	l.keyNonDoublesModified = false
}

func (l *TBibTeXLibrary) ReadNameMappingsFile() {
	maybeReloadNameMappingsDb()
	if !nameMappingsFileWritingAllowed {
		l.NoNameMappingsFileWriting = true
	}
	loadNameMappingsFromDb(l)
	l.nameMappingsModified = false
}
