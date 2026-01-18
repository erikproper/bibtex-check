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
	EntryGroups := TStringSetMap{}

	if !l.NoBibFileWriting {
		for group, keys := range l.GroupEntries {
			for key := range keys.Elements() {
				EntryGroups.AddValueToStringSetMap(key, group)
			}
		}

		l.writeLibraryFile(BibFileExtension, ProgressWritingBibFile, func(bibWriter *bufio.Writer) {
			// Write out the entries and their fields, but do so in a crossref friendly order. So first the non-bookish, and then the bookish ones
			for entry := range l.EntryFields {
				if l.EntryType(entry) == "" {
					l.Warning("No entry type for %s. Skipping.", entry)
				} else if !BibTeXBookish.Contains(l.EntryType(entry)) {
					// Should be a function. We do this twice.
					groups := EntryGroups.GetValueSetFromStringSetMap(entry).Stringify()

					bibWriter.WriteString(l.EntryString(entry, groups))
					bibWriter.WriteString("\n")
				}
			}

			for entry := range l.EntryFields {
				if l.EntryType(entry) == "" {
					l.Warning("No entry type for %s. Skipping.", entry)
				} else if BibTeXBookish.Contains(l.EntryType(entry)) {
					// Should be a function. We do this twice.
					groups := EntryGroups.GetValueSetFromStringSetMap(entry).Stringify()

					bibWriter.WriteString(l.EntryString(entry, groups))
					bibWriter.WriteString("\n")
				}
			}

			// Write out the comments
			for _, comment := range l.Comments {
				bibWriter.WriteString("@" + CommentEntryType + "{" + comment + "}\n")
				bibWriter.WriteString("\n")
			}

			// Write out the static groups
			if len(l.GroupEntries) > 0 {
				bibWriter.WriteString("@" + CommentEntryType + "{BibDesk Static Groups{")
				bibWriter.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
				bibWriter.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
				bibWriter.WriteString("<plist version=\"1.0\">\n")
				bibWriter.WriteString("<array>\n")
				for group, keys := range l.GroupEntries {
					bibWriter.WriteString("	<dict>\n")
					bibWriter.WriteString("		<key>group name</key>\n")
					bibWriter.WriteString("		<string>" + group + "</string>\n")
					bibWriter.WriteString("		<key>keys</key>\n")
					bibWriter.WriteString("		<string>")
					comma := ""
					for key := range keys.Elements() {
						bibWriter.WriteString(comma + l.MapEntryKey(key))
						comma = ","
					}
					bibWriter.WriteString("</string>\n")
					bibWriter.WriteString("	</dict>\n")
				}
				bibWriter.WriteString("</array>\n")
				bibWriter.WriteString("</plist>\n")
				bibWriter.WriteString("}}\n")
				bibWriter.WriteString("\n")
			}
		})
	}
}

func (l *TBibTeXLibrary) WriteCache() {
	l.WriteFieldsCache()
	l.WriteGroupsCache()
	l.WriteCommentsCache()
}

func (l *TBibTeXLibrary) WriteGroupsCache() {
	if !l.NoBibFileWriting {
		l.writeLibraryFile(GroupsCacheExtension, ProgressWritingGroupsCache, func(groupsWriter *bufio.Writer) {
			for group, entrySet := range l.GroupEntries {
				for entry := range entrySet.Elements() {
					groupsWriter.WriteString(entry + "	" + group + "\n")
				}
			}
		})
	}
}

func (l *TBibTeXLibrary) WriteFieldsCache() {
	if !l.NoBibFileWriting {
		l.writeLibraryFile(FieldsCacheExtension, ProgressWritingFieldsCache, func(fieldsWriter *bufio.Writer) {
			for entry := range l.EntryFields {
				if l.EntryType(entry) == "" {
					l.Warning("No entry type for %s. Skipping.", entry)
				} else {
					for field := range l.EntryFields[entry] {
						if value := l.EntryFields[entry][field]; value != "" {
							fieldsWriter.WriteString(entry + "	" + field + "	" + l.EntryFieldValueity(entry, field) + "\n")
						}
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
		l.writeLibraryFile(EntryFieldMappingsFileExtension, ProgressWritingEntryFieldAliasesFile, func(challengeWriter *bufio.Writer) {
			for key, fieldChallenges := range l.EntryFieldSourceToTarget {
				if l.EntryExists(key) {
					for field, challenges := range fieldChallenges {
						if field != PreferredAliasField {
							for challenger, winner := range challenges {
								if l.MapFieldValue(field, challenger) != l.MapEntryFieldValue(key, field, winner) {
									challengeWriter.WriteString(key + "\t" + field + "\t" + l.MapEntryFieldValue(key, field, winner) + "\t" + challenger + "\n")
								}
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
				if key == l.MapEntryKey(key) && l.EntryExists(key) {
					for nonDouble := range set.Elements() {
						if nonDouble != key && nonDouble == l.MapEntryKey(nonDouble) && l.EntryExists(nonDouble) {
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
		l.writeLibraryFile(GenericFieldMappingsFileExtension, ProgressWritingGenericFieldAliasesFile, func(challengeWriter *bufio.Writer) {
			for field, challenges := range l.GenericFieldSourceToTarget {
				if field != PreferredAliasField {
					for challenger, winner := range challenges {
						if challenger != winner {
							challengeWriter.WriteString(field + "\t" + l.MapFieldValue(field, winner) + "\t" + challenger + "\n")
						}
					}
				}
			}
		})
	}
}

// Write entry key alias/original pairs to a bufio.bWriter buffer
func (l *TBibTeXLibrary) WriteNameMappingFile() {
	if false && !l.NoNameMappingsFileWriting {
		l.writeLibraryFile(nameMappingsFileExtension, ProgressWritingNameMappingsFile, func(aliasWriter *bufio.Writer) {
			for alias, original := range l.NameAliasToName {
				if alias != original {
					aliasWriter.WriteString(original + "\t" + alias + "\n")
				}
			}
		})
	}
}

// Write entry key alias/original pairs to a bufio.bWriter buffer
func (l *TBibTeXLibrary) WriteKeyOldiesFile() {
	if !l.NoKeyOldiesFileWriting {
		l.writeLibraryFile(KeyOldiesFileExtension, ProgressWritingKeyOldiesFile, func(sourceWriter *bufio.Writer) {
			for source, target := range l.KeyToKey {
				target = l.MapEntryKey(target)
				_, isEntry := l.EntryFields[target]

				if isEntry &&
					source != target &&
					IsValidKey(source) {
					sourceWriter.WriteString(target + "\t" + source + "\n")
				}
			}
		})
	}
}

// Write entry key source/target pairs to a bufio.bWriter buffer
func (l *TBibTeXLibrary) WriteKeyHintsFile() {
	if !l.NoKeyHintsFileWriting {
		l.writeLibraryFile(KeyHintsFileExtension, ProgressWritingKeyHintsFile, func(sourceWriter *bufio.Writer) {
			for source, target := range l.HintToKey {
				target = l.MapEntryKey(target)
				_, isEntry := l.EntryFields[target]

				if isEntry &&
					source != target &&
					source != l.EntryFieldValueity(target, PreferredAliasField) &&
					source != KeyForDBLP(l.EntryFieldValueity(target, DBLPField)) {
					sourceWriter.WriteString(target + "\t" + source + "\n")
				}
			}
		})
	}
}

func (l *TBibTeXLibrary) WriteAliasesFiles() {
	l.WriteKeyOldiesFile()
	l.WriteKeyHintsFile()
	l.WriteNameMappingFile()
	l.WriteGenericFieldAliasesFile()
	l.WriteEntryFieldAliasesFile()
}

func (l *TBibTeXLibrary) WriteMappingsFiles() {
	if !l.NoFieldMappingsFileWriting {
		l.writeLibraryFile(CrossFieldMappingsFileExtension, ProgressWritingFieldMappingsFile, func(writer *bufio.Writer) {
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
