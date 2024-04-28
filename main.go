package main

import (
	"strings"
	"fmt"
	"regexp"
)

// Remove this one as soon as we have migrated the legacy files
const AllowLegacy = true

const Play = true

var Library TBibTeXLibrary

const (
	BibTeXFolder         = "/Users/erikproper/BibTeX/"
	PreferredAliasesFile = BibTeXFolder + "preferred.aliases"
	KeysMapFile          = BibTeXFolder + "keys.map"
	PreferredAliases     = BibTeXFolder + "PreferredAliases"
	AliasKeys            = BibTeXFolder + "Keys"
	ErikProperBib        = BibTeXFolder + "ErikProper.bib"
	NewBib               = BibTeXFolder + "New.bib"
)

func Titles(title string) {
	nesting := 0
	normalised := map[int]string{}
	inSpaces := true
	needsProtection := false

	fmt.Println("---")

	normalised[nesting] = ""
	for _, character := range title {
		if character == '{' {
			nesting++
			normalised[nesting] = ""
		} else if character == '}' {
			normalised[nesting-1] += normalised[nesting]
			nesting--
		} else if character == ' ' && inSpaces {
			// Skip
		} else if character == ' ' && !inSpaces {
			if needsProtection {
				normalised[nesting-1] += "[" + normalised[nesting] + "]"
			} else {
				normalised[nesting-1] += normalised[nesting]
			}
			normalised[nesting-1] += " "
			needsProtection = false
			nesting--
			inSpaces = true
		} else if inSpaces {
			nesting++
			normalised[nesting] = string(character)
			inSpaces = false
		} else {
			normalised[nesting] += string(character)
			if !inSpaces && 'A' <= character && character <= 'Z' {
				needsProtection = true
			}
		}
		fmt.Printf("%s", string(character))
	}

	fmt.Println()
	result := title
	if nesting < 1 {
		fmt.Println("Nesting already at 0. THis can't happen")
	} else {
		if nesting > 1 {
			fmt.Println("Missing }")
		}

		result = ""
		for index := nesting; index >= 0; index-- {
			result = normalised[index] + result
		}
	}

	fmt.Println(normalised)
	fmt.Println(result)
}

func ISBN (rawISBN string) string {
	var trimISBNStart = regexp.MustCompile(`^ *ISBN[-]*[1,0,3]*[:*] *`)

	trimmedISBN := strings.TrimSpace(trimISBNStart.ReplaceAllString(rawISBN, ""))
	
	return trimmedISBN
}

func main() {
	if Play {
	
	// validID.MatchString("adam[23]")
	fmt.Println(ISBN(" ISBN10:  123-45 "),"!")
	fmt.Println(ISBN(" ISBN-10: 123-45 "),"!")
	fmt.Println(ISBN(" ISBN-13: 123-45 "),"!")
	fmt.Println(ISBN(" ISBN: 123-45 "),"!")

//	strings.TrimSpace
		// Play
		// TITLES
		// Macro calls always protected.
		// { => nest
		// \ => in macro name to next space
		// \{, \&, => no protection needed
		// \', \^, etc ==> no space to next char needed
		// \x Y ==> keep space
		// " -- " ==> Sub title mode
		// ": " ==> Sub title mode
		// [nonspace]+[A-Z]+[nonspace]* => protect
		//
//		Titles("{Hello {{World}}   HOW {aRe} Things}")
//		Titles("{ Hello {{World}} HOW   a{R}e Things}")
//		Titles("{Hello {{World}} HOW a{R}e Things")
//		Titles("Hello { { Wo   rld}} HOW a{R}e Things")
		// Braces can prevent kerning between letters, so it is in general preferable to enclose entire words and not just single letters in braces to protect them.

	} else {
		Reporting := TInteraction{}

		Library = TBibTeXLibrary{}
		Library.Initialise(Reporting)
		Library.SetFilePath(BibTeXFolder)
		Library.ReadLegacyAliases()

		OldLibrary := TBibTeXLibrary{}
		OldLibrary.Initialise(Reporting)
		OldLibrary.legacyMode = true
		OldLibrary.ReadLegacyAliases()

		// Use Progress call
		fmt.Println("Reading main library")
		BibTeXParser := TBibTeXStream{}
		BibTeXParser.Initialise(Reporting, &Library)
		BibTeXParser.ParseBibFile(ErikProperBib)
		fmt.Println("Size of", ErikProperBib, "is:", len(Library.entryType))

		Library.CheckPreferredAliases()
		Library.CheckDBLPAliases()

		//	fmt.Println("Reading old libraries")
		//	BibTeXParser.Initialise(Reporting, &OldLibrary)
		//  NOTE: Ignore DSK fields. Only use file fields. If the file is there.
		//  Maybe import date-added/modified fields, if not exists yet.
		//
		//	BibTeXParser.ParseBiBFile("/Users/erikproper/BibTeX/Old/ErikProper.bib")
		//	BibTeXParser.ParseBiBFile("/Users/erikproper/BibTeX/Old/Old.bib")
		//
		//	BibTeXParser.ParseBiBFile("Convert.bib")
		//	BibTeXParser.ParseBiBFile("/Users/erikproper/BibTeX/MyLibrary.bib")
		//	fmt.Println("Size old:", len(OldLibrary.entryType))

		//	Test := "YnBsaXN0MDDSAQIDBFxyZWxhdGl2ZVBhdGhYYm9va21hcmtfECBGaWxlcy9FUC0yMDI0LTA0LTAzLTIyLTA3LTMxLnBkZk8RBERib29rRAQAAAAABBAwAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABAAwAABQAAAAEBAABVc2VycwAAAAoAAAABAQAAZXJpa3Byb3BlcgAACQAAAAEBAABOZXh0Y2xvdWQAAAAHAAAAAQEAAExpYnJhcnkABgAAAAEBAABCaUJUZVgAAAUAAAABAQAARmlsZXMAAAAaAAAAAQEAAEVQLTIwMjQtMDQtMDMtMjItMDctMzEucGRmAAAcAAAAAQYAAAQAAAAUAAAAKAAAADwAAABMAAAAXAAAAGwAAAAIAAAABAMAABVdAAAAAAAACAAAAAQDAADeCAQAAAAAAAgAAAAEAwAAuMRlBwAAAAAIAAAABAMAAAEDjwcAAAAACAAAAAQDAAApz5oHAAAAAAgAAAAEAwAAxmdJCgAAAAAIAAAABAMAAOeBbQkAAAAAHAAAAAEGAAC0AAAAxAAAANQAAADkAAAA9AAAAAQBAAAUAQAACAAAAAAEAABBxTC0xIAAABgAAAABAgAAAQAAAAAAAAAPAAAAAAAAAAAAAAAAAAAACAAAAAQDAAAFAAAAAAAAAAQAAAADAwAA9QEAAAgAAAABCQAAZmlsZTovLy8MAAAAAQEAAE1hY2ludG9zaCBIRAgAAAAEAwAAAFChG3MAAAAIAAAAAAQAAEHFlk7IgAAAJAAAAAEBAABBQUY2QTJFRi01MTg0LTQ1OEItQTM2RC04QzJDMTU5MDBENUMYAAAAAQIAAIEAAAABAAAA7xMAAAEAAAAAAAAAAAAAAAEAAAABAQAALwAAAAAAAAABBQAA/QAAAAECAAAzNjllNzI1YTcyMTkxYmRhYjZlYzMwMzMxZjUyYTQyMjM1OTQ5YTUzZDdlZmNlNmMzYzc0NjUzZGFjZWIyODNkOzAwOzAwMDAwMDAwOzAwMDAwMDAwOzAwMDAwMDAwOzAwMDAwMDAwMDAwMDAwMjA7Y29tLmFwcGxlLmFwcC1zYW5kYm94LnJlYWQtd3JpdGU7MDE7MDEwMDAwMTI7MDAwMDAwMDAwOTZkODFlNzswMTsvdXNlcnMvZXJpa3Byb3Blci9uZXh0Y2xvdWQvbGlicmFyeS9iaWJ0ZXgvZmlsZXMvZXAtMjAyNC0wNC0wMy0yMi0wNy0zMS5wZGYAAAAAzAAAAP7///8BAAAAAAAAABAAAAAEEAAAkAAAAAAAAAAFEAAAJAEAAAAAAAAQEAAAWAEAAAAAAABAEAAASAEAAAAAAAACIAAAJAIAAAAAAAAFIAAAlAEAAAAAAAAQIAAApAEAAAAAAAARIAAA2AEAAAAAAAASIAAAuAEAAAAAAAATIAAAyAEAAAAAAAAgIAAABAIAAAAAAAAwIAAAMAIAAAAAAAABwAAAeAEAAAAAAAARwAAAFAAAAAAAAAASwAAAiAEAAAAAAACA8AAAOAIAAAAAAAAACAANABoAIwBGAAAAAAAAAgEAAAAAAAAABQAAAAAAAAAAAAAAAAAABI4="
		//	data, _ := base64.StdEncoding.DecodeString(Test)
		//	Str := string(data)
		//	Start := Str[strings.Index(Str, "relativePathXbookmark")+len("relativePathXbookmark")+3 : strings.Index(Str, "DbookD")-3]
		//	fmt.Printf("%q\n", Start)

		// Use Progress call
		fmt.Println("Exporting updated library", ErikProperBib)
		Library.WriteBibTeXFile(ErikProperBib)
		Library.WriteLegacyAliases()

		// log import ...
		//
		//	log.Fatal(err)
	}
}
