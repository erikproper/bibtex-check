package main

import (
	"fmt"
	"strconv"
)

//import "io"

/// Enable logging/error reporting
/// Error messages for parser and "ForcedXXXX"
/// Check Forced vs non-forced ... do we always need both?
/// Then create a {Tag,Entry}Admin using byte, with (1) defaults (+ named/constant identifiers) based on the pre-defined types, and (2) allow for aliases
/// Split files further
/// Add comments ... also to the already splitted files.
/// Make things robust and reporting when file is not found

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

var BiBTeXParser TBiBTeXStream
var reporting TReporting
var count int

func main() {
	Reporting := TReporting{}
	BiBTeXParser.NewBiBTeXParser(Reporting)

	BiBTeXParser.ParseBiBFile("Test.bib")

	fmt.Printf("Count: " + strconv.Itoa(count) + "\n")

	//			fmt.Print("[" + runeToString(runes[i]) + "]")

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
