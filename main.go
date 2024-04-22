package main

import (
	//	"encoding/base64"
	"fmt"
)

/// %!(EXTRA string=lewis2005smart)WARNING: Alias  has a target $s, which is actually an alias for $s.

/// Library.CheckPreferredAliases()
/// - DBLP completeness (legacy!)
/// - Cleaning ...

/// Make things robust and reporting when file is not found

/// Add comments + inspect ... also to the already splitted files.

// Checks on ErikProper.bib:
// - Enable level of checks (legacy versus ErikProper.bib)
//   Make these tests (also on double entries) switchable via explicit functions.
//   So, not a list of true/false when initializing.
// - Test AllowedXX on entries and fields
// - BIBDESK files
// - Crossrefs
// - Redundancy of URLs vs DOIs
// - Auto download for ceur PDFs?

/// map[string]func()
/// (l Lib) func ProcessXXX { works on XXX field of current entry }
/// Check file for bibdsk
/// FILE 82CVJ2UD for files ...
/// when BiBDesk opened libary has a local-url, then warn.
/// when BiBDesk opened libary has a file, then warn, and try to fix.

/// Kind[field] = Which kind of cleaning/nornalisation needed
/// Should also go into config.go

/// Check consistency of fields and their use.
/// When assigning a func to a field, this field must be allowed

/// First App

// Clean KEY/Types
// Do Keymapper first before legacy import
// Then cross check Key/Types again on legacy files
// Then balance key/types between current and legacy
// Then start on the rest matching legacy and new

/// Stringset via set package

/// SequenceOfXX = struct{ members map[XX]bool, Element []XX }
/// s.Add(elements... XX)
/// s.AddAt(i int, elements... XX)
/// s.InsertAt(i int, t SeqOfXX)
/// s.Concat
/// s.Contains(elements... XX)
/// s.ContainsSet(t SetOfXX)
/// s.ContainsSequence(t SeqOfXX)
/// https://pkg.go.dev/slices
/// https://cs.opensource.google/go/go/+/refs/tags/go1.22.2:src/slices/slices.go

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
	BiBTeXFolder         = "/Users/erikproper/BiBTeX/"
	PreferredAliasesFile = BiBTeXFolder + "preferred.aliases"
	KeysMapFile          = BiBTeXFolder + "keys.map"
	PreferredAliases     = BiBTeXFolder + "PreferredAliases"
	AliasKeys            = BiBTeXFolder + "Keys"
	ErikProperBib        = BiBTeXFolder + "ErikProper.bib"
	NewBib               = BiBTeXFolder + "New.bib"
)

func main() {
	Reporting := TReporting{}

	Library = TBiBTeXLibrary{}
	Library.Initialise(Reporting, true)
	Library.legacyMode = false
	Library.ReadLegacyAliases()

	OldLibrary := TBiBTeXLibrary{}
	OldLibrary.Initialise(Reporting, false)
	OldLibrary.legacyMode = true
	OldLibrary.ReadLegacyAliases()

	fmt.Println(Library.NewKey())

	fmt.Println("Reading main library")
	BiBTeXParser := TBiBTeXStream{}
	BiBTeXParser.Initialise(Reporting, &Library)
	BiBTeXParser.ParseBiBFile(ErikProperBib)
	fmt.Println("Size of", ErikProperBib, "is:", len(Library.entryType))

	Library.CheckPreferredAliases()

	//	fmt.Println("Reading old libraries")
	//	BiBTeXParser.Initialise(Reporting, OldLibrary)
	//	BiBTeXParser.ParseBiBFile("/Users/erikproper/BiBTeX/Old/ErikProper.bib")
	//	BiBTeXParser.ParseBiBFile("/Users/erikproper/BiBTeX/Old/Old.bib")
	//	BiBTeXParser.ParseBiBFile("/Users/erikproper/BiBTeX/MyLibrary.bib")
	//	BiBTeXParser.ParseBiBFile("Convert.bib")
	//	fmt.Println("Size old:", len(OldLibrary.entryType))

	//	Test := "YnBsaXN0MDDSAQIDBFxyZWxhdGl2ZVBhdGhYYm9va21hcmtfECBGaWxlcy9FUC0yMDI0LTA0LTAzLTIyLTA3LTMxLnBkZk8RBERib29rRAQAAAAABBAwAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABAAwAABQAAAAEBAABVc2VycwAAAAoAAAABAQAAZXJpa3Byb3BlcgAACQAAAAEBAABOZXh0Y2xvdWQAAAAHAAAAAQEAAExpYnJhcnkABgAAAAEBAABCaUJUZVgAAAUAAAABAQAARmlsZXMAAAAaAAAAAQEAAEVQLTIwMjQtMDQtMDMtMjItMDctMzEucGRmAAAcAAAAAQYAAAQAAAAUAAAAKAAAADwAAABMAAAAXAAAAGwAAAAIAAAABAMAABVdAAAAAAAACAAAAAQDAADeCAQAAAAAAAgAAAAEAwAAuMRlBwAAAAAIAAAABAMAAAEDjwcAAAAACAAAAAQDAAApz5oHAAAAAAgAAAAEAwAAxmdJCgAAAAAIAAAABAMAAOeBbQkAAAAAHAAAAAEGAAC0AAAAxAAAANQAAADkAAAA9AAAAAQBAAAUAQAACAAAAAAEAABBxTC0xIAAABgAAAABAgAAAQAAAAAAAAAPAAAAAAAAAAAAAAAAAAAACAAAAAQDAAAFAAAAAAAAAAQAAAADAwAA9QEAAAgAAAABCQAAZmlsZTovLy8MAAAAAQEAAE1hY2ludG9zaCBIRAgAAAAEAwAAAFChG3MAAAAIAAAAAAQAAEHFlk7IgAAAJAAAAAEBAABBQUY2QTJFRi01MTg0LTQ1OEItQTM2RC04QzJDMTU5MDBENUMYAAAAAQIAAIEAAAABAAAA7xMAAAEAAAAAAAAAAAAAAAEAAAABAQAALwAAAAAAAAABBQAA/QAAAAECAAAzNjllNzI1YTcyMTkxYmRhYjZlYzMwMzMxZjUyYTQyMjM1OTQ5YTUzZDdlZmNlNmMzYzc0NjUzZGFjZWIyODNkOzAwOzAwMDAwMDAwOzAwMDAwMDAwOzAwMDAwMDAwOzAwMDAwMDAwMDAwMDAwMjA7Y29tLmFwcGxlLmFwcC1zYW5kYm94LnJlYWQtd3JpdGU7MDE7MDEwMDAwMTI7MDAwMDAwMDAwOTZkODFlNzswMTsvdXNlcnMvZXJpa3Byb3Blci9uZXh0Y2xvdWQvbGlicmFyeS9iaWJ0ZXgvZmlsZXMvZXAtMjAyNC0wNC0wMy0yMi0wNy0zMS5wZGYAAAAAzAAAAP7///8BAAAAAAAAABAAAAAEEAAAkAAAAAAAAAAFEAAAJAEAAAAAAAAQEAAAWAEAAAAAAABAEAAASAEAAAAAAAACIAAAJAIAAAAAAAAFIAAAlAEAAAAAAAAQIAAApAEAAAAAAAARIAAA2AEAAAAAAAASIAAAuAEAAAAAAAATIAAAyAEAAAAAAAAgIAAABAIAAAAAAAAwIAAAMAIAAAAAAAABwAAAeAEAAAAAAAARwAAAFAAAAAAAAAASwAAAiAEAAAAAAACA8AAAOAIAAAAAAAAACAANABoAIwBGAAAAAAAAAgEAAAAAAAAABQAAAAAAAAAAAAAAAAAABI4="
	//	data, _ := base64.StdEncoding.DecodeString(Test)
	//	Str := string(data)
	//	Start := Str[strings.Index(Str, "relativePathXbookmark")+len("relativePathXbookmark")+3 : strings.Index(Str, "DbookD")-3]
	//	fmt.Printf("%q\n", Start)

	fmt.Println("Exporting updated library", ErikProperBib)
	Library.WriteBiBTeXFile(ErikProperBib)
	Library.WriteLegacyAliases()

	fmt.Println(Library.NewKey())

	// log import ...
	//
	//	log.Fatal(err)
}
