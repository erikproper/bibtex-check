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
	"path/filepath"
	"sort"
)

func (l *TBibTeXLibrary) writeFile(fullFilePath, message string, writing func(*bufio.Writer)) bool {
	l.Progress(message, fullFilePath)

	if err := os.MkdirAll(filepath.Dir(fullFilePath), 0755); err != nil {
		return false
	}

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
		EntryGroups := TStringSetMap{}
		for group, keys := range l.GroupEntries {
			for key := range keys.Elements() {
				EntryGroups.AddValueToStringSetMap(key, group)
			}
		}

		entryTypes := map[string]string{}
		forEachBibEntryType(func(key, entryType string) {
			entryTypes[key] = entryType
		})

		l.writeLibraryFile(BibFileExtension, ProgressWritingBibFile, func(bibWriter *bufio.Writer) {
			// non-bookish entries first (crossref-friendly order)
			for entry, entryType := range entryTypes {
				if !BibTeXBookish.Contains(entryType) {
					groups := EntryGroups.GetValueSetFromStringSetMap(entry).Stringify()
					bibWriter.WriteString(l.EntryString(entry, groups))
					bibWriter.WriteString("\n")
				}
			}

			// bookish entries second
			for entry, entryType := range entryTypes {
				if BibTeXBookish.Contains(entryType) {
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
			forEachBibGroup(func(groupName, entryKey string) {
				groupsWriter.WriteString(entryKey + "	" + groupName + "\n")
			})
		})
	}
}

func (l *TBibTeXLibrary) WriteFieldsCache() {
	if !l.NoBibFileWriting {
		l.writeLibraryFile(FieldsCacheExtension, ProgressWritingFieldsCache, func(fieldsWriter *bufio.Writer) {
			forEachBibField(func(key, field, value string) {
				if value != "" {
					fieldsWriter.WriteString(key + "	" + field + "	" + value + "\n")
				}
			})
		})
	}
}

func (l *TBibTeXLibrary) WriteCommentsCache() {
	if !l.NoBibFileWriting {
		l.writeLibraryFile(CommentsCacheExtension, ProgressWritingCommentsCache, func(commentsWriter *bufio.Writer) {
			forEachBibComment(func(content string) {
				commentsWriter.WriteString(CacheCommentsSeparator + "\n")
				commentsWriter.WriteString(content + "\n")
			})
		})
	}
}

// Write the challenges and winners for field values, of this library, to a file
func (l *TBibTeXLibrary) WriteEntryFieldMappingsFile() {
	if !l.NoEntryFieldMappingsFileWriting {
		l.writeLibraryFile(EntryFieldMappingsFilePath, ProgressWritingEntryFieldMappingsFile, func(w *bufio.Writer) {
			var lines []string
			for key, fieldChallenges := range l.EntryFieldSourceToTarget {
				if l.EntryExists(key) {
					for field, challenges := range fieldChallenges {
						if field != PreferredAliasField {
							for challenger, winner := range challenges {
								if l.MapFieldValue(field, challenger) != l.MapEntryFieldValue(key, field, winner) {
									lines = append(lines, key+csvDelimiter+field+csvDelimiter+l.MapEntryFieldValue(key, field, winner)+csvDelimiter+challenger)
								}
							}
						}
					}
				}
			}
			sort.Strings(lines)
			for _, line := range lines {
				w.WriteString(line + "\n")
			}
		})
		saveEntryFieldMappingsToDb(l)
	}
}

func (l *TBibTeXLibrary) WriteKeyNonDoublesFile() {
	if !l.NoKeyNonDoublesFileWriting {
		l.writeLibraryFile(KeyNonDoublesFilePath, ProgressWritingNonDoublesFile, func(w *bufio.Writer) {
			var lines []string
			for key, set := range l.NonDoubles {
				if key == l.MapEntryKey(key) && l.EntryExists(key) {
					for nonDouble := range set.Elements() {
						if nonDouble != key && nonDouble == l.MapEntryKey(nonDouble) && l.EntryExists(nonDouble) {
							if !l.EvidenceForBeingDifferentEntries(key, nonDouble) {
								lines = append(lines, key+csvDelimiter+nonDouble)
							}
						}
					}
				}
			}
			sort.Strings(lines)
			for _, line := range lines {
				w.WriteString(line + "\n")
			}
		})
		saveKeyNonDoublesToDb(l)
	}
}

func (l *TBibTeXLibrary) WriteGenericFieldMappingsFile() {
	if !l.NoGenericFieldMappingsFileWriting {
		l.writeLibraryFile(GenericFieldMappingsFilePath, ProgressWritingGenericFieldMappingsFile, func(w *bufio.Writer) {
			var lines []string
			for field, challenges := range l.GenericFieldSourceToTarget {
				if field != PreferredAliasField {
					for challenger, winner := range challenges {
						if challenger != winner {
							lines = append(lines, field+csvDelimiter+l.MapFieldValue(field, winner)+csvDelimiter+challenger)
						}
					}
				}
			}
			sort.Strings(lines)
			for _, line := range lines {
				w.WriteString(line + "\n")
			}
		})
		saveGenericFieldMappingsToDb(l)
	}
}

func (l *TBibTeXLibrary) WriteNameMappingFile() {
	if !l.NoNameMappingsFileWriting {
		l.writeLibraryFile(NameMappingsFilePath, ProgressWritingNameMappingsFile, func(w *bufio.Writer) {
			var lines []string
			for alias, original := range l.NameAliasToName {
				if alias != original {
					lines = append(lines, original+csvDelimiter+alias)
				}
			}
			sort.Strings(lines)
			for _, line := range lines {
				w.WriteString(line + "\n")
			}
		})
		saveNameMappingsToDb(l)
	}
}

// Write entry key alias/original pairs to a bufio.bWriter buffer
func (l *TBibTeXLibrary) WriteKeyOldiesFile() {
	if !l.NoKeyOldiesFileWriting {
		l.writeLibraryFile(KeyOldiesFilePath, ProgressWritingKeyOldiesFile, func(w *bufio.Writer) {
			var lines []string
			for source, target := range l.KeyToKey {
				target = l.MapEntryKey(target)
				if bibEntryExists(target) && source != target && IsValidKey(source) {
					lines = append(lines, target+csvDelimiter+source)
				}
			}
			sort.Strings(lines)
			for _, line := range lines {
				w.WriteString(line + "\n")
			}
		})
		saveKeyOldiesToDb(l)
	}
}

// Write entry key source/target pairs to a bufio.bWriter buffer
func (l *TBibTeXLibrary) WriteKeyHintsFile() {
	if !l.NoKeyHintsFileWriting {
		l.writeLibraryFile(KeyHintsFilePath, ProgressWritingKeyHintsFile, func(w *bufio.Writer) {
			var lines []string
			for source, target := range l.HintToKey {
				target = l.MapEntryKey(target)
				if bibEntryExists(target) && source != target &&
					source != KeyForDBLP(l.EntryFieldValueity(target, DBLPField)) {
					lines = append(lines, target+csvDelimiter+source)
				}
			}
			sort.Strings(lines)
			for _, line := range lines {
				w.WriteString(line + "\n")
			}
		})
		saveKeyHintsToDb(l)
	}
}

func (l *TBibTeXLibrary) WriteAllMappingsFiles() {
	l.WriteKeyOldiesFile()
	l.WriteKeyHintsFile()
	l.WriteNameMappingFile()
	l.WriteGenericFieldMappingsFile()
	l.WriteEntryFieldMappingsFile()
}

func (l *TBibTeXLibrary) WriteCrossFieldMappingsFile() {
	if !l.NoCrossFieldMappingsFileWriting {
		l.writeLibraryFile(CrossFieldMappingsFilePath, ProgressWritingFieldMappingsFile, func(w *bufio.Writer) {
			var lines []string
			for sourceField, sourceFieldMappings := range l.FieldMappings {
				for sourceValue, targetFieldMappings := range sourceFieldMappings {
					for targetField, targetValue := range targetFieldMappings {
						lines = append(lines, sourceField+csvDelimiter+sourceValue+csvDelimiter+targetField+csvDelimiter+targetValue)
					}
				}
			}
			sort.Strings(lines)
			for _, line := range lines {
				w.WriteString(line + "\n")
			}
		})
		saveCrossFieldMappingsToDb(l)
	}
}
