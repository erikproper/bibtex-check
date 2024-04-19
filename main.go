package main

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
)

// Make into WARNING
// AddKeyAlias ==> Warning + clean

// stringSet concatenation for the unknown ones when reporting

// config.go
// - Aliases ... in config, and ... use ...
// - IgnoreFields ... and ... ensure they are disjoint ...

// Comments list
//func main() {
//	var s []int
//	printSlice(s)
//	// append works on nil slices.
//	s = append(s, 0)
//	printSlice(s)
//	// The slice grows as needed.
//	s = append(s, 1)
//	printSlice(s)
//	// We can add more than one element at a time.
//	s = append(s, 2, 3, 4)
//	printSlice(s)
//}

/// Export library
/// Save library + comments(!!)

// Test AllowedXX on entries and fields
// - test file = for zotero inport ... even before overwriting ...
// - Make these tests (also on double entries) switchable.
// 82CVJ2UD for files ...
/// Per stream parse round:
///   for each UnknownField report ... optional, like warning on doubles.
/// Make these two setable using functions.

// Clean KEY/Types
// Do Keymapper first before legacy import
// Then cross check Key/Types again on legacy files
// Then balance key/types between current and legacy
// Then start on the rest matching legacy and new


/// Add comments + inspect ... also to the already splitted files.

/// Make things robust and reporting when file is not found

/// First App

/// KInd[field] = Which kind of cleaning/nornalisation needed
/// Should also go into config.go
///
/// Field specific normalisation/cleaning
/// - (Book)titles, series,
/// - addresses
/// - institutions
/// - Person names
/// - Pages

/// Reading person names:
/// - Read names file first {<name>} {<alias>}
/// -  name from bibtext
/// - Use normalised string representatation to lookup in a string to string map

/////

var Library TBiBTeXLibrary

const (
	PreferredAliasesFile = "/Users/erikproper/BiBTeX/preferred.aliases"
	KeysMapFile          = "/Users/erikproper/BiBTeX/keys.map"
	PreferredAliases     = "/Users/erikproper/BiBTeX/PreferredAliases"
)

func main() {
	Reporting := TReporting{}

	Library = TBiBTeXLibrary{}
	Library.Initialise(Reporting, true)
	Library.legacyMode = false
	Library.LegacyAliases()

	OldLibrary := TBiBTeXLibrary{}
	OldLibrary.Initialise(Reporting, false)
	OldLibrary.legacyMode = true
	OldLibrary.LegacyAliases()

	BiBTeXParser := TBiBTeXStream{}

	///// -update or -check as commandline
	fmt.Println("Reading new library")
	BiBTeXParser.Initialise(Reporting, Library)
	BiBTeXParser.ParseBiBFile("/Users/erikproper/BiBTeX/ErikProper.bib")
	fmt.Println("Size new:", len(Library.entryType))

	////// -check as commandline
	//	fmt.Println("Reading old libraries")
	//	BiBTeXParser.Initialise(Reporting, OldLibrary)
	//	BiBTeXParser.ParseBiBFile("/Users/erikproper/BiBTeX/Old/ErikProper.bib")
	//	BiBTeXParser.ParseBiBFile("/Users/erikproper/BiBTeX/Old/Old.bib")
	//	BiBTeXParser.ParseBiBFile("/Users/erikproper/BiBTeX/MyLibrary.bib")
	//	BiBTeXParser.ParseBiBFile("Convert.bib")
	//	fmt.Println("Size old:", len(OldLibrary.entryType))

	//	BiBTeXParser.ParseBiBFile("Test.bib")

	fmt.Println("Exporting preferred aliases")
	os.RemoveAll(PreferredAliases)
	for a := range Library.preferredAliases {
		os.MkdirAll(PreferredAliases+"/"+a, os.ModePerm)
		os.WriteFile(PreferredAliases+"/"+a+"/alias", []byte(string(Library.preferredAliases[a])), 0644)
	}

	fmt.Println("Exporting alias mapping")
	AliasKeys := "/Users/erikproper/BiBTeX/Keys"
	os.RemoveAll(AliasKeys)
	for a := range Library.deAlias {
		h := md5.New()
		io.WriteString(h, a+"\n")
		aa := fmt.Sprintf("%x", h.Sum(nil))
		os.MkdirAll(AliasKeys+"/"+aa, os.ModePerm)
		os.WriteFile(AliasKeys+"/"+aa+"/key", []byte(string(Library.deAlias[a])), 0644)
		os.WriteFile(AliasKeys+"/"+aa+"/alias", []byte(string(a)), 0644)
	}

	Test := "YnBsaXN0MDDSAQIDBFxyZWxhdGl2ZVBhdGhYYm9va21hcmtfECBGaWxlcy9FUC0yMDI0LTA0LTAzLTIyLTA3LTMxLnBkZk8RBERib29rRAQAAAAABBAwAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABAAwAABQAAAAEBAABVc2VycwAAAAoAAAABAQAAZXJpa3Byb3BlcgAACQAAAAEBAABOZXh0Y2xvdWQAAAAHAAAAAQEAAExpYnJhcnkABgAAAAEBAABCaUJUZVgAAAUAAAABAQAARmlsZXMAAAAaAAAAAQEAAEVQLTIwMjQtMDQtMDMtMjItMDctMzEucGRmAAAcAAAAAQYAAAQAAAAUAAAAKAAAADwAAABMAAAAXAAAAGwAAAAIAAAABAMAABVdAAAAAAAACAAAAAQDAADeCAQAAAAAAAgAAAAEAwAAuMRlBwAAAAAIAAAABAMAAAEDjwcAAAAACAAAAAQDAAApz5oHAAAAAAgAAAAEAwAAxmdJCgAAAAAIAAAABAMAAOeBbQkAAAAAHAAAAAEGAAC0AAAAxAAAANQAAADkAAAA9AAAAAQBAAAUAQAACAAAAAAEAABBxTC0xIAAABgAAAABAgAAAQAAAAAAAAAPAAAAAAAAAAAAAAAAAAAACAAAAAQDAAAFAAAAAAAAAAQAAAADAwAA9QEAAAgAAAABCQAAZmlsZTovLy8MAAAAAQEAAE1hY2ludG9zaCBIRAgAAAAEAwAAAFChG3MAAAAIAAAAAAQAAEHFlk7IgAAAJAAAAAEBAABBQUY2QTJFRi01MTg0LTQ1OEItQTM2RC04QzJDMTU5MDBENUMYAAAAAQIAAIEAAAABAAAA7xMAAAEAAAAAAAAAAAAAAAEAAAABAQAALwAAAAAAAAABBQAA/QAAAAECAAAzNjllNzI1YTcyMTkxYmRhYjZlYzMwMzMxZjUyYTQyMjM1OTQ5YTUzZDdlZmNlNmMzYzc0NjUzZGFjZWIyODNkOzAwOzAwMDAwMDAwOzAwMDAwMDAwOzAwMDAwMDAwOzAwMDAwMDAwMDAwMDAwMjA7Y29tLmFwcGxlLmFwcC1zYW5kYm94LnJlYWQtd3JpdGU7MDE7MDEwMDAwMTI7MDAwMDAwMDAwOTZkODFlNzswMTsvdXNlcnMvZXJpa3Byb3Blci9uZXh0Y2xvdWQvbGlicmFyeS9iaWJ0ZXgvZmlsZXMvZXAtMjAyNC0wNC0wMy0yMi0wNy0zMS5wZGYAAAAAzAAAAP7///8BAAAAAAAAABAAAAAEEAAAkAAAAAAAAAAFEAAAJAEAAAAAAAAQEAAAWAEAAAAAAABAEAAASAEAAAAAAAACIAAAJAIAAAAAAAAFIAAAlAEAAAAAAAAQIAAApAEAAAAAAAARIAAA2AEAAAAAAAASIAAAuAEAAAAAAAATIAAAyAEAAAAAAAAgIAAABAIAAAAAAAAwIAAAMAIAAAAAAAABwAAAeAEAAAAAAAARwAAAFAAAAAAAAAASwAAAiAEAAAAAAACA8AAAOAIAAAAAAAAACAANABoAIwBGAAAAAAAAAgEAAAAAAAAABQAAAAAAAAAAAAAAAAAABI4="
	data, _ := base64.StdEncoding.DecodeString(Test)
	Str := string(data)
	Start := Str[strings.Index(Str, "relativePathXbookmark")+len("relativePathXbookmark")+3 : strings.Index(Str, "DbookD")-3]
	fmt.Printf("%q\n", Start)

	// log import ...
	//
	//	log.Fatal(err)
}
