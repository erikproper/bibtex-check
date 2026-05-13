/*
 *
 * Module: bibtex_library_writing
 *
 * This module is adds the functionality (for TBibTeXLibrary) to write out BibTeX and associated files
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 24.04.2024
 *
 */

package main

// Read bib files
func (l *TBibTeXLibrary) ReadBib(filePath string) bool {
	FullFilePath := l.FilesRoot + l.BaseName + BibFileExtension
	l.harvestNameAliases = true
	defer func() { l.harvestNameAliases = false }()
	return l.ParseBibFile(FullFilePath)
}

// Generic function to read library related files
func (l *TBibTeXLibrary) readFile(fullFilePath, message string, reading func(string)) bool {
	if message != "" {
		l.Progress(message, fullFilePath)
	}

	return processFile(fullFilePath, reading)
}

func (l *TBibTeXLibrary) readDBLPKeyFile(DBLPKey, fileName string, reading func(string)) bool {
	return l.readFile(l.FilesRoot+"DBLPScraper/bib/"+DBLPKey+"/"+fileName, "", reading)
}

func (l *TBibTeXLibrary) ForEachChildOfDBLPKey(DBLPKey string, work func(string)) {
	l.readDBLPKeyFile(DBLPKey, "children", work)
}

func (l *TBibTeXLibrary) MaybeGetDBLPCrossref(DBLPKey string) string {
	crossrefDBLPKey := ""

	l.readDBLPKeyFile(DBLPKey, "crossref", func(key string) {
		if key != "" {
			crossrefDBLPKey = key
		}
	})

	return crossrefDBLPKey
}

// Generic function to read library related files
func (l *TBibTeXLibrary) readLibraryFile(fileExtension, message string, reading func(string)) bool {
	return l.readFile(l.FilesRoot+l.BaseName+fileExtension, message, reading)
}

func (l *TBibTeXLibrary) ReadCrossFieldMappingsFile() {
	maybeReloadCrossFieldMappingsDb()
	if !crossFieldMappingsFileWritingAllowed {
		l.NoCrossFieldMappingsFileWriting = true
	}
	loadCrossFieldMappingsFromDb(l)
	l.crossFieldMappingsModified = false
}

// Read key field challenge file
func (l *TBibTeXLibrary) ReadEntryFieldMappingsFile() {
	maybeReloadEntryFieldMappingsDb()
	if !entryFieldMappingsFileWritingAllowed {
		l.NoEntryFieldMappingsFileWriting = true
	}
	loadEntryFieldMappingsFromDb(l)
	l.entryFieldMappingsModified = false
}

// Read generic field challenge file
func (l *TBibTeXLibrary) ReadGenericFieldMappingsFile() {
	maybeReloadGenericFieldMappingsDb()
	if !genericFieldMappingsFileWritingAllowed {
		l.NoGenericFieldMappingsFileWriting = true
	}
	loadGenericFieldMappingsFromDb(l)
	l.genericFieldMappingsModified = false
}

func (l *TBibTeXLibrary) ReadKeyOldiesFile() {
	maybeReloadKeyOldiesDb()
	if !keyOldiesFileWritingAllowed {
		l.NoKeyOldiesFileWriting = true
	}
	loadKeyOldiesFromDb(l)
	l.keyOldiesModified = false
}

func (l *TBibTeXLibrary) ReadKeyHintsFile() {
	maybeReloadKeyHintsDb()
	if !keyHintsFileWritingAllowed {
		l.NoKeyHintsFileWriting = true
	}
	loadKeyHintsFromDb(l)
	l.keyHintsModified = false
}

func (l *TBibTeXLibrary) ReadKeyNonDoublesFile() {
	maybeReloadKeyNonDoublesDb()
	if !keyNonDoublesFileWritingAllowed {
		l.NoKeyNonDoublesFileWriting = true
	}
	loadKeyNonDoublesFromDb(l)
	l.keyNonDoublesModified = false
}

func (l *TBibTeXLibrary) ReadURLsIgnoreFile() {
	maybeReloadURLsIgnoreDb()
	loadURLsIgnoreFromDb(l)
}

func (l *TBibTeXLibrary) ReadPDFConfirmedOkFile() {
	maybeReloadPDFConfirmedOkDb()
	if !pdfConfirmedOkFileWritingAllowed {
		l.NoPDFConfirmedOkFileWriting = true
	}
	loadPDFConfirmedOkFromDb(l)
	l.pdfConfirmedOkModified = false
}

func (l *TBibTeXLibrary) ReadNameMappingsFile() {
	maybeReloadNameMappingsDb()
	if !nameMappingsFileWritingAllowed {
		l.NoNameMappingsFileWriting = true
	}
	loadNameMappingsFromDb(l)
	l.nameMappingsModified = false
}
