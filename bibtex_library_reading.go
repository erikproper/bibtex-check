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

		if !l.PreferredAliasExists(key) && CheckPreferredAliasValidity(alias) {
			l.AddPreferredAlias(alias)
		}
	})
}

// Read challenge files
// A sequence of entries:
//
//	K <key>
//	F <field>
//	C <challenge_1>
//	C ...
//	C <challenge_n>
//	W <winner>
func (l *TBibTeXLibrary) ReadChallenges(filePath string) {
	l.ChallengesFilePath = filePath

	key := ""
	field := ""
	challenger := ""
	l.readFile(l.ChallengesFilePath, ProgressReadingChallengesFile, func(line string) {
		if len(line) < 3 {
			l.Warning(WarningChallengeLineTooShort, line)
			return
		}

		switch string(line[0]) {
		case ChallengeKey:
			key = line[2:]

		case ChallengeField:
			field = line[2:]

		case ChallengeChallenger:
			challenger = line[2:]

		case ChallengeWinner:
			l.MaybeRegisterChallengeWinner(key, field, l.NormaliseFieldValue(field, challenger), l.NormaliseFieldValue(field, line[2:]))
		}
	})
}

// Read challengenames files
// A sequence of entries:
//
//	N <name>
//	A <alias_1>
//	A ...
//	A <alias_n>
func (l *TBibTeXLibrary) ReadNameAliases(filePath string) {
	l.NameAliasesFilePath = filePath

	name := ""
	l.readFile(l.NameAliasesFilePath, ProgressReadingNameAliasesFile, func(line string) {
		if len(line) < 3 {
			l.Warning(WarningNameAliasesLineTooShort, line)
			return
		}

		switch string(line[0]) {
		case NameOriginal:
			name = line[2:]

		case NameAlias:
			l.RegisterNameAlias(name, line[2:])
		}
	})
}
