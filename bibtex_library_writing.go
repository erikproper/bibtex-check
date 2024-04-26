//
// Module: bibtex_library_writing
//
// This module is adds the functionality (for TBibTeXLibrary) to write out BibTeX files
//
// Creator: Henderik A. Proper (erikproper@fastmail.com)
//
// Version of: 24.04.2024
//

package main

import (
	"bufio"
	"os"
)

// Does what its name says ... writing the library to the provided BibTeX file
// Notes:
// - As we ignore preambles, these are not written.
// - When we start managing the groups (of keys) the way Bibdesk does, we need to ensure that their embedded as an XML structure embedded in a comment, is updated.
func (l *TBibTeXLibrary) WriteBibTeXFile(fileName string) bool {
	BackupFile(fileName)

	bibFile, err := os.Create(fileName)
	if err != nil {
		return false
	}
	defer bibFile.Close()

	bibWriter := bufio.NewWriter(bibFile)

	// Write out the entries and their fields
	for key, fields := range l.entryFields {
		bibWriter.WriteString("@" + l.entryType[key] + "{" + key + ",\n")
		for field, value := range fields {
			bibWriter.WriteString("   " + field + " = {" + value + "},\n")
		}
		bibWriter.WriteString("}\n")
		bibWriter.WriteString("\n")
	}

	// Write out the comments
	for _, comment := range l.comments {
		bibWriter.WriteString("@" + CommentEntryType + "{" + comment + "}\n")
		bibWriter.WriteString("\n")
	}

	bibWriter.Flush()

	return true
}
