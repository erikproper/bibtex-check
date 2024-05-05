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
	"strings"
)

func (l *TBibTeXLibrary) ReadBib(filePath string) bool {
	l.BibFilePath = filePath

	return l.ParseBibFile(l.FilesRoot + l.BibFilePath)
}

// Quick and dirty reading of the keys.map and preferred.aliases file.
func (l *TBibTeXLibrary) ReadAliases(filePath string) {
	l.AliasesFilePath = filePath

	file, err := os.Open(l.FilesRoot + l.AliasesFilePath)
	if err != nil {
		log.Fatal(err) /// Don't want to do it like this.
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		s := strings.Split(string(scanner.Text()), " ")

		if len(s) != 2 {
			fmt.Println("Line does not have precisely two entries:", s)
			log.Fatal(err)
			return
		}

		alias := s[0]
		key := s[1]

		l.AddKeyAlias(alias, key, false)

		if !l.PreferredAliasExists(key) && CheckPreferredAliasValidity(alias) {
			l.AddPreferredAlias(alias)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	file.Close()
}

func (l *TBibTeXLibrary) ReadChallenges(filePath string) {
	l.ChallengesFilePath = filePath

	file, err := os.Open(l.FilesRoot + l.ChallengesFilePath)
	if err != nil {
		log.Fatal(err) /// Don't want to do it like this.
	}

	key := ""
	field := ""
	challenge := ""

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		s := string(scanner.Text())

		if len(s) < 3 {
			fmt.Println("Line is too short.", s)
			log.Fatal(err)
			return
		}

		switch s[0] {
		case 'K':
			key = s[2:]

		case 'F':
			field = s[2:]

		case 'C':
			challenge = s[2:]

		case 'W':
			l.registerChallengeWinner(key, field, challenge, s[2:])
			//			l.registerChallengeWinner(key, field, l.ProcessFieldValue(field, challenge), l.ProcessFieldValue(field, s[2:]))
		}

	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	file.Close()
}
