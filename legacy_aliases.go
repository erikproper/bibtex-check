package main

import (
	"bufio"
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

func (l *TBiBTeXLibrary) CheckPreferredAliases() {
	for key, alias := range l.preferredAliases {
		if !CheckPreferredAlias(alias) {
			l.Warning(WarningBadAlias, alias, key)
		}
	}

	for alias, key := range l.deAlias {
		if l.preferredAliases[key] == "" {
			if CheckPreferredAlias(alias) {
				l.AddPreferredAlias(alias)
			} else {
				loweredAlias := strings.ToLower(alias)
				if l.deAlias[loweredAlias] == "" && loweredAlias != key && CheckPreferredAlias(loweredAlias) {
					l.AddKeyAlias(loweredAlias, key)
					l.AddPreferredAlias(loweredAlias)
				}
			}
		}
	}
}

func (l *TBiBTeXLibrary) CheckDBLPAliases() {
	for alias, key := range l.deAlias {
		if strings.Index(alias, "DBLP:") == 0 {
			dblp := alias[5:]
			if l.entryFields[key] != nil && dblp != l.entryFields[key]["dblp"] {
				if l.entryFields[key]["dblp"] != "" {
					fmt.Println("Found:", dblp, "from", alias, "while", l.entryFields[key]["dblp"])
				} else {
					l.entryFields[key]["dblp"] = dblp
				}
			}
		}
	}
}

func (l *TBiBTeXLibrary) ReadLegacyAliases() {
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

func (l *TBiBTeXLibrary) WriteLegacyAliases() {
	fmt.Println("Writing preferred aliases")

	BackupFile(PreferredAliasesFile)
	os.RemoveAll(PreferredAliases)

	paFile, err := os.Create(PreferredAliasesFile)
	if err != nil {
		log.Fatal(err)
	}
	defer paFile.Close()

	paWriter := bufio.NewWriter(paFile)
	for key, alias := range l.preferredAliases {
		os.MkdirAll(PreferredAliases+"/"+key, os.ModePerm)
		os.WriteFile(PreferredAliases+"/"+key+"/alias", []byte(alias), 0644)

		paWriter.WriteString(alias + "\n")
	}
	paWriter.Flush()

	fmt.Println("Writing aliases map")

	BackupFile(KeysMapFile)
	os.RemoveAll(AliasKeys)

	kmFile, err := os.Create(KeysMapFile)
	if err != nil {
		log.Fatal(err)
	}
	defer kmFile.Close()

	kmWriter := bufio.NewWriter(kmFile)
	for alias, key := range Library.deAlias {
		hash := md5.New()
		io.WriteString(hash, alias+"\n")
		aa := fmt.Sprintf("%x", hash.Sum(nil))
		os.MkdirAll(AliasKeys+"/"+aa, os.ModePerm)
		os.WriteFile(AliasKeys+"/"+aa+"/key", []byte(key), 0644)
		os.WriteFile(AliasKeys+"/"+aa+"/alias", []byte(alias), 0644)

		kmWriter.WriteString(alias + " " + key + "\n")
	}
	kmWriter.Flush()

	// Identify mapping for the keys. Makes the scripts simpler for now.
	for key := range Library.entryType {
		hash := md5.New()
		io.WriteString(hash, key+"\n")
		aa := fmt.Sprintf("%x", hash.Sum(nil))
		os.MkdirAll(AliasKeys+"/"+aa, os.ModePerm)
		os.WriteFile(AliasKeys+"/"+aa+"/key", []byte(key), 0644)
		os.WriteFile(AliasKeys+"/"+aa+"/alias", []byte(key), 0644)
	}
}
