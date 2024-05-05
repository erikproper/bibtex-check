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
	"fmt"
	"log"
	"os"
)

// Generic function to write files
func (l *TBibTeXLibrary) writeFile(filePath, message string, writing func(*bufio.Writer)) bool {
	ActualFilePath := l.FilesRoot + filePath

	l.Progress(message, ActualFilePath)

	BackupFile(ActualFilePath)

	file, err := os.Create(ActualFilePath)
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
	l.ForEachEntry(func(entry string) {
		bibWriter.WriteString(l.EntryString(entry))
		bibWriter.WriteString("\n")
	})

	// Write out the comments
	writeComment := func(comment string) {
		bibWriter.WriteString("@" + CommentEntryType + "{" + comment + "}\n")
		bibWriter.WriteString("\n")
	}
	for _, comment := range l.comments {
		writeComment(comment)
	}
}

// Write the content of BibTeX content of the library to a file
func (l *TBibTeXLibrary) WriteBibTeXFile() bool {
	return l.writeFile(l.BibFilePath, ProgressWritingBibFile, l.writeBibTeXContent)
}

//// HERE!

// Function to write the BibTeX content of the library to a bufio.bWriter buffer
func (l *TBibTeXLibrary) writeChallenges(chWriter *bufio.Writer) {
	for key, fieldChallenges := range l.challengeWinners {
		if l.EntryExists(key) {
			chWriter.WriteString("K " + key + "\n")
			for field, challenges := range fieldChallenges {
				chWriter.WriteString("F " + field + "\n")
				for challenger, winner := range challenges {
					chWriter.WriteString("C " + challenger + "\n")
					chWriter.WriteString("W " + winner + "\n")
				}
			}
		}
	}
}

// Write the content of BibTeX content of the library to a file
func (l *TBibTeXLibrary) WriteChallenges() bool {
	return l.writeFile(l.ChallengesFilePath, ProgressWritingChallengesFile, l.writeChallenges)
}

func (l *TBibTeXLibrary) WriteAliases() {
	fmt.Println("Writing aliases map")

	BackupFile(l.FilesRoot + l.AliasesFilePath)

	kmFile, err := os.Create(l.FilesRoot + l.AliasesFilePath)
	if err != nil {
		log.Fatal(err)
	}
	defer kmFile.Close()

	kmWriter := bufio.NewWriter(kmFile)
	for key, alias := range Library.preferredAliases {
		kmWriter.WriteString(alias + " " + key + "\n")
	}
	for alias, key := range Library.deAlias {
		if alias != Library.preferredAliases[key] {
			kmWriter.WriteString(alias + " " + key + "\n")
		}
	}
	kmWriter.Flush()
}
