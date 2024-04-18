package main

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// config.go

// Test AllowedXX on entries and fields


// Comments list
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

/// This should actually be read/seeded from a config file
/// Create a config.go file for now. Later we can see what needs to be read or not.
///
/// for each field used in entry:
///   if unknown then
///      if exists new_field = FieldAliasesMap[field] then
///         if new_field already has value then
///            ask
///         else
///            rename
///         fi
///      else
///         Add to UnknownFields list for current stream
///      fi
///
///   if resulting field is AllowedFields, but not in AllowedFields list for this type
///      then warning
///
/// for each field in MandatoryTypeFields[type]
///   if missing then warning
///
/// Per stream parse round:
///   for each UnknownField report
///
///

/// First App

/// KInd[field] = Which kind of cleaning/nornalisation needed
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

	for t, _ := range Library.usedFields {
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
