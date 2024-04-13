package main

import (
	"fmt"
	"strconv"
)

//import "io"

/// Then create a {Tag,Entry}Admin using byte, with (1) defaults (+ named/constant identifiers) based on the pre-defined types, and (2) allow for aliases
/// Add comments + inspect ... also to the already splitted files.
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
