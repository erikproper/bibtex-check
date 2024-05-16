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
	BaseName                = "ErikProper"
	BibTeXFolder            = "/Users/erikproper/BibTeX/"
	BibFile                 = BaseName + ".bib"
	KeyAliasesFile          = BaseName + ".keys"
	PreferredKeyAliasesFile = BaseName + ".preferred"
	NameAliasesFile         = BaseName + ".names"
	JournalAliasesFile      = BaseName + ".journals"
	PublisherAliasesFile    = BaseName + ".publishers"
	InstitutionAliasesFile  = BaseName + ".institutions"
	OrganisationAliasesFile = BaseName + ".organisations"
	SchoolsAliasesFile      = BaseName + ".schools"
	ChallengesFile          = BaseName + ".challenges"
	MainLibrary             = "main"
)

func InitialiseMainLibrary() bool {
	Library = TBibTeXLibrary{}
	Library.Initialise(Reporting, MainLibrary, BibTeXFolder)

	Library.ReadKeyAliases(KeyAliasesFile)
	Library.ReadPreferredKeyAliases(PreferredKeyAliasesFile)
	Library.ReadNameAliases(NameAliasesFile)
	Library.ReadJournalAliases(JournalAliasesFile)
	Library.ReadChallenges(ChallengesFile)

	return true
}

func OpenMainBibFile() bool {
	if Library.ReadBib(BibFile) {
		Library.ReportLibrarySize()
		// Check library consistency call:
		/**/
		Library.CheckKeyAliasesConsistency()
		/**/ Library.CheckPreferredKeyAliasesConsistency()
		/**/ Library.CheckEntries()

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
			OldLibrary.Initialise(Reporting, "legacy", BibTeXFolder)
			OldLibrary.legacyMode = true
			OldLibrary.ReadKeyAliases(KeyAliasesFile)
			OldLibrary.ReadNameAliases(NameAliasesFile)
			OldLibrary.ReadJournalAliases(JournalAliasesFile)

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
			for oldEntry, oldType := range OldLibrary.EntryTypes {
				newKey, newType, isEntry := Library.LookupEntryWithType(stripUniquePrefix.ReplaceAllString(oldEntry, ""))

				if isEntry {
					// We don't have a set type function??
					Library.EntryTypes[newKey] = Library.ResolveFieldValue(newKey, EntryTypeField, oldType, newType)

					// EntryFields function???
					for oldField, oldValue := range OldLibrary.EntryFields[oldEntry] {
						if oldField == "file" {
							if oldValue != "" && Library.EntryFields[newKey]["bdsk-file-1"] == "" {
								Library.EntryFields[newKey]["local-url"] = oldValue
							}
						}

						// The next test should be a nice function IsAllowedEntryField(Library.EntryTypes[newKey], oldField)
						if BibTeXAllowedEntryFields[Library.EntryTypes[newKey]].Set().Contains(oldField) && BibTeXImportFields.Contains(oldField) {
							Library.EntryFields[newKey][oldField] = Library.ResolveFieldValue(newKey, oldField, oldValue, Library.EntryFields[newKey][oldField])
						}
					}
				}
			}
		}

	case len(os.Args) == 3 && os.Args[1] == "-alias":
		Reporting.SetInteractionOff()
		InitialiseMainLibrary()

		// Function call.
		alias, ok := Library.PreferredKeyAliases[CleanKey(os.Args[2])]

		if ok {
			fmt.Println(alias)
		}

	case len(os.Args) > 2 && os.Args[1] == "-entry":
		Reporting.SetInteractionOff()

		if InitialiseMainLibrary() && OpenMainBibFile() {
			actualKey, ok := Library.LookupEntry(CleanKey(os.Args[2]))
			if ok {
				fmt.Println(Library.EntryString(actualKey))
			}
		}

	case len(os.Args) > 2 && os.Args[1] == "-key":
		Reporting.SetInteractionOff()

		if InitialiseMainLibrary() && OpenMainBibFile() {
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
				Library.AddKeyAlias(alias, key)
				Library.WriteKeyAliases()
			}
		}

	case len(os.Args) > 2 && os.Args[1] == "-preferred":
		alias := CleanKey(os.Args[2])

		if PreferredKeyAliasIsValid(alias) {
			writeAliases = true

			InitialiseMainLibrary()

			if len(os.Args) == 4 {
				key := CleanKey(os.Args[3])
				Library.AddKeyAlias(alias, key)
			}

			Library.AddPreferredKeyAlias(alias)
			Library.WriteKeyAliases()
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
		Library.WriteKeyAliases()
		Library.WritePreferredKeyAliases()
		Library.WriteNameAliases()
		Library.WriteJournalAliases()
	}

	if writeChallenges {
		Library.WriteChallenges()
	}
}
