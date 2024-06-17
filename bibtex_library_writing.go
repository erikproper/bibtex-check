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
	//"fmt"
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
// - When we start managing the groups (of keys) the way Bibdesk does, we need to ensure that their embedded as an XML structure embedded in a comment, is updated.
func (l *TBibTeXLibrary) WriteBibTeXFile() {
	if !l.NoBibFileWriting {
		l.writeLibraryFile(BibFileExtension, ProgressWritingBibFile, func(bibWriter *bufio.Writer) {
			// Write out the entries and their fields
			for entry := range l.EntryTypes {
				bibWriter.WriteString(l.EntryString(entry))
				bibWriter.WriteString("\n")
			}

			if !l.migrationMode {
				// Write out the comments
				for _, comment := range l.Comments {
					bibWriter.WriteString("@" + CommentEntryType + "{" + comment + "}\n")
					bibWriter.WriteString("\n")
				}
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
				if key == l.DeAliasEntryKey(key) {
					for nonDouble := range set.Elements() {
						if nonDouble != key && nonDouble == l.DeAliasEntryKey(nonDouble) {
							if !l.ProvenNonDoubleness(key, nonDouble) {
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

// Write the preferred key aliases from this library, to a bufio.bWriter buffer
func (l *TBibTeXLibrary) WritePreferredKeyAliasesFile() {
	if !l.NoPreferredKeyAliasesFileWriting {
		l.writeLibraryFile(PreferredKeyAliasesFileExtension, ProgressWritingPreferredKeyAliasesFile, func(aliasWriter *bufio.Writer) {
			for key, alias := range Library.PreferredKeyAliases {
				if key != alias && AllowLegacy {
					aliasWriter.WriteString(alias + "\n")
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
	l.WritePreferredKeyAliasesFile()
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
