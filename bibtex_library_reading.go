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
	"bufio"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// Read bib files
func (l *TBibTeXLibrary) ReadBib(filePath string) bool {
	//	l.BibFilePath = filePath
	FullFilePath := l.FilesRoot + l.BaseName + BibFileExtension

	return l.ParseBibFile(FullFilePath)
	//l.FilesRoot + l.BibFilePath)
}

// Generic function to read library related files
func (l *TBibTeXLibrary) readFile(fullFilePath, message string, reading func(string)) bool {
	if message != "" {
		l.Progress(message, fullFilePath)
	}

	file, err := os.Open(fullFilePath)
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		reading(string(scanner.Text()))
	}

	return scanner.Err() == nil
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
		CacheModTime := ModTime(FieldsCacheFile)

		// Maybe do this via a set?
		return CacheModTime > ModTime(FileBase+BibFileExtension) &&
			CacheModTime > ModTime(FileBase+NameMappingsFileExtension) &&
			CacheModTime > ModTime(FileBase+KeyOldiesFileExtension) &&
			CacheModTime > ModTime(FileBase+EntryFieldMappingsFileExtension) &&
			CacheModTime > ModTime(FileBase+GenericFieldMappingsFileExtension) &&
			CacheModTime > ModTime(FileBase+CrossFieldMappingsFileExtension) &&
			CacheModTime > ModTime(FileBase+NonDoublesFileExtension)
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

func (l *TBibTeXLibrary) ReadFieldMappingsFile() {
	l.readLibraryFile(CrossFieldMappingsFileExtension, ProgressReadingFieldMappingsFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 4 {
			l.Warning(WarningFieldMappingsTooShort, line)
			l.NoFieldMappingsFileWriting = true
			return
		}

		sourceField := elements[0]
		sourceValue := l.MapFieldValue(sourceField, elements[1])

		targetField := elements[2]
		targetValue := l.NormaliseFieldValue(targetField, elements[3])

		l.AddFieldMapping(sourceField, sourceValue, targetField, targetValue)
	})
}

// Read key field challenge file
func (l *TBibTeXLibrary) ReadEntryFieldAliasesFile() {
	l.readLibraryFile(EntryFieldMappingsFileExtension, ProgressReadingEntryFieldAliasesFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 4 {
			l.Warning(WarningEntryFieldMappingsLineTooShort, line)
			l.NoEntryFieldAliasesFileWriting = true
			return
		}

		key := elements[0]
		field := elements[1]
		winner := l.MapNormalisedFieldValue(field, elements[2])
		challenger := l.MapNormalisedFieldValue(field, elements[3])
		l.AddEntryFieldAlias(key, field, challenger, winner, true)
	})
}

// Read generic field challenge file
func (l *TBibTeXLibrary) ReadGenericFieldAliasesFile() {
	l.readLibraryFile(GenericFieldMappingsFileExtension, ProgressReadingGenericFieldAliasesFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 3 {
			l.Warning(WarningGenericFieldMappingsLineTooShort, line)
			l.NoGenericFieldAliasesFileWriting = true
			return
		}

		field := elements[0]
		winner := l.NormaliseFieldValue(field, elements[1])
		challenger := l.NormaliseFieldValue(field, elements[2])
		l.AddGenericFieldAlias(field, challenger, winner, true)
	})
}

func (l *TBibTeXLibrary) ReadKeyOldiesFile() {
	l.readLibraryFile(KeyOldiesFileExtension, ProgressReadingKeyOldiesFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 2 {
			l.Warning(WarningKeyAliasesLineTooShort, line)
			l.NoKeyOldiesFileWriting = true
			return
		}

		l.AddKeyAlias(elements[1], elements[0])
	})
}

func (l *TBibTeXLibrary) ReadKeyHintsFile() {
	l.readLibraryFile(KeyHintsFileExtension, ProgressReadingKeyHintsFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 2 {
			l.Warning(WarningKeyHintsLineTooShort, line)
			l.NoKeyHintsFileWriting = true
			return
		}

		l.AddKeyHint(elements[1], elements[0])
	})
}

func (l *TBibTeXLibrary) ReadNonDoublesFile() {
	l.readLibraryFile(NonDoublesFileExtension, ProgressReadingNonDoublesFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 2 {
			l.Warning(WarningNonDoublesLineTooShort, line)
			l.NoNonDoublesFileWriting = true
			return
		}

		// Why pass on &l.NameAliasToName, &l.NameToAliases???
		l.AddNonDoubles(elements[0], elements[1])
	})
}

func (l *TBibTeXLibrary) ReadNameMappingsFile() {
	connectToDatabase(l.FilesRoot, l.BaseName)

	query := `INSERT INTO name_aliases (alias, name) VALUES (?, ?) ON CONFLICT(alias) DO UPDATE SET name = excluded.name;`

	l.readLibraryFile(NameMappingsFileExtension, ProgressReadingNameMappingsFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 2 {
			l.Warning(WarningNameMappingsLineTooShort, line)
			l.NoNameMappingsFileWriting = true
			return
		}

		_, err := db.Exec(query, ApplyLaTeXMap(elements[1]), ApplyLaTeXMap(elements[0]))
		if err != nil {
			l.Warning("WarningNameMappingsInsertFailed", err)
		}

		// Why pass on &l.NameAliasToName, &l.NameToAliases???
		l.AddAliasForName(ApplyLaTeXMap(elements[1]), ApplyLaTeXMap(elements[0]), &l.NameAliasToName, &l.NameToAliases)
	})
}
