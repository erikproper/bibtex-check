package main

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

/// Comments list

/// Export library

/// Split off library.go file
/// Add comments + inspect ... also to the already splitted files.
/// Make things robust and reporting when file is not found
/// Save library + comments(!!)

/// Create an "AllowedTags" set
/// Dito an "AllowedTypes"
/// Check the consistency of the Maps to these allowed sets ...
///
/// If a Tag is not in this, set then try and apply the mapping
/// As such, the BiBTeXTagNameMap should be used by adding an order in which we try to apply.
/// If it leads to a possible synonym, then delete + warn about the unknown tag.
/// If it (after mapping) exists, we need user input ...
/// By the way, do check the consistency
/// But only try to do so after having created the full entry
///
/// Deal with _XXX fields after entry is finished.
/// Same with xXXX fields
/// publisheD
/// Use a MaybeMapTags map with potential mappings
/// Should have an order though ....
///
///
/// Should be the start of a CleanEntry function.
/// Should be called from the bibtexstream side.
/// This function should then also report the actually usedTags ...

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

// Check usage of "New"

const (
	WarningEntryAlreadyExists = "Entry '%s' already exists"
)

type (
	TBiBTeXLibrary struct {
		entries       map[string]TBiBTeXEntry
		newKey        string
		newEntry      TBiBTeXEntry
		usedTags      TStringSet
		warnOnDoubles bool
		TReporting    // Error reporting channel
	}

	TBiBTeXEntry struct {
		tags TStringMap
	}
)

func (l *TBiBTeXLibrary) NewLibrary(reporting TReporting, warnOnDoubles bool) {
	l.entries = map[string]TBiBTeXEntry{}
	l.usedTags = TStringSet{}
	l.newEntry = TBiBTeXEntry{}
	l.newKey = ""
	l.TReporting = reporting
	l.warnOnDoubles = warnOnDoubles
}

func (l *TBiBTeXLibrary) StartNewLibraryEntry(key string) bool {
	l.newKey = key
	l.newEntry = TBiBTeXEntry{}
	l.newEntry.tags = TStringMap{}

	return true
}

func (l *TBiBTeXLibrary) AssignTag(tag, value string) bool {
	l.newEntry.tags[tag] = value
	l.usedTags[tag] = true

	return true
}

func (l *TBiBTeXLibrary) FinishNewLibraryEntry() bool {
	_, exists := l.entries[l.newKey]

	if exists && l.warnOnDoubles {
		l.Warning(WarningEntryAlreadyExists, l.newKey)

		return true
	}

	l.entries[l.newKey] = l.newEntry

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
