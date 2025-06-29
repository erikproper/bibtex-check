package main

import (
	//	"bufio"
	"fmt"
	"os"
	//	"regexp"
	"strings"
)

var (
	Library   TBibTeXLibrary
	Reporting TInteraction
)

const (
	BaseName     = "ErikProper"
	BibTeXFolder = "/Users/erikproper/BibTeX/"
	BibFile      = BaseName + ".bib"
	MainLibrary  = "main"
)

// Put this one in a SEPARATE FILE
// Update bib mapping file
//func (l *TBibTeXLibrary) UpdateBibMap(file string) {
//	bibMap := TStringMap{}
//
//	l.readFile(file, "Reading mapping file %s", func(line string) {
//		elements := strings.Split(line, " ")
//		if len(elements) < 2 {
//			l.Warning("File line too short: %s", line)
//			return
//		}
//
//		candidateKey := elements[1]
//		if lookupKey, isAlias := l.KeyAliasToKey[candidateKey]; isAlias {
//			bibMap[elements[0]] = lookupKey
//		} else {
//			bibMap[elements[0]] = candidateKey
//		}
//	})
//
//	l.writeFile(file, "Writing mapping file %s", func(bibWriter *bufio.Writer) {
//		for alias, key := range bibMap {
//			bibWriter.WriteString(alias + " " + key + "\n")
//		}
//	})
//}

///// Add these to library.go ??

func OpenLibraryToUpdate() bool {
	Library = TBibTeXLibrary{}
	Library.Initialise(Reporting, MainLibrary, BibTeXFolder, BaseName)

	Library.ReadKeyOldiesFile()
	Library.ReadKeyHintsFile()

	Library.ReadNameMappingsFile()
	Library.ReadGenericFieldAliasesFile()
	Library.ReadEntryFieldAliasesFile()
	Library.ReadFieldMappingsFile()
	Library.CheckFieldMappings()

	result := false
	if Library.ValidCache() {
		Library.ReadCache()
		result = true
	} else {
		result = Library.ReadBib(BibFile) // Needed to pass this parameter ... BaseName on initialise !?
		if result {
			Library.WriteCache()
		}
	}

	Library.ReportLibrarySize()
	Library.CheckKeyOldiesConsistency()

	return result
}

func OpenLibraryToReport() bool {
	Library = TBibTeXLibrary{}
	Library.Initialise(Reporting, MainLibrary, BibTeXFolder, BaseName)

	result := false
	Library.ReadKeyOldiesFile()
	if Library.ValidCache() {
		Library.ReadCache()
		result = true
	} else {
		Library.ReadNameMappingsFile()
		Library.ReadGenericFieldAliasesFile()
		Library.ReadEntryFieldAliasesFile()
		Library.ReadFieldMappingsFile()

		result = Library.ReadBib(BibFile) // Needed to pass this parameter ... BaseName on initialise !?
		if result {
			Library.WriteCache()
		}
	}

	Library.ReportLibrarySize()

	return result
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

	switch {
	case len(os.Args) == 1:
		if OpenLibraryToUpdate() {
			writeBibFile = true
			writeAliases = true
			writeMappings = true

			Library.CheckEntries()
			Library.ReadNonDoublesFile()
			Library.CheckFiles()
			Library.WriteNonDoublesFile()
		}

	case len(os.Args) == 2 && os.Args[1] == "-pdfs":
		Reporting.SetInteractionOff()
		if OpenLibraryToReport() {
			for key := range Library.EntryFields {
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

		//	case len(os.Args) == 2 && os.Args[1] == "-legacy": // Keep for -import / -sync
		//		if InitialiseMainLibrary() && OpenMainBibFile() {
		//			writeBibFile = true
		//			writeAliases = false
		//			writeMappings = true
		//			Library.CheckEntries()
		//			Library.ReadNonDoublesFile()
		//
		//			OldLibrary := TBibTeXLibrary{}
		//			OldLibrary.Progress("Reading legacy library")
		//			OldLibrary.Initialise(Reporting, "legacy", BibTeXFolder, BaseName)
		//			OldLibrary.ReadAliasesFiles()
		//			OldLibrary.ReadFieldMappingsFile()
		//
		//			BibTeXParser := TBibTeXStream{}
		//			BibTeXParser.Initialise(Reporting, &OldLibrary)
		//			BibTeXParser.ParseBibFile(BibTeXFolder + "Old/Old.bib")
		//
		//			OldLibrary.ReportLibrarySize()
		//
		//			var stripUniquePrefix = regexp.MustCompile(`^[0-9]*AAAAA`)
		//			// 20673AAAAAzhai2005extractingdata [0-9]*AAAAA
		//			for oldEntry, oldType := range OldLibrary.EntryFields {
		//				cleanOldEntry := stripUniquePrefix.ReplaceAllString(oldEntry, "")
		//
		//				newKey, newType, isEntry := Library.DeAliasEntryKeyWithType(cleanOldEntry)
		//
		//				if Library.EntryFieldValueity(newKey, DBLPField) != "" {
		//					if isEntry && Library.EntryFieldValueity(newKey, DBLPField) != "" {
		//						// We don't have a set type function??
		//						Library.EntryTypes[newKey] = Library.ResolveFieldValue(newKey, oldEntry, EntryTypeField, oldType, newType)
		//
		//						crossrefKey := Library.EntryFieldValueity(newKey, "crossref")
		//
		//						// EntryFields function???
		//						for oldField, oldValue := range OldLibrary.EntryFields[oldEntry] {
		//							// The next test should be a nice function IsAllowedEntryField(Library.EntryTypes[newKey], oldField)
		//							if BibTeXAllowedEntryFields[Library.EntryTypes[newKey]].Set().Contains(oldField) && BibTeXImportFields.Contains(oldField) {
		//								if crossrefKey != "" && BibTeXMustInheritFields.Contains(oldField) {
		//									target := Library.MaybeResolveFieldValue(crossrefKey, oldEntry, oldField, oldValue, Library.EntryFieldValueity(crossrefKey, oldField))
		//
		//									if oldField == "booktitle" {
		//										if Library.EntryFields[crossrefKey][TitleField] == Library.EntryFields[crossrefKey]["booktitle"] {
		//											Library.EntryFields[crossrefKey][TitleField] = target
		//										}
		//									}
		//
		//									Library.EntryFields[crossrefKey][oldField] = target
		//								} else {
		//									Library.EntryFields[newKey][oldField] = Library.ResolveFieldValue(newKey, oldEntry, oldField, oldValue, Library.EntryFields[newKey][oldField])
		//								}
		//							}
		//						}
		//					} else {
		//						fmt.Println("Old entry is not mapped:", cleanOldEntry)
		//
		//						newKey := Library.NewKey()
		//
		//						Library.EntryTypes[newKey] = OldLibrary.EntryTypes[oldEntry]
		//						Library.EntryFields[newKey] = OldLibrary.EntryFields[oldEntry]
		//
		//						Library.AddKeyAlias(cleanOldEntry, newKey)
		//					}
		//
		//					FIXThatShouldBeChecks(newKey)
		//
		//					Library.WriteNonDoublesFile()
		//					Library.WriteAliasesFiles()
		//					Library.WriteMappingsFiles()
		//					Library.WriteBibTeXFile()
		//				}
		//			}
		//		}

		//	case len(os.Args) == 3 && os.Args[1] == "-update_map":
		//		InitialiseMainLibrary()
		//		Library.UpdateBibMap(os.Args[2])

	case len(os.Args) == 3 && os.Args[1] == "-alias":
		Reporting.SetInteractionOff()

		if OpenLibraryToReport() {
			alias := Library.PreferredKey(Library.DeAliasEntryKey(CleanKey(os.Args[2])))
			if alias != "" {
				fmt.Println(alias)
			}
		}

	case len(os.Args) > 2 && os.Args[1] == "-entry":
		Reporting.SetInteractionOff()

		if OpenLibraryToReport() {
			fmt.Println(Library.EntryString(Library.DeAliasEntryKey(CleanKey(os.Args[2])), ""))
		}

	case len(os.Args) == 3 && os.Args[1] == "-key":
		Reporting.SetInteractionOff()

		if OpenLibraryToReport() {
			fmt.Println(Library.DeAliasEntryKey(CleanKey(os.Args[2])))
		}

	case len(os.Args) == 2 && os.Args[1] == "-fixall":
		if OpenLibraryToUpdate() {
			Library.CheckEntries()
			Library.ReadNonDoublesFile()

			count := 0
			for key := range Library.EntryFields {
				count++
				fmt.Println("Entry count: ", count)
				FIXThatShouldBeChecks(key)
			}
			Library.WriteNonDoublesFile()

			writeBibFile = true
			writeAliases = true
			writeMappings = true
		}

	case len(os.Args) == 2 && os.Args[1] == "-fixdblp":
		if OpenLibraryToUpdate() {
			Library.CheckEntries()
			Library.ReadNonDoublesFile()

			count := 0
			for key := range Library.EntryFields {
				if Library.EntryFieldValueity(key, DBLPField) != "" {
					if Library.EntryFields[key][EntryTypeField] == "book" ||
						Library.EntryFields[key][EntryTypeField] == "incollection" ||
						Library.EntryFields[key][EntryTypeField] == "misc" ||
						Library.EntryFields[key][EntryTypeField] == "inbook" ||
						Library.EntryFields[key][EntryTypeField] == "booklet" ||
						Library.EntryFields[key][EntryTypeField] == "manual" ||
						Library.EntryFields[key][EntryTypeField] == "mastersthesis" ||
						Library.EntryFields[key][EntryTypeField] == "phdthesis" ||
						Library.EntryFields[key][EntryTypeField] == "techreport" ||
						Library.EntryFields[key][EntryTypeField] == "proceedings" ||
						Library.EntryFields[key][EntryTypeField] == "inproceedings" ||
						Library.EntryFields[key][EntryTypeField] == "article" {
						count++
						fmt.Println("Entry count: ", count)
						Library.CheckDBLP(key)
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

		if OpenLibraryToUpdate() {
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
		if OpenLibraryToUpdate() {
			Library.CheckEntries()
			Library.ReadNonDoublesFile()

			writeBibFile = true
			writeAliases = true
			writeMappings = true

			if Library.LookupDBLPKey(os.Args[2]) == "" {
				// Leads to a double READ ... NOT NEEDED. MaybeAdd does it and the DBLPCHeck again ...
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

		if OpenLibraryToUpdate() {
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
		if OpenLibraryToUpdate() {
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
				Library.AddKeyAlias(alias, key)
			}

			Library.CheckEntries()
			FIXThatShouldBeChecks(key)
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
		Library.CheckEntries()
		Library.WriteBibTeXFile()
		Library.WriteCache()
	}
}
