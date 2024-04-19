package main

import (
	"bufio"
	"log"
	"os"
	"strings"
)

func (l *TBiBTeXLibrary) LegacyAliases() {
	file, err := os.Open(KeysMapFile)
	if err != nil {
		log.Fatal(err)
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		s := string(scanner.Text())
		l.AddKeyAlias(s[:strings.Index(s, " ")], s[strings.Index(s, " ")+1:])

	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	file.Close()

	file, err = os.Open(PreferredAliasesFile)
	if err != nil {
		log.Fatal(err)
	}

	scanner = bufio.NewScanner(file)
	for scanner.Scan() {
		l.AddPreferredAlias(scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	file.Close()
}
