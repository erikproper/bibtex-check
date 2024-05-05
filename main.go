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
	BibTeXFolder   = "/Users/erikproper/BibTeX/"
	BibFile        = "ErikProper.bib"
	AliasesFile    = "ErikProper.aliases"
	ChallengesFile = "ErikProper.challenges"
	MainLibrary    = "main"
)

func InitialiseMainLibrary() bool {
	Library = TBibTeXLibrary{}
	Library.Initialise(Reporting, MainLibrary)
	Library.SetFilesRoot(BibTeXFolder)
	Library.ReadAliases(AliasesFile)
	Library.ReadChallenges(ChallengesFile)

	return true
}

func OpenMainBibFile() bool {
	if Library.ReadBib(BibFile) {
		Library.ReportLibrarySize()
		Library.CheckAliases()
		Library.CheckEntries()

		return true
	} else {
		return false
	}
}

func CleanKey(rawKey string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(rawKey, "\\cite{", ""), "cite{", ""), "}", ""))
}

func main() {
	Reporting = TInteraction{}
	writeAliases := false
	writeChallenges := false
	writeBibFile := false

	switch {
	case len(os.Args) == 1:
		if InitialiseMainLibrary() && OpenMainBibFile() {
			writeBibFile = true
			writeAliases = true
			writeChallenges = true
		}

	case len(os.Args) == 2 && os.Args[1] == "-meta":
		if InitialiseMainLibrary() && OpenMainBibFile() {
			writeBibFile = true
			writeAliases = false
			writeChallenges = true

			OldLibrary := TBibTeXLibrary{}
			OldLibrary.Progress("Reading legacy library")
			OldLibrary.Initialise(Reporting, "legacy")
			OldLibrary.legacyMode = true
			OldLibrary.SetFilesRoot(BibTeXFolder)
			OldLibrary.ReadAliases(AliasesFile)

			BibTeXParser := TBibTeXStream{}
			BibTeXParser.Initialise(Reporting, &OldLibrary)
			BibTeXParser.ParseBibFile(BibTeXFolder + "Old/Old1.bib")
			BibTeXParser.ParseBibFile(BibTeXFolder + "Old/Old2.bib")
			BibTeXParser.ParseBibFile(BibTeXFolder + "Old/Old3.bib")
			BibTeXParser.ParseBibFile(BibTeXFolder + "Old/Old4.bib")
			BibTeXParser.ParseBibFile(BibTeXFolder + "Old/Old5.bib")
			BibTeXParser.ParseBibFile(BibTeXFolder + "Old/Old6.bib")
			BibTeXParser.ParseBibFile(BibTeXFolder + "Old/Old7.bib")

			OldLibrary.ReportLibrarySize()

			var stripUniquePrefix = regexp.MustCompile(`^[0-9]*AAAAA`)
			// 20673AAAAAzhai2005extractingdata [0-9]*AAAAA
			for oldEntry, oldType := range OldLibrary.entryType {
				newKey, newType, isEntry := Library.LookupEntryWithType(stripUniquePrefix.ReplaceAllString(oldEntry, ""))

				if isEntry {
					// We don't have a set type function??
					Library.entryType[newKey] = Library.ResolveFieldValue(newKey, EntryTypeField, oldType, newType)

					// EntryFields function???
					for oldField, oldValue := range OldLibrary.entryFields[oldEntry] {
						if oldField == "file" {
							if oldValue != "" && Library.entryFields[newKey]["bdsk-file-1"] == "" {
								Library.entryFields[newKey]["local-url"] = oldValue
							}
						}

						// The next test should be a nice function IsAllowedEntryField(Library.entryType[newKey], oldField)
						if BibTeXAllowedEntryFields[Library.entryType[newKey]].Set().Contains(oldField) {
							switch oldField {
							case "crossref":
								Library.entryFields[newKey][oldField] = Library.ResolveFieldValue(newKey, oldField, oldValue, Library.entryFields[newKey][oldField])

							case "chapter":
								Library.entryFields[newKey][oldField] = Library.ResolveFieldValue(newKey, oldField, oldValue, Library.entryFields[newKey][oldField])

							case "dblp":
								Library.entryFields[newKey][oldField] = Library.ResolveFieldValue(newKey, oldField, oldValue, Library.entryFields[newKey][oldField])

							case "doi":
								Library.entryFields[newKey][oldField] = Library.ResolveFieldValue(newKey, oldField, oldValue, Library.entryFields[newKey][oldField])

							case "pages":
								Library.entryFields[newKey][oldField] = Library.ResolveFieldValue(newKey, oldField, oldValue, Library.entryFields[newKey][oldField])

							}
						}
					}
				}
			}
		}

	case len(os.Args) == 3 && os.Args[1] == "-alias":
		Reporting.SetSilenced()
		InitialiseMainLibrary()

		// Function call.
		alias, ok := Library.preferredAliases[CleanKey(os.Args[2])]

		if ok {
			fmt.Println(alias)
		}

	case len(os.Args) > 2 && os.Args[1] == "-key":
		Reporting.SetSilenced()
		InitialiseMainLibrary()

		if OpenMainBibFile() {
			// Function call.
			actualKey, ok := Library.LookupEntry(CleanKey(os.Args[2]))
			if ok {
				fmt.Println(actualKey)
			}
		}

	case len(os.Args) > 3 && os.Args[1] == "-map":
		if InitialiseMainLibrary() && OpenMainBibFile() {
			keysString := ""
			writeAliases = true

			for _, keyString := range os.Args[2:] {
				keysString += "," + CleanKey(keyString)
			}
			keyStrings := strings.Split(keysString, ",")

			key := keyStrings[len(keyStrings)-1]
			for _, alias := range keyStrings[1 : len(keyStrings)-1] {
				fmt.Println("Mapping", alias, "to", key)
				Library.AddKeyAlias(alias, key, true)
			}
		}

	case len(os.Args) > 2 && os.Args[1] == "-preferred":
		alias := CleanKey(os.Args[2])

		if CheckPreferredAliasValidity(alias) {
			writeAliases = true

			InitialiseMainLibrary()

			if len(os.Args) == 4 {
				key := CleanKey(os.Args[3])
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
		Library.WriteBibTeXFile()
	}

	if writeAliases {
		Library.WriteAliases()
	}

	if writeChallenges {
		Library.WriteChallenges()
	}
}
