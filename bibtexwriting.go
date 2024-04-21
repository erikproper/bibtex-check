package main

import (
	"bufio"
	"os"
)

func (l *TBiBTeXLibrary) WriteBiBTeXFile(fileName string) bool {
	BackupFile(fileName)

	bibFile, err := os.Create(fileName)
	if err != nil {
		return false
	}
	defer bibFile.Close()

	bibWriter := bufio.NewWriter(bibFile)

	for key, fields := range l.entryFields {
		bibWriter.WriteString("@" + l.entryType[key] + "{" + key + ",\n")
		for field, value := range fields {
			bibWriter.WriteString("   " + field + " = {" + value + "},\n")
		}
		bibWriter.WriteString("}\n")
		bibWriter.WriteString("\n")
	}

	for _, comment := range l.comments {
		bibWriter.WriteString("@" + CommentEntryType + "{" + comment + "}\n")
		bibWriter.WriteString("\n")
	}

	bibWriter.Flush()

	return true
}
