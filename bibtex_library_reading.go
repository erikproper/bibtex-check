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

// Quick and dirty reading of the keys.map and preferred.aliases file.
func (l *TBibTeXLibrary) ReadAliases(filePath string) {
	l.AliasesFilePath = filePath

	l.readFile(l.AliasesFilePath, ProgressReadingAliasesFile, func(line string) {
		strings := strings.Split(line, " ")

		if len(strings) != 2 {
			l.Warning(WarningAliasLineBadEntries, line)
			return
		}

		alias := strings[0]
		key := strings[1]

		l.AddKeyAlias(alias, key, false)

		if !l.PreferredAliasExists(key) && CheckPreferredAliasValidity(alias) {
			l.AddPreferredAlias(alias)
		}
	})
}

func (l *TBibTeXLibrary) ReadChallenges(filePath string) {
	l.ChallengesFilePath = filePath

	key := ""
	field := ""
	challenge := ""

	l.readFile(l.ChallengesFilePath, ProgressReadingChallengesFile, func(line string) {
		if len(line) < 3 {
			l.Warning(WarningChallengeLineTooShort, line)
			return
		}

		switch line[0] {
		case 'K':
			key = line[2:]

		case 'F':
			field = line[2:]

		case 'C':
			challenge = line[2:]

		case 'W':
			l.RegisterChallengeWinner(key, field, l.NormaliseFieldValue(field, challenge), l.NormaliseFieldValue(field, line[2:]))
		}
	})
}
