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
	l.BibFilePath = filePath

	return l.ParseBibFile(l.FilesRoot + l.BibFilePath)
}

// Generic function to read library related files
func (l *TBibTeXLibrary) readFile(filePath, message string, reading func(string)) bool {
	FullFilePath := l.FilesRoot + filePath

	l.Progress(message, FullFilePath)

	file, err := os.Open(FullFilePath)
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

// Read preferred key aliases file
func (l *TBibTeXLibrary) ReadPreferredKeyAliases(filePath string) {
	l.PreferredKeyAliasesFilePath = filePath

	l.readFile(filePath, ProgressReadingPreferredKeyAliasesFile, l.AddPreferredKeyAlias)
}

// General function to read aliases files
// These files contain two strings per line, separated by a tab, where the second string is an alias for the latter.
func (l *TBibTeXLibrary) readAliasesMapping(filePath, progress string, addMapping func(alias, target string), checkAliasesMapping func()) {
	l.readFile(filePath, progress, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 2 {
			l.Warning(WarningAliasesLineTooShort, line)
			return
		}

		addMapping(elements[1], elements[0])
	})

	checkAliasesMapping()
}

// Read key aliases file
func (l *TBibTeXLibrary) ReadKeyAliases(filePath string) {
	l.KeyAliasesFilePath = filePath

	l.readAliasesMapping(filePath, ProgressReadingKeyAliasesFile, l.AddKeyAlias, l.CheckKeyAliasesMapping)
}

// Read name aliases files
func (l *TBibTeXLibrary) ReadNameAliases(filePath string) {
	l.NameAliasesFilePath = filePath

	l.readAliasesMapping(filePath, ProgressReadingNameAliasesFile, l.AddAliasForName, l.CheckNameAliasesMapping)
}

// Read journal aliases files
func (l *TBibTeXLibrary) ReadJournalAliases(filePath string) {
	l.JournalAliasesFilePath = filePath

	l.readAliasesMapping(filePath, ProgressReadingJournalAliasesFile, l.AddAliasForJournal, l.CheckJournalAliasesMapping)
}

// Read challenge files
func (l *TBibTeXLibrary) ReadChallenges(filePath string) {
	l.ChallengesFilePath = filePath

	l.readFile(l.ChallengesFilePath, ProgressReadingChallengesFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 4 {
			l.Warning(WarningChallengeLineTooShort, line)
			return
		}

		key := elements[0]
		field := elements[1]
		winner := l.NormaliseFieldValue(field, elements[3])

		// We do normalise the challengers, but want to ignore error messages.
		// Challenged values may actually have errors ...
		silenced := l.InteractionIsOff()
		l.SetInteractionOff()
		/**/ challenger := l.NormaliseFieldValue(field, elements[2])
		l.SetInteraction(silenced)

		l.AddChallengeWinner(key, field, challenger, winner)
	})
}
