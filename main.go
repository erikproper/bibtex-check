package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	AllowLegacy bool
	Library     TBibTeXLibrary
	Reporting   TInteraction
)

const (
	BaseName     = "ErikProper"
	BibTeXFolder = "/Users/erikproper/BibTeX/"
	BibFile      = BaseName + ".bib"
	MainLibrary  = "main"
)

// Put this one in a SEPARATE FILE
// Update bib mapping file
func (l *TBibTeXLibrary) UpdateBibMap(file string) {
	bibMap := TStringMap{}

	l.readFile(file, "Reading mapping file %s", func(line string) {
		elements := strings.Split(line, " ")
		if len(elements) < 2 {
			l.Warning("File line too short: %s", line)
			return
		}

		candidateKey := elements[1]
		if lookupKey, isAlias := l.KeyAliasToKey[candidateKey]; isAlias {
			bibMap[elements[0]] = lookupKey
		} else {
			bibMap[elements[0]] = candidateKey
		}
	})

	l.writeFile(file, "Writing mapping file %s", func(bibWriter *bufio.Writer) {
		for alias, key := range bibMap {
			bibWriter.WriteString(alias + " " + key + "\n")
		}
	})
}

func InitialiseMainLibrary() bool {
	Library = TBibTeXLibrary{}
	Library.Initialise(Reporting, MainLibrary, BibTeXFolder, BaseName)

	Library.ReadAliasesFiles()
	Library.ReadFieldMappingsFile()
	Library.CheckAliases()

	return true
}

func OpenMainBibFile() bool {

	if Library.ReadBib(BibFile) {
		Library.ReportLibrarySize()
		Library.CheckKeyAliasesConsistency()

		//Library.CreateTitleIndex()

		return true
	} else {
		return false
	}
}

func FIXThatShouldBeChecks(key string) {
	Library.CheckNeedToMergeForEqualTitles(key)
	Library.CheckNeedToSplitBookishEntry(key)
	Library.CheckDBLP(key)
}

func CleanKey(rawKey string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(rawKey, "\\cite{", ""), "cite{", ""), "}", ""))
}

func main() {
	Reporting = TInteraction{}
	writeAliases := false
	writeMappings := false
	writeBibFile := false
	AllowLegacy = false

	switch {
	case len(os.Args) == 1:
		if InitialiseMainLibrary() && OpenMainBibFile() {
			writeBibFile = true
			writeAliases = true
			writeMappings = true

			Library.CheckEntries()

			// This reading should be done on-demand
			Library.ReadNonDoublesFile()
			Library.CheckFiles()
			Library.WriteNonDoublesFile()
		}

	case len(os.Args) == 2 && os.Args[1] == "-pdfs":
		Reporting.SetInteractionOff()
		if InitialiseMainLibrary() && OpenMainBibFile() {
			for key := range Library.EntryTypes {
				filePath := Library.FilesRoot + FilesFolder + key + ".pdf"
				if !FileExists(filePath) {
					URL := Library.EntryFieldValueity(key, "url")
					if URL != "" && URL[len(URL)-4:] == ".pdf" {
						fmt.Println("get direct", filePath, "\""+URL+"\"")
					}

					DOI := Library.EntryFieldValueity(key, "doi")
					if strings.HasPrefix(DOI, "10.1007/") {
						fmt.Println("get springer", filePath, "\"https://link.springer.com/chapter/"+DOI+"#preview\"")
					}
				}
			}
		}

	case len(os.Args) == 2 && os.Args[1] == "-legacy": // Keep for -import / -sync
		if InitialiseMainLibrary() && OpenMainBibFile() {
			writeBibFile = true
			writeAliases = false
			writeMappings = true
			AllowLegacy = true
			Library.CheckEntries()
			Library.ReadNonDoublesFile()

			OldLibrary := TBibTeXLibrary{}
			OldLibrary.Progress("Reading legacy library")
			OldLibrary.Initialise(Reporting, "legacy", BibTeXFolder, BaseName)
			OldLibrary.legacyMode = true
			OldLibrary.ReadAliasesFiles()
			OldLibrary.ReadFieldMappingsFile()

			BibTeXParser := TBibTeXStream{}
			BibTeXParser.Initialise(Reporting, &OldLibrary)
			BibTeXParser.ParseBibFile(BibTeXFolder + "Old/Old.bib")

			OldLibrary.ReportLibrarySize()

			var stripUniquePrefix = regexp.MustCompile(`^[0-9]*AAAAA`)
			// 20673AAAAAzhai2005extractingdata [0-9]*AAAAA
			for oldEntry, oldType := range OldLibrary.EntryTypes {
				cleanOldEntry := stripUniquePrefix.ReplaceAllString(oldEntry, "")

				newKey, newType, isEntry := Library.DeAliasEntryKeyWithType(cleanOldEntry)

				if Library.EntryFieldValueity(newKey, "dblp") != "" {
					if isEntry && Library.EntryFieldValueity(newKey, "dblp") != "" {
						// We don't have a set type function??
						Library.EntryTypes[newKey] = Library.ResolveFieldValue(newKey, oldEntry, EntryTypeField, oldType, newType)

						crossrefKey := Library.EntryFieldValueity(newKey, "crossref")

						// EntryFields function???
						for oldField, oldValue := range OldLibrary.EntryFields[oldEntry] {
							if oldField == "file" {
								if oldValue != "" && Library.EntryFields[newKey]["bdsk-file-1"] == "" {
									Library.EntryFields[newKey]["local-url"] = oldValue
								}
							}

							// The next test should be a nice function IsAllowedEntryField(Library.EntryTypes[newKey], oldField)
							if BibTeXAllowedEntryFields[Library.EntryTypes[newKey]].Set().Contains(oldField) && BibTeXImportFields.Contains(oldField) {
								if crossrefKey != "" && BibTeXMustInheritFields.Contains(oldField) {
									target := Library.MaybeResolveFieldValue(crossrefKey, oldEntry, oldField, oldValue, Library.EntryFieldValueity(crossrefKey, oldField))

									if oldField == "booktitle" {
										if Library.EntryFields[crossrefKey]["title"] == Library.EntryFields[crossrefKey]["booktitle"] {
											Library.EntryFields[crossrefKey]["title"] = target
										}
									}

									Library.EntryFields[crossrefKey][oldField] = target
								} else {
									Library.EntryFields[newKey][oldField] = Library.ResolveFieldValue(newKey, oldEntry, oldField, oldValue, Library.EntryFields[newKey][oldField])
								}
							}
						}
					} else {
						fmt.Println("Old entry is not mapped:", cleanOldEntry)

						newKey := Library.NewKey()

						Library.EntryTypes[newKey] = OldLibrary.EntryTypes[oldEntry]
						Library.EntryFields[newKey] = OldLibrary.EntryFields[oldEntry]

						Library.AddKeyAlias(cleanOldEntry, newKey)
					}

					FIXThatShouldBeChecks(newKey)

					Library.WriteNonDoublesFile()
					Library.WriteAliasesFiles()
					Library.WriteMappingsFiles()
					Library.WriteBibTeXFile()
				}
			}
		}

	case len(os.Args) == 3 && os.Args[1] == "-update_map":
		InitialiseMainLibrary()
		Library.UpdateBibMap(os.Args[2])

	case len(os.Args) == 3 && os.Args[1] == "-alias":
		Reporting.SetInteractionOff()
		InitialiseMainLibrary()

		// Function call.
		alias, ok := Library.PreferredKeyAliases[Library.DeAliasEntryKey(CleanKey(os.Args[2]))]
		if ok {
			fmt.Println(alias)
		}

	case len(os.Args) > 2 && os.Args[1] == "-entry":
		Reporting.SetInteractionOff()

		if InitialiseMainLibrary() && OpenMainBibFile() {
			fmt.Println(Library.EntryString(Library.DeAliasEntryKey(CleanKey(os.Args[2]))))
		}

	case len(os.Args) == 3 && os.Args[1] == "-key":
		Reporting.SetInteractionOff()

		if InitialiseMainLibrary() && OpenMainBibFile() {
			fmt.Println(Library.DeAliasEntryKey(CleanKey(os.Args[2])))
		}

	case len(os.Args) == 2 && os.Args[1] == "-fixall":
		if InitialiseMainLibrary() && OpenMainBibFile() {
			Library.CheckEntries()
			Library.ReadNonDoublesFile()

			for key := range Library.EntryTypes {
				if Library.EntryFieldValueity(key, "dblp") != "" {
					if Library.EntryTypes[key] == "book" ||
						Library.EntryTypes[key] == "incollection" ||
						Library.EntryTypes[key] == "inbook" ||
						Library.EntryTypes[key] == "article" {
						FIXThatShouldBeChecks(key)
					}
				}
			}
			Library.WriteNonDoublesFile()

			writeBibFile = true
			writeAliases = true
			writeMappings = true
		}

	case len(os.Args) > 2 && os.Args[1] == "-fix":
		keysString := ""

		for _, keyString := range os.Args[2:] {
			keysString += "," + CleanKey(keyString)
		}
		keyStrings := strings.Split(keysString, ",")

		if InitialiseMainLibrary() && OpenMainBibFile() {
			Library.CheckEntries()
			Library.ReadNonDoublesFile()

			for _, key := range keyStrings[1:] {
				FIXThatShouldBeChecks(key)
			}

			Library.WriteNonDoublesFile()

			writeBibFile = true
			writeAliases = true
			writeMappings = true
		}

	case len(os.Args) == 3 && os.Args[1] == "-dblp_add":
		if InitialiseMainLibrary() && OpenMainBibFile() {
			Library.CheckEntries()
			Library.ReadNonDoublesFile()

			writeBibFile = true
			writeAliases = true
			writeMappings = true

			if Library.LookupDBLPKey(os.Args[2]) == "" {
				Added := Library.MaybeAddDBLPEntry(os.Args[2])
				if Added != "" {
					FIXThatShouldBeChecks(Added)
				}
			}

			Library.WriteNonDoublesFile()
		}

	case len(os.Args) > 2 && os.Args[1] == "-merge":
		keysString := ""

		for _, keyString := range os.Args[2:] {
			keysString += "," + CleanKey(keyString)
		}
		keyStrings := strings.Split(keysString, ",")

		if InitialiseMainLibrary() && OpenMainBibFile() {
			Library.CheckEntries()
			Library.ReadNonDoublesFile()

			writeBibFile = true
			writeAliases = true
			writeMappings = true

			key := Library.DeAliasEntryKey(keyStrings[len(keyStrings)-1])
			for _, alias := range keyStrings[1 : len(keyStrings)-1] {
				Library.MergeEntries(alias, key)
			}

			for _, key := range keyStrings[1:] {
				if Library.DeAliasEntryKey(key) == key {
					FIXThatShouldBeChecks(key)
				}
			}

			Library.WriteNonDoublesFile()
		}

	case len(os.Args) > 3 && os.Args[1] == "-map":
		if InitialiseMainLibrary() && OpenMainBibFile() {
			Library.CheckEntries()

			writeBibFile = true
			writeAliases = true
			writeMappings = true
			keysString := ""

			for _, keyString := range os.Args[2:] {
				keysString += "," + CleanKey(keyString)
			}
			keyStrings := strings.Split(keysString, ",")

			key := Library.DeAliasEntryKey(keyStrings[len(keyStrings)-1])
			for _, alias := range keyStrings[1 : len(keyStrings)-1] {
				fmt.Println("Mapping", alias, "to", key)
				Library.UpdateGroupKeys(alias, key)
				Library.AddKeyAlias(alias, key)
				Library.CheckPreferredKeyAliasesConsistency(key)
			}

			Library.CheckEntries()
			FIXThatShouldBeChecks(key)
		}

	case len(os.Args) > 2 && os.Args[1] == "-preferred":
		alias := CleanKey(os.Args[2])

		if IsValidPreferredKeyAlias(alias) {
			writeAliases = true

			InitialiseMainLibrary()

			if len(os.Args) == 4 {
				key := CleanKey(os.Args[3])
				Library.AddKeyAlias(alias, key)
			}

			Library.AddPreferredKeyAlias(alias)
			Library.NoEntryFieldAliasesFileWriting = true //// Temporary hack
			Library.WriteAliasesFiles()
		} else {
			fmt.Println("Not a valid preferred alias.")
		}

	default:
		fmt.Println("Parameters:", len(os.Args))
		fmt.Println(os.Args)
	}

	if writeAliases {
		Library.WriteAliasesFiles()
	}

	if writeMappings {
		Library.WriteMappingsFiles()
	}

	if writeBibFile {
		Library.WriteBibTeXFile()
	}
}
