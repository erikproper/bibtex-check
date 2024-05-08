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

// Read key alias files
// These files contain two keys per line, where the former is an alias to the latter key
func (l *TBibTeXLibrary) ReadKeyAliases(filePath string) {
	l.KeyAliasesFilePath = filePath

	l.readFile(l.KeyAliasesFilePath, ProgressReadingKeyAliasesFile, func(line string) {
		strings := strings.Split(line, " ")

		if len(strings) != 2 {
			l.Warning(WarningKeyAliasesLineBadEntries, line)
			return
		}

		alias := strings[0]
		key := strings[1]

		l.AddKeyAlias(alias, key, false)

		if !l.PreferredKeyAliasExists(key) && CheckPreferredKeyAliasValidity(alias) {
			l.AddPreferredKeyAlias(alias)
		}
	})
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
		challenger := l.NormaliseFieldValue(field, elements[2])
		winner := l.NormaliseFieldValue(field, elements[3])

		l.RegisterChallengeWinner(key, field, challenger, winner)
	})
}

// Read names aliases files
func (l *TBibTeXLibrary) ReadNameAliases(filePath string) {
	l.NameAliasesFilePath = filePath

	l.readFile(l.NameAliasesFilePath, ProgressReadingNameAliasesFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 2 {
			l.Warning(WarningNameAliasesLineTooShort, line)
			return
		}

		l.RegisterAliasForName(elements[1], elements[0])
	})
}
