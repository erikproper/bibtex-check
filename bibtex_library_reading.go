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

// Quick and dirty reading of the keys.map and preferred.aliases file.
func (l *TBibTeXLibrary) ReadAliases() {
	file, err := os.Open(KeysMapFile)
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

	file, err = os.Open(ChallengesFile)
	if err != nil {
		log.Fatal(err) /// Don't want to do it like this.
	}

	key := ""
	field := ""
	challenge := ""

	scanner = bufio.NewScanner(file)
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
		}

	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	file.Close()
}

// Quick and dirty write-out of:
// - the ErikProper.aliases files
// - the creation of the "mapping" folders to enable the old scripts to still do their work
func (l *TBibTeXLibrary) WriteChallenges() {
	fmt.Println("Writing challenges map")

	BackupFile(ChallengesFile)

	chFile, err := os.Create(ChallengesFile)
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

	BackupFile(KeysMapFile)

	kmFile, err := os.Create(KeysMapFile)
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
