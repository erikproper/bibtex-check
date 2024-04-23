package main

import (
	//	"encoding/base64"
	"fmt"
)

/// Add comments + inspect + cleaning ...
/// clean IO in this file as well. Add a "Progress" channel.

// Checks on ErikProper.bib:
// - Enable level of checks (legacy/import versus ErikProper.bib)
//   Make these tests (also on double entries) switchable via explicit functions.
//   So, not a list of true/false when initializing.
/////// CHECK legacy mode
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

/// check.files BIBDSK bi-directional!
/// ISBN/ISSN should not contain spaces

/// Kind[field] = Which kind of cleaning/nornalisation needed
/// Should also go into config.go

/// Check consistency of fields and their use.
/// When assigning a func to a field, this field must be allowed

/// For aliases to new keys ... check if the entry is there ...

// Clean KEY/Types
// Do Keymapper first before legacy import
// Then cross check Key/Types again on legacy files
// Then balance key/types between current and legacy
// Then start on the rest matching legacy and new

/// organization field ... "abused" as publisher in proceedings??

/// First App
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

/// Merging legacy:
/// 1: Cluster based on mapped IDs
///   - Ensure it is possible to do this comparison/update to ErikProper.bib's library as well as a future migration library.
///   - Work with a per entry LibX.EntryA to LibY.EntryB mapper where one is the challenger, and the other is the master so-far.
///   - Check missing information towards ErikProper.bib like presently
///   - Do so, in a file-per-file way.
///   - So, only include the presently included fields.
///   - Create an OK file with "<id> <field> <md5_1> <md5_2>"
///     - The first md5 is the present one from the champion.
///     - The second md5 is the rejected one from the challenger
///   ==> Now stop using the check.metadata script
/// 2: Cluster based on mapped IDs
///   - Check missing information towards ErikProper.bib with more fields
///   - If entry is prefix of the other, take the longer one
///   - Compare normalised name fields
///   - Compare normalised title fields
///   - Other strategies?
/// 3: Cluster the entries from the legacy sources based keys
///   - Read/write a migration.map file
///   - For each key with mapping to ErikProper.bib:
///     - Create a temporary ID
///     - Add key mapping (also export to migration.map)
///     - "Sync" to the Migrate Library in the same way as for ErikProper.bib
///     - In doing so, add to the OK file as well.
/// 4: Cluster the clustered entries based on additional heuristics
///     - Titles, authors, etc. Requires user decision.
///     - Requires the ability to change the alias to IDs mapping for the "winners"
///     - Define the champion, and challenge this one from the old clustered entry.
/// 5: Once stable, add the final new IDs to ErikProper.bib and extend the keys.map file accordingly
///     - NOTE: Also rename the targets in keys.map that are now actually aliases!!
/// 6: Re-use these techniques to look for doubles in ErikProper.bib

/// BEFORE introducing Aliases and the first one as preferred alias ....
/// Start using sequences for entries and the fields in general.
/// SequenceOfXX = struct{ members map[XX]bool, Element []XX }
/// s.Add(elements... XX)
/// s.AddAt(i int, elements... XX)
/// s.InsertAt(i int, t SeqOfXX)
/// s.Concat
/// s.Contains(elements... XX)
/// s.ContainsSet(t SetOfXX)
/// s.ContainsSequence(t SeqOfXX)
/// s.Sequence() = range of index, elements
/// s.Elements() = range of elements
/// https://pkg.go.dev/slices
/// https://cs.opensource.google/go/go/+/refs/tags/go1.22.2:src/slices/slices.go

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
	Reporting := TInteraction{}

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
	Library.CheckDBLPAliases()

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
