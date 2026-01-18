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
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// Start of connecting things to sqlite
// Should, of course, in the end go into its own module/Table
//
// Upon any "write" operation, we should copy the existing database Table to a backup location.
// Only once, of course ...
// After a write operation, we also need to set the "dirty flag" for the table.
// The "reading" operations should only read from the Table, when it is newer (or when the database is empty)
// Writing a Table only needs to be done when there are changes.
const (
	sqliteDatabaseDriver = "sqlite3"
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

func connectToDatabase() {
	dbName := bibTeXFolder + bibTeXBaseName + sqliteFileExtension

	var err error

	db, err = sql.Open(sqliteDatabaseDriver, dbName)

	if err != nil {
		dbInteraction.Progress("Could not open sqlite database %s: %s", dbName, err.Error())
	}

	ensureTableDatesTableExists()
	ensureNameMappingsTableExists()
}

/*
 * Table dates table
 */

func ensureTableDatesTableExists() {
	create := `
		  CREATE TABLE IF NOT EXISTS table_modification_times (
		    table_name TEXT PRIMARY KEY,
		    modification_time INT NOT NULL
		  );
		`

	tryCreateTableIfNeeded(create)
}

func setTableDate(tableName string, date int64) {
	update := `
		INSERT INTO table_modification_times (table_name, modification_time) 
		  VALUES (?, ?) 
		  ON CONFLICT(table_name) 
		    DO UPDATE SET modification_time = excluded.modification_time;
		`

	_, err := db.Exec(update, tableName, date)
	if err != nil {
		dbInteraction.Error("Adding table modification date failed %s:", err)
	}
}

func tableModTime(tableName string) int64 {
	query := `SELECT table_name, modification_time FROM table_modification_times WHERE table_name = ?`

	row := db.QueryRow(query, tableName)

	var date int64
	err := row.Scan(&tableName, &date)
	if err != nil {
		date = 0
	}

	return date
}

/*
 * Name mappings table
 */

var (
	nameMappingsTableIsUpdated = false
	nameMappingsFileWriting    = true
)

func ensureNameMappingsTableExists() {
	create := `
		  CREATE TABLE IF NOT EXISTS name_mappings (
            alias TEXT PRIMARY KEY,  
            name TEXT NOT NULL
		  );
		`

	tryCreateTableIfNeeded(create)
}

func maybeReadNameMappingsFile() {
	nameMappingsFileName := bibTeXFolder + bibTeXBaseName + nameMappingsFileExtension

	if fileModTime(nameMappingsFileName) > tableModTime("name_mappings") {
		delete := `DROP TABLE IF EXISTS name_mappings;`
		_, err := db.Exec(delete)
		if err != nil {
			dbInteraction.Warning("Could not drop name_mappings table: %s", err)
		}

		ensureNameMappingsTableExists()

		dbInteraction.Progress("Reading name mappings file %s into database", nameMappingsFileName)

		query := `INSERT INTO name_mappings (alias, name) VALUES (?, ?) ON CONFLICT(alias) DO UPDATE SET name = excluded.name;`

		processFile(nameMappingsFileName, func(line string) {
			elements := strings.Split(line, "\t")
			if len(elements) < 2 {
				dbInteraction.Warning(WarningNameMappingsLineTooShort, line)
				nameMappingsFileWriting = false
				return
			}

			_, err := db.Exec(query, ApplyLaTeXMap(elements[1]), ApplyLaTeXMap(elements[0]))
			if err != nil {
				dbInteraction.Warning("Name mapping insertion failed", err)
			}

			// Update the cache!
		})

	}

	setTableDate("name_mappings", fileModTime(nameMappingsFileName))

	nameMappingsTableIsUpdated = true
}

// Now read from the list, using, and updating the in memory version as needed

// And then write back the name mappings file if needed, and allowed!
