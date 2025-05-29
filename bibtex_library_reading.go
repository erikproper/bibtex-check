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
	// "fmt"
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
			CacheModTime > ModTime(FileBase+NameAliasesFileExtension) &&
			CacheModTime > ModTime(FileBase+KeyAliasesFileExtension) &&
			CacheModTime > ModTime(FileBase+EntryFieldAliasesFileExtension) &&
			CacheModTime > ModTime(FileBase+GenericFieldAliasesFileExtension) &&
			CacheModTime > ModTime(FileBase+FieldMappingsFileExtension) &&
			CacheModTime > ModTime(FileBase+NonDoublesFileExtension)
	} else {
		return false
	}
}

func (l *TBibTeXLibrary) ReadCache() {
	l.ReadFieldsCache()
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

		l.SetEntryFieldValue(key, field, l.ProcessCachedEntryFieldValue(key, field, value))
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
	l.readLibraryFile(FieldMappingsFileExtension, ProgressReadingFieldMappingsFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 4 {
			l.Warning(WarningFieldMappingsTooShort, line)
			l.NoFieldMappingsFileWriting = true
			return
		}

		sourceField := elements[0]
		sourceValue := l.UpdateFieldValue(sourceField, elements[1])

		targetField := elements[2]
		targetValue := l.UpdateFieldValue(targetField, elements[3])

		l.AddFieldMapping(sourceField, sourceValue, targetField, targetValue)
	})
}

func (l *TBibTeXLibrary) normalisedAliasTargetPair(key, field, winner, challenger string) (string, string) {
	normalisedWinner := ""
	if winner != "" {
		normalisedWinner = l.NormaliseFieldValue(field, key, winner)
	}

	// We do normalise the challengers, but want to ignore error messages.
	// The challenged values may actually have errors ...
	normalisedChallenger := ""
	if challenger != "" {
		silenced := l.InteractionIsOff()
		l.SetInteractionOff()
		normalisedChallenger = l.NormaliseFieldValue(field, key, challenger)
		l.SetInteraction(silenced)
	}

	return normalisedWinner, normalisedChallenger
}

// Read key field challenge file
func (l *TBibTeXLibrary) ReadEntryFieldAliasesFile() {
	l.readLibraryFile(EntryFieldAliasesFileExtension, ProgressReadingEntryFieldAliasesFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 4 {
			l.Warning(WarningEntryFieldAliasesLineTooShort, line)
			l.NoEntryFieldAliasesFileWriting = true
			return
		}

		key := elements[0]
		field := elements[1]
		winner, challenger := l.normalisedAliasTargetPair(key, field, elements[2], elements[3])
		l.AddEntryFieldAlias(key, field, l.DeAliasFieldValue(field, challenger), l.DeAliasFieldValue(field, winner), true)
	})
}

// Read generic field challenge file
func (l *TBibTeXLibrary) ReadGenericFieldAliasesFile() {
	l.readLibraryFile(GenericFieldAliasesFileExtension, ProgressReadingGenericFieldAliasesFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 3 {
			l.Warning(WarningGenericFieldAliasesLineTooShort, line)
			l.NoGenericFieldAliasesFileWriting = true
			return
		}

		field := elements[0]
		winner, challenger := l.normalisedAliasTargetPair("", field, elements[1], elements[2])
		l.AddGenericFieldAlias(field, challenger, winner, true)
	})
}

func (l *TBibTeXLibrary) ReadKeyAliasesFile() {
	l.readLibraryFile(KeyAliasesFileExtension, ProgressReadingKeyAliasesFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 2 {
			l.Warning(WarningKeyAliasesLineTooShort, line)
			l.NoKeyAliasesFileWriting = true
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

func (l *TBibTeXLibrary) ReadNameAliasesFile() {
	l.readLibraryFile(NameAliasesFileExtension, ProgressReadingNameAliasesFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 2 {
			l.Warning(WarningNameAliasesLineTooShort, line)
			l.NoNameAliasesFileWriting = true
			return
		}

		// Why pass on &l.NameAliasToName, &l.NameToAliases???
		l.AddAliasForName(ApplyLaTeXMap(elements[1]), ApplyLaTeXMap(elements[0]), &l.NameAliasToName, &l.NameToAliases)
	})
}
