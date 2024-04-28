package main

import (
	"encoding/base64"
	"os"
	//	"fmt"
	"strings"
)

type TFieldProcessors = map[string]func(*TBibTeXLibrary, string) string

const (
	WarningMissingFile   = "File %s for key %s seems not to exist"
	WarningBadISBN       = "Found wrong ISBN \"%s\" for key %s"
	WarningBadISSN       = "Found wrong ISSN \"%s\" for key %s"
	WarningBadISBNRepair = "Found wrong ISBN \"%s\" for key %s, will repair to \"%s\""
	WarningBadISSNRepair = "Found wrong ISBN \"%s\" for key %s, will repair to \"%s\""
)

var fieldProcessors TFieldProcessors

func processISSNValue(library *TBibTeXLibrary, value string) string {
	lastIndex := len(value)
	digits := 0
	for index, character := range value {
		if ('0' <= character && character <= '9') || character == 'X' {
			digits++
		} else if character != '-' {
			lastIndex = index - 1
			break
		}
		lastIndex = index
	}

	ISSN := value

	if digits != 8 {
		library.Warning(WarningBadISSN, value, library.currentKey)
	} else if lastIndex+1 < len(value) {
		ISSN := value[0 : lastIndex+1]
		library.Warning(WarningBadISSNRepair, value, library.currentKey, ISSN)
	}

	if len(ISSN) < 9 {
		ISSN = ISSN[0:4] + "-" + ISSN[4:8]
	}

	return ISSN
}

func processISBNValue(library *TBibTeXLibrary, value string) string {
	lastIndex := len(value)
	digits := 0
	for index, character := range value {
		if ('0' <= character && character <= '9') || character == 'X' {
			digits++
		} else if character != '-' {
			lastIndex = index - 1
			break
		}
		lastIndex = index
	}

	if digits != 10 && digits != 13 {
		library.Warning(WarningBadISBN, value, library.currentKey)

		return value
	} else if lastIndex+1 < len(value) {
		repairedValue := value[0 : lastIndex+1]
		library.Warning(WarningBadISBNRepair, value, library.currentKey, repairedValue)

		return repairedValue
	}

	return value
}

func processDBLPValue(library *TBibTeXLibrary, value string) string {
	if !library.legacyMode {
		library.AddKeyAlias("DBLP:"+value, library.currentKey)
	}

	return value
}

func processBDSKFileValue(library *TBibTeXLibrary, value string) string {
	data, _ := base64.StdEncoding.DecodeString(value)
	payload := string(data)

	fileNameStart := strings.Index(payload, "relativePathXbookmark") + len("relativePathXbookmark") + 3
	fileNameEnd := strings.Index(payload, ".pdf") + 4
	fileName := payload

	if 0 <= fileNameStart && fileNameStart < fileNameEnd && fileNameEnd <= len(payload) {
		fileName = payload[fileNameStart:fileNameEnd]
	}

	_, err := os.Stat(library.files + fileName)
	if err != nil {
		library.Warning(WarningMissingFile, fileName, library.currentKey)
	}

	return value
}

func (l *TBibTeXLibrary) NormaliseFieldValue(field, value string) string {
	valueNormaliser, hasNormaliser := fieldProcessors[field]

	if hasNormaliser {
		return valueNormaliser(l, value)
	} else {
		return strings.TrimSpace(value)
	}
}

func init() {
	fieldProcessors = TFieldProcessors{}
	fieldProcessors["bdsk-file-1"] = processBDSKFileValue
	fieldProcessors["bdsk-file-2"] = processBDSKFileValue
	fieldProcessors["bdsk-file-3"] = processBDSKFileValue
	fieldProcessors["bdsk-file-4"] = processBDSKFileValue
	fieldProcessors["bdsk-file-5"] = processBDSKFileValue
	fieldProcessors["bdsk-file-6"] = processBDSKFileValue
	fieldProcessors["bdsk-file-7"] = processBDSKFileValue
	fieldProcessors["bdsk-file-8"] = processBDSKFileValue
	fieldProcessors["bdsk-file-9"] = processBDSKFileValue
	fieldProcessors["dblp"] = processDBLPValue
	fieldProcessors["isbn"] = processISBNValue
	fieldProcessors["issn"] = processISSNValue
}
