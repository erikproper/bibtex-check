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

func (l *TBibTeXLibrary) writeFile(filePath, message string, writing func(*bufio.Writer)) bool {
	l.Progress(message, filePath)

	BackupFile(filePath)

	file, err := os.Create(filePath)
	if err != nil {
		return false
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	writing(writer)
	writer.Flush()

	return true
}

// Does what its name says ... writing the library to the provided BibTeX file
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

///// COMMENTS
func (l *TBibTeXLibrary) WriteBibTeXFile() bool {
	return l.writeFile(l.bibFilePath, "Writing bib file %s", l.writeBibTeXContent)
}

func (l *TBibTeXLibrary) WriteChallenges() {
	fmt.Println("Writing challenges map")

	// One block for both actually.
	BackupFile(l.challengesFilePath)
	chFile, err := os.Create(l.challengesFilePath)
	if err != nil {
		log.Fatal(err)
	}
	defer chFile.Close()

	chWriter := bufio.NewWriter(chFile)
	for key, fieldChallenges := range l.challengeWinners {
		_, keyIsUsed := l.entryType[key]
		if !keyIsUsed {
			fmt.Println("Not in use:", key)
		}
		chWriter.WriteString("K " + key + "\n")
		for field, challenges := range fieldChallenges {
			chWriter.WriteString("F " + field + "\n")
			for challenger, winner := range challenges {
				chWriter.WriteString("C " + challenger + "\n")
				chWriter.WriteString("W " + winner + "\n")
			}
		}
	}
	chWriter.Flush()
}

func (l *TBibTeXLibrary) WriteAliases() {
	fmt.Println("Writing aliases map")

	BackupFile(l.aliasesFilePath)

	kmFile, err := os.Create(l.aliasesFilePath)
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

	l.WriteChallenges()
}
