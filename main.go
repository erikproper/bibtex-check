package main

import (
	"crypto/md5"
	"fmt"
	"io"
)

//import "io"

/// @string{a1998={1998}} ... when only numbers, then illegal tag name
/// string names must start with letter.
///
/// b.library.NewLibraryEntry(key)

/// TCharSet via anonymous struct?

/// Missing/from cleanup (no Missing; fromXXX -> XXXClass)
/// Pattern:
///   b.MaybeReportError(errorMissingTagName) ||
///   b.SkipToNextEntry(fromTagName)
/// via function

/// Print library (content + tags)
/// pre-defined strings ... jan/feb/etc

/// Test
/// Split off library file
/// Add comments + inspect ... also to the already splitted files.
/// Make things robust and reporting when file is not found
/// Save library + comments(!!)

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
	WarningNoSelectedEntry    = "No entry selected. Can't really happen .."
)

type (
	TBiBTeXLibrary struct {
		entries       map[string]TBiBTeXEntry
		SelectedEntry *TBiBTeXEntry
		UsedTags      TStringSet
		warnOnDoubles bool
		reporting     TReporting // Error reporting channel
	}

	TBiBTeXEntry struct {
		Tags TStringMap
	}
)

func (l *TBiBTeXLibrary) NewLibrary(reporting TReporting, warnOnDoubles bool) {
	l.entries = map[string]TBiBTeXEntry{}
	l.UsedTags = TStringSet{}
	l.SelectedEntry = nil
	l.reporting = reporting
	l.warnOnDoubles = warnOnDoubles
}

func (l *TBiBTeXLibrary) maybeNewLibraryEntry(key string, warnOnDoubles bool) bool {
	entry, exists := l.entries[key]
	if exists {
		if warnOnDoubles {
			l.reporting.Warning(WarningEntryAlreadyExists, key)

			return true
		}
	} else {
		entry.NewBiBTeXEntry()
		l.entries[key] = entry
	}

	l.SelectedEntry = &entry

	return true
}

func (l *TBiBTeXLibrary) NewLibraryEntry(key string) bool {
	return l.maybeNewLibraryEntry(key, l.warnOnDoubles)
}

func (l *TBiBTeXLibrary) EnforceEntrySelection() bool {
	if l.SelectedEntry == nil {
		l.reporting.Warning(WarningNoSelectedEntry)

		l.maybeNewLibraryEntry("", false)
	}

	return true
}

/////

func (e *TBiBTeXEntry) NewBiBTeXEntry() {
	e.Tags = TStringMap{}
}

var BiBTeXParser TBiBTeXStream
var Library TBiBTeXLibrary
var Reporting TReporting

func main() {
	Reporting.NewReporting()
	Library.NewLibrary(Reporting, true)
	BiBTeXParser.NewBiBTeXParser(Reporting, Library)
	BiBTeXParser.ParseBiBFile("Test.bib")

	fmt.Println(Library)

	h := md5.New()
	io.WriteString(h, "zot:IJ6KKKAQ\n")
	fmt.Printf("%x\n", h.Sum(nil))

	// log import ...
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
