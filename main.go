package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Remove this one as soon as we have migrated the legacy files
const AllowLegacy = true

var (
	Library   TBibTeXLibrary
	Reporting TInteraction
)

const (
	BibTeXFolder     = "/Users/erikproper/BibTeX/"
	PreferredAliases = BibTeXFolder + "PreferredAliases"
	AliasKeys        = BibTeXFolder + "Keys"
	ErikProperBib    = BibTeXFolder + "ErikProper.bib"
	KeysMapFile      = BibTeXFolder + "ErikProper.aliases"
	ChallengesFile   = BibTeXFolder + "ErikProper.challenges"
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

func Page(pages string) string {
	trimedPageRanges := ""

	trimedPageRanges = strings.TrimSpace(pages)
	trimedPageRanges = strings.ReplaceAll(trimedPageRanges, " ", "")
	trimedPageRanges = strings.ReplaceAll(trimedPageRanges, "--", "-")

	rangesList := ""
	comma := ""

	for _, pageRange := range strings.Split(trimedPageRanges, ",") {
		trimedPagesList := strings.Split(pageRange, "-")
		switch {
		case len(trimedPagesList) == 0:
			return pages

		case len(trimedPagesList) == 1:
			return trimedPagesList[0]

		case len(trimedPagesList) == 2:
			firstPagePair := strings.Split(trimedPagesList[0], ":")
			secondPagePair := strings.Split(trimedPagesList[1], ":")

			if len(firstPagePair) == 1 || len(secondPagePair) == 1 {
				rangesList += comma + trimedPagesList[0] + "--" + trimedPagesList[1]
			} else if len(firstPagePair) == 2 && len(secondPagePair) == 2 {
				if firstPagePair[0] == secondPagePair[0] {
					rangesList += comma + trimedPagesList[0] + "--" + secondPagePair[1]
				} else {
					rangesList += comma + trimedPagesList[0] + "--" + trimedPagesList[1]
				}
			} else {
				return pages
			}

		default:
			return pages
		}

		comma = ", "
	}

	return rangesList
}

func Play() {
	fmt.Println(Page("379--423,6:23--656"))
	fmt.Println(Page("10"))
	fmt.Println(Page("3 :10"))
	fmt.Println(Page("3: 10"))
	fmt.Println(Page("3: 10"))
	fmt.Println(Page("10-20--30"))
	fmt.Println(Page("10-20"))
	fmt.Println(Page("2:10-2:20"))
	fmt.Println(Page("2:10--3:20"))

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
}

func InitialiseLibrary() bool {
	Library.Progress("Initialising main library")
	Library = TBibTeXLibrary{}
	Library.Initialise(Reporting)
	Library.SetFilePath(BibTeXFolder)
	Library.ReadLegacyAliases()

	return true
}

func OpenLibrary() bool {
	InitialiseLibrary()

	// Use Progress call
	Library.Progress("Reading main library")
	BibTeXParser := TBibTeXStream{}
	BibTeXParser.Initialise(Reporting, &Library)
	if BibTeXParser.ParseBibFile(ErikProperBib) {
		fmt.Println("Size of", ErikProperBib, "is:", len(Library.entryType))
		Library.CheckPreferredAliases()
		Library.CheckDBLPAliases()

		return true
	} else {
		return false
	}
}

func main() {
	Reporting = TInteraction{}
	writeAliases := false
	writeBibFile := false

	switch {
	case len(os.Args) == 1:
		if OpenLibrary() {
			writeBibFile = true
			writeAliases = true
		}

	case len(os.Args) == 2 && os.Args[1] == "-meta":
		if OpenLibrary() {
			writeBibFile = false
			writeAliases = false

			OldLibrary := TBibTeXLibrary{}
			OldLibrary.Progress("Reading legacy library")
			OldLibrary.Initialise(Reporting)
			OldLibrary.legacyMode = true
			OldLibrary.ReadLegacyAliases()

			BibTeXParser := TBibTeXStream{}
			BibTeXParser.Initialise(Reporting, &OldLibrary)

			BibTeXParser.ParseBibFile("/Users/erikproper/BibTeX/Old/ErikProper.bib")
			//			BibTeXParser.ParseBibFile("/Users/erikproper/BibTeX/Old/Old.bib")
			//			BibTeXParser.ParseBibFile("Convert.bib")
			//			BibTeXParser.ParseBibFile("/Users/erikproper/BibTeX/MyLibrary.bib")
			//			fmt.Println("Size of legacy pool is:", len(OldLibrary.entryType))

			var stripUniquePrefix = regexp.MustCompile(`^[0-9]*AAAAA`)
			// 20673AAAAAzhai2005extractingdata [0-9]*AAAAA
			for oldEntry, oldType := range OldLibrary.entryType {
				newKey, newType, isEntry := Library.LookupEntryWithType(stripUniquePrefix.ReplaceAllString(oldEntry, ""))

				if isEntry {
					Library.entryType[newKey] = Library.ResolveFieldValue(newKey, EntryTypeField, oldType, newType)
				}
			}
		}

	case len(os.Args) == 2 && os.Args[1] == "-play":
		Play()

	case len(os.Args) == 3 && os.Args[1] == "-alias":
		Library.Silenced()

		key := strings.ReplaceAll(strings.ReplaceAll(os.Args[2], "\\cite{", ""), "}", "")

		InitialiseLibrary()

		alias, ok := Library.preferredAliases[key]

		if ok {
			fmt.Println(alias)
		}

	case len(os.Args) > 3 && os.Args[1] == "-map":
		if OpenLibrary() {
			keysString := ""
			writeAliases = true

			for _, keyString := range os.Args[2:] {
				keysString += "," + strings.ReplaceAll(strings.ReplaceAll(keyString, "\\cite{", ""), "}", "")
			}
			keyStrings := strings.Split(keysString, ",")

			key := keyStrings[len(keyStrings)-1]
			for _, alias := range keyStrings[1 : len(keyStrings)-1] {
				fmt.Println("Mapping", alias, "to", key)
				Library.AddKeyAlias(alias, key, true)
			}
		}

	case len(os.Args) > 2 && os.Args[1] == "-preferred":
		alias := strings.ReplaceAll(strings.ReplaceAll(os.Args[2], "\\cite{", ""), "}", "")

		if Library.IsValidPreferredAlias(alias) {
			writeAliases = true

			InitialiseLibrary()

			if len(os.Args) == 4 {
				key := strings.ReplaceAll(strings.ReplaceAll(os.Args[3], "\\cite{", ""), "}", "")
				Library.AddKeyAlias(alias, key, true)
			}

			Library.AddPreferredAlias(alias)
		} else {
			fmt.Println("Not a valid preferred alias.")
		}

	default:
		fmt.Println("Parameters:", len(os.Args))
		fmt.Println(os.Args)
	}

	if writeBibFile {
		fmt.Println("Exporting updated library", ErikProperBib)
		Library.WriteBibTeXFile(ErikProperBib)
	}

	if writeAliases {
		fmt.Println("Exporting updated aliases", KeysMapFile)
		Library.WriteLegacyAliases()
	}
}
