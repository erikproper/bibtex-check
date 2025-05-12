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
	//"strings"
	//"fmt"
	"os"
)

func (l *TBibTeXLibrary) writeFile(fullFilePath, message string, writing func(*bufio.Writer)) bool {
	l.Progress(message, fullFilePath)

	BackupFile(fullFilePath)

	file, err := os.Create(fullFilePath)
	if err != nil {
		return false
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	writing(writer)
	writer.Flush()

	return true
}

// Generic function to write library related files
func (l *TBibTeXLibrary) writeLibraryFile(fileExtension, message string, writing func(*bufio.Writer)) bool {
	return l.writeFile(l.FilesRoot+l.BaseName+fileExtension, message, writing)
}

// Function to write the BibTeX content of the library to a bufio.bWriter buffer
// Notes:
// - As we ignore preambles, these are not written.
func (l *TBibTeXLibrary) WriteBibTeXFile() {
	if !l.NoBibFileWriting {
		l.writeLibraryFile(BibFileExtension, ProgressWritingBibFile, func(bibWriter *bufio.Writer) {
			// Write out the entries and their fields, but do so in a crossref friendly order. So first the non-bookish, and then the bookish ones
			for entry := range l.EntryFields {
				if !BibTeXBookish.Contains(l.EntryType(entry)) {
					bibWriter.WriteString(l.EntryString(entry))
					bibWriter.WriteString("\n")
				}
			}

			for entry := range l.EntryFields {
				if BibTeXBookish.Contains(l.EntryType(entry)) {
					bibWriter.WriteString(l.EntryString(entry))
					bibWriter.WriteString("\n")
				}
			}

			// Write out the comments
			for _, comment := range l.Comments {
				bibWriter.WriteString("@" + CommentEntryType + "{" + comment + "}\n")
				bibWriter.WriteString("\n")
			}
		})
	}
}

func (l *TBibTeXLibrary) WriteCache() {
	l.WriteFieldsCache()
	l.WriteCommentsCache()
}

func (l *TBibTeXLibrary) WriteFieldsCache() {
	if !l.NoBibFileWriting {
		l.writeLibraryFile(FieldsCacheExtension, ProgressWritingFieldsCache, func(fieldsWriter *bufio.Writer) {
			for entry := range l.EntryFields {
				for field := range l.EntryFields[entry] {
					if value := l.EntryFields[entry][field]; value != "" {
						fieldsWriter.WriteString(entry + "	" + field + "	" + l.EntryFieldValueity(entry, field) + "\n")
					}
				}
			}
		})
	}
}

func (l *TBibTeXLibrary) WriteCommentsCache() {
	if !l.NoBibFileWriting {
		l.writeLibraryFile(CommentsCacheExtension, ProgressWritingCommentsCache, func(commentsWriter *bufio.Writer) {
			for entry := range l.Comments {
				commentsWriter.WriteString(CacheCommentsSeparator + "\n")
				commentsWriter.WriteString(l.Comments[entry] + "\n")
			}
		})
	}
}

// Write the challenges and winners for field values, of this library, to a file
func (l *TBibTeXLibrary) WriteEntryFieldAliasesFile() {
	if !l.NoEntryFieldAliasesFileWriting {
		l.writeLibraryFile(EntryFieldAliasesFileExtension, ProgressWritingEntryFieldAliasesFile, func(challengeWriter *bufio.Writer) {
			for key, fieldChallenges := range l.EntryFieldAliasToTarget {
				if l.EntryExists(key) {
					for field, challenges := range fieldChallenges {
						for challenger, winner := range challenges {
							if l.DeAliasFieldValue(field, challenger) != l.DeAliasEntryFieldValue(key, field, winner) {
								challengeWriter.WriteString(key + "\t" + field + "\t" + l.DeAliasEntryFieldValue(key, field, winner) + "\t" + challenger + "\n")
							}
						}
					}
				}
			}
		})
	}
}

func (l *TBibTeXLibrary) WriteNonDoublesFile() {
	if !l.NoNonDoublesFileWriting {
		l.writeLibraryFile(NonDoublesFileExtension, ProgressWritingNonDoublesFile, func(challengeWriter *bufio.Writer) {
			for key, set := range l.NonDoubles {
				if key == l.DeAliasEntryKey(key) && l.EntryExists(key) {
					for nonDouble := range set.Elements() {
						if nonDouble != key && nonDouble == l.DeAliasEntryKey(nonDouble) && l.EntryExists(nonDouble) {
							if !l.EvidenceForBeingDifferentEntries(key, nonDouble) {
								challengeWriter.WriteString(key + "\t" + nonDouble + "\n")
							}
						}
					}
				}
			}
		})
	}
}

func (l *TBibTeXLibrary) WriteGenericFieldAliasesFile() {
	if !l.NoGenericFieldAliasesFileWriting {
		l.writeLibraryFile(GenericFieldAliasesFileExtension, ProgressWritingGenericFieldAliasesFile, func(challengeWriter *bufio.Writer) {
			for field, challenges := range l.GenericFieldAliasToTarget {
				for challenger, winner := range challenges {
					if challenger != winner {
						challengeWriter.WriteString(field + "\t" + l.DeAliasFieldValue(field, winner) + "\t" + challenger + "\n")
					}
				}
			}
		})
	}
}

// Write entry key alias/original pairs to a bufio.bWriter buffer
func (l *TBibTeXLibrary) WriteNameAliasesFile() {
	if !l.NoNameAliasesFileWriting {
		l.writeLibraryFile(NameAliasesFileExtension, ProgressWritingNameAliasesFile, func(aliasWriter *bufio.Writer) {
			for alias, original := range l.NameAliasToName {
				if alias != original {
					aliasWriter.WriteString(original + "\t" + alias + "\n")
				}
			}
		})
	}
}

// Write entry key alias/original pairs to a bufio.bWriter buffer
func (l *TBibTeXLibrary) WriteKeyAliasesFile() {
	if !l.NoKeyAliasesFileWriting {
		l.writeLibraryFile(KeyAliasesFileExtension, ProgressWritingKeyAliasesFile, func(aliasWriter *bufio.Writer) {
			for alias, original := range l.KeyAliasToKey {
				if alias != original {
					aliasWriter.WriteString(original + "\t" + alias + "\n")
				}
			}
		})
	}
}

func (l *TBibTeXLibrary) WriteAliasesFiles() {
	l.WriteKeyAliasesFile()
	l.WriteNameAliasesFile()
	l.WriteGenericFieldAliasesFile()
	l.WriteEntryFieldAliasesFile()
}

func (l *TBibTeXLibrary) WriteMappingsFiles() {
	if !l.NoFieldMappingsFileWriting {
		l.writeLibraryFile(FieldMappingsFileExtension, ProgressWritingFieldMappingsFile, func(writer *bufio.Writer) {
			for sourceField, sourceFieldMappings := range l.FieldMappings {
				for sourceValue, targetFieldMappings := range sourceFieldMappings {
					for targetField, targetValue := range targetFieldMappings {
						writer.WriteString(sourceField + "\t" + sourceValue + "\t" + targetField + "\t" + targetValue + "\n")
					}
				}
			}
		})
	}
}
