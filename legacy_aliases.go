//
// Module: legacy_aliases
//
// This module is only intended to deal with the legacy situation.
// Due to my Zotero experiment, I now have:
// - Files in a Zotero folder
// - Older BIB files (referring to PDFs in the latter Zotero folder) with multiple occurrences
//
// Creator: Henderik A. Proper (erikproper@fastmail.com)
//
// Version of: 23.04.2024
//

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

// Check the compliance of preferred aliases.
// For the moment, the preferred aliases are stored in a separate file.
// Later, these will simply be the first entry in a "aliases" field in the BIB file.
// Once we've reached that point, we can integrate this check into the regular checks per field.
func (l *TBibTeXLibrary) CheckPreferredAliases() {
	// This actually still makes sense after we wrapup the legacy stuff.
	// Well .. for imports ... so ... add it to the import functionality
	// Only the lowercase part ...
	for alias, key := range l.deAlias {
		if l.preferredAliases[key] == "" {
			if l.IsValidPreferredAlias(alias) {
				l.AddPreferredAlias(alias)
			} else {
				loweredAlias := strings.ToLower(alias)
				if l.deAlias[loweredAlias] == "" && loweredAlias != key && l.IsValidPreferredAlias(loweredAlias) {
					l.AddKeyAlias(loweredAlias, key, false)
					l.AddPreferredAlias(loweredAlias)
					//				} else {
					//					fmt.Println("No preferred alias for:", key)
				}
			}
		}
	}
}

// Each "DBLP:" pre-fixed alias should be consistent with the dblp field of the referenced entry.
// These dblp fields are important for the future functionality of syncing with the dblp.org database.
func (l *TBibTeXLibrary) CheckDBLPAliases() {
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

// Quick and dirty reading of the keys.map and preferred.aliases file.
func (l *TBibTeXLibrary) ReadLegacyAliases() {
	file, err := os.Open(KeysMapFile)
	if err != nil {
		log.Fatal(err) /// Don't want to do it like this.
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		s := string(scanner.Text())
		// Use split!!
		alias := s[:strings.Index(s, " ")]
		key := s[strings.Index(s, " ")+1:]

		l.AddKeyAlias(alias, key, false)

		// l.HasPreferredAlias
		if l.preferredAliases[key] == "" && l.IsValidPreferredAlias(alias) {
			l.AddPreferredAlias(alias)
		} else {
			// fmt.Println("No pref alias:", key)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	file.Close()

	// file, err = os.Open(PreferredAliasesFile)
	//
	//	if err != nil {
	//		log.Fatal(err)
	//	}
	//
	// scanner = bufio.NewScanner(file)
	//
	//	for scanner.Scan() {
	//		l.AddPreferredAlias(scanner.Text())
	//	}
	//
	//	if err := scanner.Err(); err != nil {
	//		log.Fatal(err)
	//	}
	//
	// file.Close()
}

// Quick and dirty write-out of:
// - the ErikProper.aliases files
// - the creation of the "mapping" folders to enable the old scripts to still do their work
func (l *TBibTeXLibrary) WriteLegacyAliases() {
	fmt.Println("Writing aliases map")

	BackupFile(KeysMapFile)
	os.RemoveAll(AliasKeys)

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
		hash := md5.New()
		io.WriteString(hash, alias+"\n")
		aa := fmt.Sprintf("%x", hash.Sum(nil))
		os.MkdirAll(AliasKeys+"/"+aa, os.ModePerm)
		os.WriteFile(AliasKeys+"/"+aa+"/key", []byte(key), 0644)
		os.WriteFile(AliasKeys+"/"+aa+"/alias", []byte(alias), 0644)

		if alias != Library.preferredAliases[key] {
			kmWriter.WriteString(alias + " " + key + "\n")
		}
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
