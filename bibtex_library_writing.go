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

// Generic function to write library related files
func (l *TBibTeXLibrary) writeFile(filePath, message string, writing func(*bufio.Writer)) bool {
	FullFilePath := l.FilesRoot + filePath

	l.Progress(message, FullFilePath)

	BackupFile(FullFilePath)

	file, err := os.Create(FullFilePath)
	if err != nil {
		return false
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	writing(writer)
	writer.Flush()

	return true
}

// Function to write the BibTeX content of the library to a bufio.bWriter buffer
// Notes:
// - As we ignore preambles, these are not written.
// - When we start managing the groups (of keys) the way Bibdesk does, we need to ensure that their embedded as an XML structure embedded in a comment, is updated.
func (l *TBibTeXLibrary) writeBibTeXContent(bibWriter *bufio.Writer) {
	// Write out the entries and their fields
	for entry := range l.EntryTypes {
		bibWriter.WriteString(l.EntryString(entry))
		bibWriter.WriteString("\n")
	}

	// Write out the comments
	for _, comment := range l.Comments {
		bibWriter.WriteString("@" + CommentEntryType + "{" + comment + "}\n")
		bibWriter.WriteString("\n")
	}
}

// Write the content of BibTeX content of the library to a file
func (l *TBibTeXLibrary) WriteBibTeXFile() bool {
	return l.writeFile(l.BibFilePath, ProgressWritingBibFile, l.writeBibTeXContent)
}

// Write the challenges and winners for field values, of this library, to a bufio.bWriter buffer
func (l *TBibTeXLibrary) writeChallenges(challengeWriter *bufio.Writer) {
	for key, fieldChallenges := range l.ChallengeWinners {
		if l.EntryExists(key) {
			challengeWriter.WriteString("K " + key + "\n")
			for field, challenges := range fieldChallenges {
				challengeWriter.WriteString("F " + field + "\n")
				for challenger, winner := range challenges {
					challengeWriter.WriteString("C " + challenger + "\n")
					challengeWriter.WriteString("W " + winner + "\n")
				}
			}
		}
	}
}

// Write the challenges and winners for field values, of this library, to a file
func (l *TBibTeXLibrary) WriteChallenges() bool {
	return l.writeFile(l.ChallengesFilePath, ProgressWritingChallengesFile, l.writeChallenges)
}

// Write the aliases from this library, to a bufio.bWriter buffer
func (l *TBibTeXLibrary) writeAliases(aliasWriter *bufio.Writer) {
	// First write the preferred aliases, so they are read first when reading them in again
	for key, alias := range Library.PreferredAliases {
		aliasWriter.WriteString(alias + " " + key + "\n")
	}

	// Then write the other aliases
	for alias, key := range Library.AliasToEntry {
		if alias != Library.PreferredAliases[key] {
			aliasWriter.WriteString(alias + " " + key + "\n")
		}
	}
}

// Write the aliases from this library, to a file
func (l *TBibTeXLibrary) WriteAliases() bool {
	return l.writeFile(l.AliasesFilePath, ProgressWritingAliasesFile, l.writeAliases)
}
