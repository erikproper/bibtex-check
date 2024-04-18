package main

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

/// Move library functions to adminitration

/// Comments list

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

/// Add comments + inspect ... also to the already splitted files.

/// Make things robust and reporting when file is not found

/// An KnownTypeTags(type, tag, IsMandatory) sequence, that populate the maps:
/// - AllowedTags
/// - AllowedTypes
/// - OptionalTypeTags
/// - MandatoryTypeTags
///
/// TypeAlias(type, alias)
/// TagAlias(tag, alias)
/// publisheD
/// For each AllowedTag:
/// - _XXX
/// - xXXX
///
/// This should actually be read/seeded from a config file
/// Create a config.go file for now. Later we can see what needs to be read or not.
///
/// for each tag used in entry:
///   if unknown then
///      if exists new_tag = TagAliasesMap[tag] then
///         if new_tag already has value then
///            ask
///         else
///            rename
///         fi
///      else
///         Add to UnknownTags list for current stream
///      fi
///
///   if resulting tag is AllowedTags, but not in AllowedTags list for this type
///      then warning
///
/// for each tag in MandatoryTypeTags[type]
///   if missing then warning
///
/// Per stream parse round:
///   for each UnknownTag report
///
///

/// First App

/// KInd[tag] = Which kind of cleaning/nornalisation needed
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

// Check usage of "New"

const (
	WarningEntryAlreadyExists = "Entry '%s' already exists"
)

type (
	TBiBTeXLibrary struct {
		entries       map[string]TStringMap
		currentKey    string
		usedTags      TStringSet
		warnOnDoubles bool
		TReporting    // Error reporting channel
	}
)

func (l *TBiBTeXLibrary) NewLibrary(reporting TReporting, warnOnDoubles bool) {
	l.entries = map[string]TStringMap{}
	l.usedTags = TStringSet{}
	l.currentKey = ""
	l.TReporting = reporting
	l.warnOnDoubles = warnOnDoubles
}

func (l *TBiBTeXLibrary) StartRecordingLibraryEntry(key string) bool {
	l.currentKey = key

	_, exists := l.entries[l.currentKey]

	if exists {
		if l.warnOnDoubles {
			l.Warning(WarningEntryAlreadyExists, l.currentKey)
		}
	} else {
		l.entries[l.currentKey] = TStringMap{}
	}

	return true
}

func (l *TBiBTeXLibrary) AssignTag(tag, value string) bool {
	l.entries[l.currentKey][tag] = value
	l.usedTags[tag] = true

	return true
}

func (l *TBiBTeXLibrary) FinishRecordingLibraryEntry() bool {
	// This is where we need to do a lot of checks ...

	return true
}

/////

var BiBTeXParser TBiBTeXStream
var Library TBiBTeXLibrary
var Reporting TReporting

func main() {
	Reporting.NewReporting()
	Library.NewLibrary(Reporting, false)
	BiBTeXParser.NewBiBTeXParser(Reporting, Library)
	BiBTeXParser.ParseBiBFile("Test.bib")

	fmt.Println(Library)

	for t, _ := range Library.usedTags {
		fmt.Println(t)
	}

	h := md5.New()
	io.WriteString(h, "zot:IJ6KKKAQ\n")
	//	fmt.Printf("%x\n", h.Sum(nil))

	Test := "YnBsaXN0MDDSAQIDBFxyZWxhdGl2ZVBhdGhYYm9va21hcmtfECBGaWxlcy9FUC0yMDI0LTA0LTAzLTIyLTA3LTMxLnBkZk8RBERib29rRAQAAAAABBAwAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABAAwAABQAAAAEBAABVc2VycwAAAAoAAAABAQAAZXJpa3Byb3BlcgAACQAAAAEBAABOZXh0Y2xvdWQAAAAHAAAAAQEAAExpYnJhcnkABgAAAAEBAABCaUJUZVgAAAUAAAABAQAARmlsZXMAAAAaAAAAAQEAAEVQLTIwMjQtMDQtMDMtMjItMDctMzEucGRmAAAcAAAAAQYAAAQAAAAUAAAAKAAAADwAAABMAAAAXAAAAGwAAAAIAAAABAMAABVdAAAAAAAACAAAAAQDAADeCAQAAAAAAAgAAAAEAwAAuMRlBwAAAAAIAAAABAMAAAEDjwcAAAAACAAAAAQDAAApz5oHAAAAAAgAAAAEAwAAxmdJCgAAAAAIAAAABAMAAOeBbQkAAAAAHAAAAAEGAAC0AAAAxAAAANQAAADkAAAA9AAAAAQBAAAUAQAACAAAAAAEAABBxTC0xIAAABgAAAABAgAAAQAAAAAAAAAPAAAAAAAAAAAAAAAAAAAACAAAAAQDAAAFAAAAAAAAAAQAAAADAwAA9QEAAAgAAAABCQAAZmlsZTovLy8MAAAAAQEAAE1hY2ludG9zaCBIRAgAAAAEAwAAAFChG3MAAAAIAAAAAAQAAEHFlk7IgAAAJAAAAAEBAABBQUY2QTJFRi01MTg0LTQ1OEItQTM2RC04QzJDMTU5MDBENUMYAAAAAQIAAIEAAAABAAAA7xMAAAEAAAAAAAAAAAAAAAEAAAABAQAALwAAAAAAAAABBQAA/QAAAAECAAAzNjllNzI1YTcyMTkxYmRhYjZlYzMwMzMxZjUyYTQyMjM1OTQ5YTUzZDdlZmNlNmMzYzc0NjUzZGFjZWIyODNkOzAwOzAwMDAwMDAwOzAwMDAwMDAwOzAwMDAwMDAwOzAwMDAwMDAwMDAwMDAwMjA7Y29tLmFwcGxlLmFwcC1zYW5kYm94LnJlYWQtd3JpdGU7MDE7MDEwMDAwMTI7MDAwMDAwMDAwOTZkODFlNzswMTsvdXNlcnMvZXJpa3Byb3Blci9uZXh0Y2xvdWQvbGlicmFyeS9iaWJ0ZXgvZmlsZXMvZXAtMjAyNC0wNC0wMy0yMi0wNy0zMS5wZGYAAAAAzAAAAP7///8BAAAAAAAAABAAAAAEEAAAkAAAAAAAAAAFEAAAJAEAAAAAAAAQEAAAWAEAAAAAAABAEAAASAEAAAAAAAACIAAAJAIAAAAAAAAFIAAAlAEAAAAAAAAQIAAApAEAAAAAAAARIAAA2AEAAAAAAAASIAAAuAEAAAAAAAATIAAAyAEAAAAAAAAgIAAABAIAAAAAAAAwIAAAMAIAAAAAAAABwAAAeAEAAAAAAAARwAAAFAAAAAAAAAASwAAAiAEAAAAAAACA8AAAOAIAAAAAAAAACAANABoAIwBGAAAAAAAAAgEAAAAAAAAABQAAAAAAAAAAAAAAAAAABI4="
	data, _ := base64.StdEncoding.DecodeString(Test)
	Str := string(data)
	Start := Str[strings.Index(Str, "relativePathXbookmark")+len("relativePathXbookmark")+3 : strings.Index(Str, "DbookD")-3]
	fmt.Printf("%q\n", Start)

	// log import ...
	//
	//	log.Fatal(err)
}

//  { address
//    author
//    booktitle
//    chapter
//    doi
//    edition
//    editor
//    howpublished
//    institution
//    isbn
//    issn
//    journal
//    eprinttype
//    eprint??
//    key
//    month
//    note
//    number
//    occasion
//    organization
//    pages
//    publisher
//    school
//    series
//    title
//    type
//    url
//    volume
//    year
//
