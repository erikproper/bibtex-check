/*
 *
 * Module: bibtex_library_db
 *
 * This module is concerned with sqlite specific operations
 *
 * Creator: Henderik A. Proper (erikproper@fastmail.com)
 *
 * Version of: 15.01.2026
 *
 */

package main

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

// Start of connecting things to sqlite
// Should, of course, in the end go into its own module/file
//
// Upon any "write" operation, we should copy the existing database file to a backup location.
// Only once, of course ...
// After a write operation, we also need to set the "dirty flag" for the table.
// The "reading" operations should only read from the file, when it is newer (or when the database is empty)
// Writing a file only needs to be done when there are changes.
const (
	sqliteDatabaseDriver = "sqlite3"
	sqliteFileExtension  = ".sqlite3"
)

var (
	dbInteraction TInteraction
	db            *sql.DB
)

func tryCreateTableIfNeeded(command string) {
	_, err := db.Exec(command)
	if err != nil {
		dbInteraction.Error("Could not ensure existance of table: %s", err)
		// BAIL!
	}
}

func connectToDatabase(filesRoot, baseName string) {
	DatabaseName := filesRoot + baseName + sqliteFileExtension

	var err error

	db, err = sql.Open(sqliteDatabaseDriver, DatabaseName)

	if err != nil {
		dbInteraction.Progress("Could not open sqlite database %s: %s", DatabaseName, err.Error())
	}

	ensureFileDatesTable()
	ensureNameAliasesTable()
}

/*
 * File dates table
 */

func ensureFileDatesTable() {
	create := `
		  CREATE TABLE IF NOT EXISTS file_dates (
		    file_name TEXT PRIMARY KEY,
		    mod_date TEXT NOT NULL
		  );
		`

	tryCreateTableIfNeeded(create)
}

func setFileDate(fileName, date string) {
	update := `
		INSERT INTO file_dates (file_name, mod_date) 
		  VALUES (?, ?) 
		  ON CONFLICT(file_name) 
		    DO UPDATE SET mod_date = excluded.mod_date;
		`

	_, err := db.Exec(update, fileName, date)
	if err != nil {
		dbInteraction.Error("Adding file date failed %s:", err)
	}
}

func getFileDate(fileName string) string {
	query := `SELECT file_name, mod_date FROM file_dates WHERE file_name = ?`

	row := db.QueryRow(query, fileName)

	var date string
	err := row.Scan(&fileName, &date)
	if err != nil {
		date = ""
	}

	return date
}

/*
 * Name aliases table
 */

func ensureNameAliasesTable() {
	create := `
		  CREATE TABLE IF NOT EXISTS name_aliases (
            alias TEXT PRIMARY KEY,  
            name TEXT NOT NULL
		  );
		`

	tryCreateTableIfNeeded(create)
}

//func (l *TBibTeXLibrary) ensureNameAliasesTable() {
//	ModTime(FieldsCacheFile)
//}

// func (l *TBibTeXLibrary) ensureNameAliasesTableIsUpToDate() {
//}
// Do an on-demand read when needed. So, when a lookup is needed, then check if an update check has been done
