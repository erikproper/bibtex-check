/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: LibraryDB
 *
 * SQLite persistence layer for the BibTeX library. Each logical data source
 * (name mappings, key hints, …) follows the same three-function pattern:
 *   maybeReload<X>Db()      — sync flat file → DB when file is newer
 *   load<X>FromDb(l)        — populate in-memory library fields from DB
 *   save<X>ToDb(l)          — write in-memory library fields back to DB
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 04.05.2026
 *
 */

package main

import (
	"database/sql"
	"runtime"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	sqliteDatabaseDriver = "sqlite3"
)

var (
	dbInteraction      TInteraction
	db                 *sql.DB
	entryCache         map[string]*TBibTeXEntry
	entrySnapshots     map[string]map[string]string
	bibEntriesModified bool
)

// --- entry cache ---

// initEntryCache loads the entire bib_entries table into memory when there is
// sufficient available RAM (estimated headroom > 2× the projected cache size).
// If the machine is too constrained, entryCache stays nil and callers fall back
// to per-query DB reads.
func initEntryCache() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	count := int64(countBibEntries())
	const bytesPerEntry = 2048
	estimatedBytes := count * bytesPerEntry
	totalRAM := int64(systemTotalRAM())
	if totalRAM > 0 && estimatedBytes > (totalRAM-int64(ms.Sys))/2 {
		return
	}

	rows, err := db.Query(`SELECT entry_key, field, value FROM bib_entries`)
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries for cache: %s", err)
		return
	}
	defer rows.Close()

	cache := map[string]*TBibTeXEntry{}
	for rows.Next() {
		var key, field, value string
		if err := rows.Scan(&key, &field, &value); err != nil {
			dbInteraction.Warning("Could not scan bib_entries for cache: %s", err)
			return
		}
		e, ok := cache[key]
		if !ok {
			e = &TBibTeXEntry{Key: key, Fields: map[string]string{}}
			cache[key] = e
		}
		e.Fields[field] = value
	}
	entryCache = cache
}

// --- database connection ---

func connectToDatabase() {
	dbName := bibTeXFolder + bibTeXBaseName + sqliteFileExtension

	var err error
	db, err = sql.Open(sqliteDatabaseDriver, dbName)
	if err != nil {
		dbInteraction.Progress("Could not open sqlite database %s: %s", dbName, err.Error())
	}

	ensureTableDatesTableExists()
	ensureNameMappingsTableExists()
	ensureKeyHintsTableExists()
	ensureKeyOldiesTableExists()
	ensureKeyNonDoublesTableExists()
	ensureCrossFieldMappingsTableExists()
	ensureEntryFieldMappingsTableExists()
	ensureGenericFieldMappingsTableExists()
	ensureBibTablesExist()
}

// --- table_modification_times table ---

func ensureTableDatesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS table_modification_times (
		  table_name        TEXT PRIMARY KEY,
		  modification_time INT  NOT NULL
		);`)
}

func setTableDate(tableName string, date int64) {
	_, err := db.Exec(`
		INSERT INTO table_modification_times (table_name, modification_time)
		  VALUES (?, ?)
		  ON CONFLICT(table_name)
		    DO UPDATE SET modification_time = excluded.modification_time;`,
		tableName, date)
	if err != nil {
		dbInteraction.Error("Updating table modification date failed: %s", err)
	}
}

func tableModTime(tableName string) int64 {
	row := db.QueryRow(
		`SELECT modification_time FROM table_modification_times WHERE table_name = ?`,
		tableName)

	var date int64
	if err := row.Scan(&date); err != nil {
		date = 0
	}
	return date
}

func tryCreateTableIfNeeded(command string) {
	if _, err := db.Exec(command); err != nil {
		dbInteraction.Error("Could not ensure existence of table: %s", err)
	}
}

// --- name_mappings table ---

var nameMappingsFileWritingAllowed = true

func ensureNameMappingsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS name_mappings (
		  alias TEXT PRIMARY KEY,
		  name  TEXT NOT NULL
		);`)
}

// maybeReloadNameMappingsDb syncs the flat file into the DB when the file is newer.
// Each line must have two non-empty fields: canonical name and alias.
func maybeReloadNameMappingsDb() {
	nameMappingsFileName := bibTeXFolder + bibTeXBaseName + NameMappingsFilePath

	if fileModTime(nameMappingsFileName) <= tableModTime("name_mappings") {
		return
	}

	dbInteraction.Progress("Reloading name mappings from %s", nameMappingsFileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS name_mappings;`); err != nil {
		dbInteraction.Warning("Could not drop name_mappings table: %s", err)
	}
	ensureNameMappingsTableExists()

	upsert := `INSERT INTO name_mappings (alias, name) VALUES (?, ?)
	             ON CONFLICT(alias) DO UPDATE SET name = excluded.name;`

	processFile(nameMappingsFileName, func(line string) {
		elements := strings.Split(line, csvDelimiter)
		if len(elements) >= 2 && elements[0] != "" && elements[1] != "" {
			if _, err := db.Exec(upsert, ApplyLaTeXMap(elements[1]), ApplyLaTeXMap(elements[0])); err != nil {
				dbInteraction.Warning("Name mapping insertion failed: %s", err)
			}
		} else {
			dbInteraction.Warning(WarningNameMappingsLineTooShort, line)
			nameMappingsFileWritingAllowed = false
		}
	})

	setTableDate("name_mappings", fileModTime(nameMappingsFileName))
}

// loadNameMappingsFromDb populates l.NameAliasToName and l.NameToAliases from
// the DB, then derives additional aliases via FindAliases for each stored pair
// and for each unique canonical.
func loadNameMappingsFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT alias, name FROM name_mappings`)
	if err != nil {
		dbInteraction.Warning("Could not query name_mappings: %s", err)
		return
	}
	defer rows.Close()

	type pair struct{ alias, name string }
	var pairs []pair
	for rows.Next() {
		var alias, name string
		if err := rows.Scan(&alias, &name); err != nil {
			dbInteraction.Warning("Could not scan name_mappings row: %s", err)
			continue
		}
		pairs = append(pairs, pair{alias, name})
	}

	for _, p := range pairs {
		l.AddAlias(p.alias, p.name, &l.NameAliasToName, &l.NameToAliases, true)
	}

	for _, p := range pairs {
		l.FindAliases(p.name, p.alias)
	}

	canonicals := map[string]bool{}
	for _, p := range pairs {
		canonicals[p.name] = true
	}
	for canonical := range canonicals {
		l.FindAliases(canonical, canonical)
	}
}

// saveNameMappingsToDb upserts all non-self alias pairs into the DB and
// refreshes the table modification time to match the (just-written) flat file.
func saveNameMappingsToDb(l *TBibTeXLibrary) {
	upsert := `INSERT INTO name_mappings (alias, name) VALUES (?, ?)
	             ON CONFLICT(alias) DO UPDATE SET name = excluded.name;`
	for alias, name := range l.NameAliasToName {
		if alias != name {
			if _, err := db.Exec(upsert, alias, name); err != nil {
				dbInteraction.Warning("Name mapping upsert failed: %s", err)
			}
		}
	}

	nameMappingsFileName := bibTeXFolder + bibTeXBaseName + NameMappingsFilePath
	setTableDate("name_mappings", fileModTime(nameMappingsFileName))
}

// --- key_hints table ---

var keyHintsFileWritingAllowed = true

func ensureKeyHintsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS key_hints (
		  hint TEXT PRIMARY KEY,
		  key  TEXT NOT NULL
		);`)
}

// maybeReloadKeyHintsDb syncs the flat file into the DB when the file is newer.
func maybeReloadKeyHintsDb() {
	keyHintsFileName := bibTeXFolder + bibTeXBaseName + KeyHintsFilePath

	if fileModTime(keyHintsFileName) <= tableModTime("key_hints") {
		return
	}

	dbInteraction.Progress("Reloading key hints from %s", keyHintsFileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS key_hints;`); err != nil {
		dbInteraction.Warning("Could not drop key_hints table: %s", err)
	}
	ensureKeyHintsTableExists()

	upsert := `INSERT INTO key_hints (hint, key) VALUES (?, ?)
	             ON CONFLICT(hint) DO UPDATE SET key = excluded.key;`

	processFile(keyHintsFileName, func(line string) {
		elements := strings.Split(line, csvDelimiter)
		if len(elements) < 2 {
			dbInteraction.Warning(WarningKeyHintsLineTooShort, line)
			keyHintsFileWritingAllowed = false
			return
		}
		// File format: key TAB hint  (elements[0]=key, elements[1]=hint)
		if _, err := db.Exec(upsert, elements[1], elements[0]); err != nil {
			dbInteraction.Warning("Key hint insertion failed: %s", err)
		}
	})

	setTableDate("key_hints", fileModTime(keyHintsFileName))
}

// loadKeyHintsFromDb populates l.HintToKey from the DB.
func loadKeyHintsFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT hint, key FROM key_hints`)
	if err != nil {
		dbInteraction.Warning("Could not query key_hints: %s", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var hint, key string
		if err := rows.Scan(&hint, &key); err != nil {
			dbInteraction.Warning("Could not scan key_hints row: %s", err)
			continue
		}
		l.AddKeyHint(hint, key)
	}
}

// syncKeyHintsDbFromFile forces the DB to be reloaded from the flat file.
// Called after WriteKeyHintsFile so the DB mirrors the filtered flat file content.
func syncKeyHintsDbFromFile() {
	setTableDate("key_hints", 0)
	maybeReloadKeyHintsDb()
}

// saveKeyHintsToDb writes the filtered key hints directly to the DB without a file roundtrip.
func saveKeyHintsToDb(l *TBibTeXLibrary) {
	if _, err := db.Exec(`DELETE FROM key_hints;`); err != nil {
		dbInteraction.Warning("Could not clear key_hints: %s", err)
	}
	upsert := `INSERT INTO key_hints (hint, key) VALUES (?, ?)
	             ON CONFLICT(hint) DO UPDATE SET key = excluded.key;`
	for source, target := range l.HintToKey {
		target = l.MapEntryKey(target)
		if bibEntryExists(target) &&
			source != target &&
			source != KeyForDBLP(l.EntryFieldValueity(target, DBLPField)) {
			if _, err := db.Exec(upsert, source, target); err != nil {
				dbInteraction.Warning("key_hints insert failed: %s", err)
			}
		}
	}
	setTableDate("key_hints", fileModTime(bibTeXFolder+bibTeXBaseName+KeyHintsFilePath))
}

// --- key_oldies table ---

var keyOldiesFileWritingAllowed = true

func ensureKeyOldiesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS key_oldies (
		  alias TEXT PRIMARY KEY,
		  key   TEXT NOT NULL
		);`)
}

// maybeReloadKeyOldiesDb syncs the flat file into the DB when the file is newer.
func maybeReloadKeyOldiesDb() {
	keyOldiesFileName := bibTeXFolder + bibTeXBaseName + KeyOldiesFilePath

	if fileModTime(keyOldiesFileName) <= tableModTime("key_oldies") {
		return
	}

	dbInteraction.Progress("Reloading key oldies from %s", keyOldiesFileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS key_oldies;`); err != nil {
		dbInteraction.Warning("Could not drop key_oldies table: %s", err)
	}
	ensureKeyOldiesTableExists()

	upsert := `INSERT INTO key_oldies (alias, key) VALUES (?, ?)
	             ON CONFLICT(alias) DO UPDATE SET key = excluded.key;`

	processFile(keyOldiesFileName, func(line string) {
		elements := strings.Split(line, csvDelimiter)
		if len(elements) < 2 {
			dbInteraction.Warning(WarningKeyAliasesLineTooShort, line)
			keyOldiesFileWritingAllowed = false
			return
		}
		// File format: key TAB alias  (elements[0]=key, elements[1]=alias)
		if _, err := db.Exec(upsert, elements[1], elements[0]); err != nil {
			dbInteraction.Warning("Key oldie insertion failed: %s", err)
		}
	})

	setTableDate("key_oldies", fileModTime(keyOldiesFileName))
}

// loadKeyOldiesFromDb populates l.KeyToKey from the DB.
func loadKeyOldiesFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT alias, key FROM key_oldies`)
	if err != nil {
		dbInteraction.Warning("Could not query key_oldies: %s", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var alias, key string
		if err := rows.Scan(&alias, &key); err != nil {
			dbInteraction.Warning("Could not scan key_oldies row: %s", err)
			continue
		}
		l.AddKeyAlias(alias, key)
	}
}

// syncKeyOldiesDbFromFile forces the DB to be reloaded from the flat file.
// Called after WriteKeyOldiesFile so the DB mirrors the filtered flat file content.
func syncKeyOldiesDbFromFile() {
	setTableDate("key_oldies", 0)
	maybeReloadKeyOldiesDb()
}

// saveKeyOldiesToDb writes the filtered key aliases directly to the DB without a file roundtrip.
func saveKeyOldiesToDb(l *TBibTeXLibrary) {
	if _, err := db.Exec(`DELETE FROM key_oldies;`); err != nil {
		dbInteraction.Warning("Could not clear key_oldies: %s", err)
	}
	upsert := `INSERT INTO key_oldies (alias, key) VALUES (?, ?)
	             ON CONFLICT(alias) DO UPDATE SET key = excluded.key;`
	for source, target := range l.KeyToKey {
		target = l.MapEntryKey(target)
		if bibEntryExists(target) && source != target && IsValidKey(source) {
			if _, err := db.Exec(upsert, source, target); err != nil {
				dbInteraction.Warning("key_oldies insert failed: %s", err)
			}
		}
	}
	setTableDate("key_oldies", fileModTime(bibTeXFolder+bibTeXBaseName+KeyOldiesFilePath))
}

// --- key_non_doubles table ---

var keyNonDoublesFileWritingAllowed = true

func ensureKeyNonDoublesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS key_non_doubles (
		  key1 TEXT NOT NULL,
		  key2 TEXT NOT NULL,
		  PRIMARY KEY (key1, key2)
		);`)
}

// maybeReloadKeyNonDoublesDb syncs the flat file into the DB when the file is newer.
func maybeReloadKeyNonDoublesDb() {
	keyNonDoublesFileName := bibTeXFolder + bibTeXBaseName + KeyNonDoublesFilePath

	if fileModTime(keyNonDoublesFileName) <= tableModTime("key_non_doubles") {
		return
	}

	dbInteraction.Progress("Reloading key non-doubles from %s", keyNonDoublesFileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS key_non_doubles;`); err != nil {
		dbInteraction.Warning("Could not drop key_non_doubles table: %s", err)
	}
	ensureKeyNonDoublesTableExists()

	insert := `INSERT INTO key_non_doubles (key1, key2) VALUES (?, ?)
	             ON CONFLICT(key1, key2) DO NOTHING;`

	processFile(keyNonDoublesFileName, func(line string) {
		elements := strings.Split(line, csvDelimiter)
		if len(elements) < 2 {
			dbInteraction.Warning(WarningNonDoublesLineTooShort, line)
			keyNonDoublesFileWritingAllowed = false
			return
		}
		if _, err := db.Exec(insert, elements[0], elements[1]); err != nil {
			dbInteraction.Warning("Key non-double insertion failed: %s", err)
		}
	})

	setTableDate("key_non_doubles", fileModTime(keyNonDoublesFileName))
}

// loadKeyNonDoublesFromDb populates l.NonDoubles from the DB.
func loadKeyNonDoublesFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT key1, key2 FROM key_non_doubles`)
	if err != nil {
		dbInteraction.Warning("Could not query key_non_doubles: %s", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var key1, key2 string
		if err := rows.Scan(&key1, &key2); err != nil {
			dbInteraction.Warning("Could not scan key_non_doubles row: %s", err)
			continue
		}
		l.AddNonDoubles(key1, key2)
	}
}

// syncKeyNonDoublesDbFromFile forces the DB to be reloaded from the flat file.
// Called after WriteKeyNonDoublesFile so the DB mirrors the filtered flat file content.
func syncKeyNonDoublesDbFromFile() {
	setTableDate("key_non_doubles", 0)
	maybeReloadKeyNonDoublesDb()
}

// saveKeyNonDoublesToDb writes the filtered non-doubles set directly to the DB without a file roundtrip.
func saveKeyNonDoublesToDb(l *TBibTeXLibrary) {
	if _, err := db.Exec(`DELETE FROM key_non_doubles;`); err != nil {
		dbInteraction.Warning("Could not clear key_non_doubles: %s", err)
	}
	insert := `INSERT INTO key_non_doubles (key1, key2) VALUES (?, ?) ON CONFLICT DO NOTHING;`
	for key, set := range l.NonDoubles {
		if key == l.MapEntryKey(key) && l.EntryExists(key) {
			for nonDouble := range set.Elements() {
				if nonDouble != key && nonDouble == l.MapEntryKey(nonDouble) && l.EntryExists(nonDouble) {
					if !l.EvidenceForBeingDifferentEntries(key, nonDouble) {
						if _, err := db.Exec(insert, key, nonDouble); err != nil {
							dbInteraction.Warning("key_non_doubles insert failed: %s", err)
						}
					}
				}
			}
		}
	}
	setTableDate("key_non_doubles", fileModTime(bibTeXFolder+bibTeXBaseName+KeyNonDoublesFilePath))
}

// --- filter_cross_field_mappings table ---

var crossFieldMappingsFileWritingAllowed = true

func ensureCrossFieldMappingsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS filter_cross_field_mappings (
		  source_field TEXT NOT NULL,
		  source_value TEXT NOT NULL,
		  target_field TEXT NOT NULL,
		  target_value TEXT NOT NULL,
		  PRIMARY KEY (source_field, source_value, target_field)
		);`)
}

// maybeReloadCrossFieldMappingsDb syncs the flat file into the DB when the file is newer.
func maybeReloadCrossFieldMappingsDb() {
	fileName := bibTeXFolder + bibTeXBaseName + CrossFieldMappingsFilePath

	if fileModTime(fileName) <= tableModTime("filter_cross_field_mappings") {
		return
	}

	dbInteraction.Progress("Reloading cross-field mappings from %s", fileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS filter_cross_field_mappings;`); err != nil {
		dbInteraction.Warning("Could not drop filter_cross_field_mappings table: %s", err)
	}
	ensureCrossFieldMappingsTableExists()

	upsert := `INSERT INTO filter_cross_field_mappings
	             (source_field, source_value, target_field, target_value) VALUES (?, ?, ?, ?)
	             ON CONFLICT(source_field, source_value, target_field)
	               DO UPDATE SET target_value = excluded.target_value;`

	processFile(fileName, func(line string) {
		elements := strings.Split(line, csvDelimiter)
		if len(elements) < 4 {
			dbInteraction.Warning(WarningFieldMappingsTooShort, line)
			crossFieldMappingsFileWritingAllowed = false
			return
		}
		if _, err := db.Exec(upsert, elements[0], elements[1], elements[2], elements[3]); err != nil {
			dbInteraction.Warning("Cross-field mapping insertion failed: %s", err)
		}
	})

	setTableDate("filter_cross_field_mappings", fileModTime(fileName))
}

// loadCrossFieldMappingsFromDb populates l.FieldMappings from the DB.
// Applies the same normalisation as ReadCrossFieldMappingsFile (MapFieldValue / NormaliseFieldValue).
func loadCrossFieldMappingsFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(
		`SELECT source_field, source_value, target_field, target_value FROM filter_cross_field_mappings`)
	if err != nil {
		dbInteraction.Warning("Could not query filter_cross_field_mappings: %s", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var sourceField, sourceValue, targetField, targetValue string
		if err := rows.Scan(&sourceField, &sourceValue, &targetField, &targetValue); err != nil {
			dbInteraction.Warning("Could not scan filter_cross_field_mappings row: %s", err)
			continue
		}
		l.AddFieldMapping(
			sourceField, l.MapFieldValue(sourceField, sourceValue),
			targetField, l.NormaliseFieldValue(targetField, targetValue))
	}
}

// syncCrossFieldMappingsDbFromFile forces the DB to be reloaded from the flat file.
func syncCrossFieldMappingsDbFromFile() {
	setTableDate("filter_cross_field_mappings", 0)
	maybeReloadCrossFieldMappingsDb()
}

// saveCrossFieldMappingsToDb writes the field mappings directly to the DB without a file roundtrip.
func saveCrossFieldMappingsToDb(l *TBibTeXLibrary) {
	if _, err := db.Exec(`DELETE FROM filter_cross_field_mappings;`); err != nil {
		dbInteraction.Warning("Could not clear filter_cross_field_mappings: %s", err)
	}
	upsert := `INSERT INTO filter_cross_field_mappings
	             (source_field, source_value, target_field, target_value) VALUES (?, ?, ?, ?)
	             ON CONFLICT(source_field, source_value, target_field)
	               DO UPDATE SET target_value = excluded.target_value;`
	for sourceField, sourceFieldMappings := range l.FieldMappings {
		for sourceValue, targetFieldMappings := range sourceFieldMappings {
			for targetField, targetValue := range targetFieldMappings {
				if _, err := db.Exec(upsert, sourceField, sourceValue, targetField, targetValue); err != nil {
					dbInteraction.Warning("filter_cross_field_mappings insert failed: %s", err)
				}
			}
		}
	}
	setTableDate("filter_cross_field_mappings", fileModTime(bibTeXFolder+bibTeXBaseName+CrossFieldMappingsFilePath))
}

// --- filter_entry_field_mappings table ---

var entryFieldMappingsFileWritingAllowed = true

func ensureEntryFieldMappingsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS filter_entry_field_mappings (
		  entry_key  TEXT NOT NULL,
		  field      TEXT NOT NULL,
		  winner     TEXT NOT NULL,
		  challenger TEXT NOT NULL,
		  PRIMARY KEY (entry_key, field, challenger)
		);`)
}

// maybeReloadEntryFieldMappingsDb syncs the flat file into the DB when the file is newer.
func maybeReloadEntryFieldMappingsDb() {
	fileName := bibTeXFolder + bibTeXBaseName + EntryFieldMappingsFilePath

	if fileModTime(fileName) <= tableModTime("filter_entry_field_mappings") {
		return
	}

	dbInteraction.Progress("Reloading entry field aliases from %s", fileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS filter_entry_field_mappings;`); err != nil {
		dbInteraction.Warning("Could not drop filter_entry_field_mappings table: %s", err)
	}
	ensureEntryFieldMappingsTableExists()

	upsert := `INSERT INTO filter_entry_field_mappings
	             (entry_key, field, winner, challenger) VALUES (?, ?, ?, ?)
	             ON CONFLICT(entry_key, field, challenger)
	               DO UPDATE SET winner = excluded.winner;`

	processFile(fileName, func(line string) {
		elements := strings.Split(line, csvDelimiter)
		if len(elements) < 4 {
			dbInteraction.Warning(WarningEntryFieldMappingsLineTooShort, line)
			entryFieldMappingsFileWritingAllowed = false
			return
		}
		if _, err := db.Exec(upsert, elements[0], elements[1], elements[2], elements[3]); err != nil {
			dbInteraction.Warning("Entry field alias insertion failed: %s", err)
		}
	})

	setTableDate("filter_entry_field_mappings", fileModTime(fileName))
}

// loadEntryFieldMappingsFromDb populates l.EntryFieldSourceToTarget from the DB.
// Applies the same normalisation as ReadEntryFieldMappingsFile (MapNormalisedFieldValue).
func loadEntryFieldMappingsFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(
		`SELECT entry_key, field, winner, challenger FROM filter_entry_field_mappings`)
	if err != nil {
		dbInteraction.Warning("Could not query filter_entry_field_mappings: %s", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var key, field, winner, challenger string
		if err := rows.Scan(&key, &field, &winner, &challenger); err != nil {
			dbInteraction.Warning("Could not scan filter_entry_field_mappings row: %s", err)
			continue
		}
		l.AddEntryFieldAlias(key, field,
			l.MapNormalisedFieldValue(field, challenger),
			l.MapNormalisedFieldValue(field, winner),
			true)
	}
}

// syncEntryFieldMappingsDbFromFile forces the DB to be reloaded from the flat file.
func syncEntryFieldMappingsDbFromFile() {
	setTableDate("filter_entry_field_mappings", 0)
	maybeReloadEntryFieldMappingsDb()
}

// saveEntryFieldMappingsToDb writes the filtered entry field aliases directly to the DB without a file roundtrip.
func saveEntryFieldMappingsToDb(l *TBibTeXLibrary) {
	if _, err := db.Exec(`DELETE FROM filter_entry_field_mappings;`); err != nil {
		dbInteraction.Warning("Could not clear filter_entry_field_mappings: %s", err)
	}
	upsert := `INSERT INTO filter_entry_field_mappings
	             (entry_key, field, winner, challenger) VALUES (?, ?, ?, ?)
	             ON CONFLICT(entry_key, field, challenger)
	               DO UPDATE SET winner = excluded.winner;`
	for key, fieldChallenges := range l.EntryFieldSourceToTarget {
		if l.EntryExists(key) {
			for field, challenges := range fieldChallenges {
				if field != PreferredAliasField {
					for challenger, winner := range challenges {
						if l.MapFieldValue(field, challenger) != l.MapEntryFieldValue(key, field, winner) {
							if _, err := db.Exec(upsert, key, field, l.MapEntryFieldValue(key, field, winner), challenger); err != nil {
								dbInteraction.Warning("filter_entry_field_mappings insert failed: %s", err)
							}
						}
					}
				}
			}
		}
	}
	setTableDate("filter_entry_field_mappings", fileModTime(bibTeXFolder+bibTeXBaseName+EntryFieldMappingsFilePath))
}

// --- filter_generic_field_mappings table ---

var genericFieldMappingsFileWritingAllowed = true

func ensureGenericFieldMappingsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS filter_generic_field_mappings (
		  field      TEXT NOT NULL,
		  winner     TEXT NOT NULL,
		  challenger TEXT NOT NULL,
		  PRIMARY KEY (field, challenger)
		);`)
}

// maybeReloadGenericFieldMappingsDb syncs the flat file into the DB when the file is newer.
func maybeReloadGenericFieldMappingsDb() {
	fileName := bibTeXFolder + bibTeXBaseName + GenericFieldMappingsFilePath

	if fileModTime(fileName) <= tableModTime("filter_generic_field_mappings") {
		return
	}

	dbInteraction.Progress("Reloading generic field aliases from %s", fileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS filter_generic_field_mappings;`); err != nil {
		dbInteraction.Warning("Could not drop filter_generic_field_mappings table: %s", err)
	}
	ensureGenericFieldMappingsTableExists()

	upsert := `INSERT INTO filter_generic_field_mappings
	             (field, winner, challenger) VALUES (?, ?, ?)
	             ON CONFLICT(field, challenger)
	               DO UPDATE SET winner = excluded.winner;`

	processFile(fileName, func(line string) {
		elements := strings.Split(line, csvDelimiter)
		if len(elements) < 3 {
			dbInteraction.Warning(WarningGenericFieldMappingsLineTooShort, line)
			genericFieldMappingsFileWritingAllowed = false
			return
		}
		if _, err := db.Exec(upsert, elements[0], elements[1], elements[2]); err != nil {
			dbInteraction.Warning("Generic field alias insertion failed: %s", err)
		}
	})

	setTableDate("filter_generic_field_mappings", fileModTime(fileName))
}

// loadGenericFieldMappingsFromDb populates l.GenericFieldSourceToTarget from the DB.
// Applies the same normalisation as ReadGenericFieldMappingsFile (NormaliseFieldValue).
func loadGenericFieldMappingsFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(
		`SELECT field, winner, challenger FROM filter_generic_field_mappings`)
	if err != nil {
		dbInteraction.Warning("Could not query filter_generic_field_mappings: %s", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var field, winner, challenger string
		if err := rows.Scan(&field, &winner, &challenger); err != nil {
			dbInteraction.Warning("Could not scan filter_generic_field_mappings row: %s", err)
			continue
		}
		l.AddGenericFieldAlias(field,
			l.NormaliseFieldValue(field, challenger),
			l.NormaliseFieldValue(field, winner),
			true)
	}
}

// syncGenericFieldMappingsDbFromFile forces the DB to be reloaded from the flat file.
func syncGenericFieldMappingsDbFromFile() {
	setTableDate("filter_generic_field_mappings", 0)
	maybeReloadGenericFieldMappingsDb()
}

// saveGenericFieldMappingsToDb writes the filtered generic field aliases directly to the DB without a file roundtrip.
func saveGenericFieldMappingsToDb(l *TBibTeXLibrary) {
	if _, err := db.Exec(`DELETE FROM filter_generic_field_mappings;`); err != nil {
		dbInteraction.Warning("Could not clear filter_generic_field_mappings: %s", err)
	}
	upsert := `INSERT INTO filter_generic_field_mappings
	             (field, winner, challenger) VALUES (?, ?, ?)
	             ON CONFLICT(field, challenger)
	               DO UPDATE SET winner = excluded.winner;`
	for field, challenges := range l.GenericFieldSourceToTarget {
		if field != PreferredAliasField {
			for challenger, winner := range challenges {
				if challenger != winner {
					if _, err := db.Exec(upsert, field, l.MapFieldValue(field, winner), challenger); err != nil {
						dbInteraction.Warning("filter_generic_field_mappings insert failed: %s", err)
					}
				}
			}
		}
	}
	setTableDate("filter_generic_field_mappings", fileModTime(bibTeXFolder+bibTeXBaseName+GenericFieldMappingsFilePath))
}

// --- bib_entries / bib_groups / bib_comments tables ---

func ensureBibTablesExist() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS bib_entries (
		  entry_key TEXT NOT NULL,
		  field     TEXT NOT NULL,
		  value     TEXT NOT NULL,
		  PRIMARY KEY (entry_key, field)
		);`)
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS bib_groups (
		  group_name TEXT NOT NULL,
		  entry_key  TEXT NOT NULL,
		  PRIMARY KEY (group_name, entry_key)
		);`)
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS bib_comments (
		  position INTEGER PRIMARY KEY,
		  content  TEXT NOT NULL
		);`)
}

// --- bib entry write primitives ---

// activeTx, when non-nil, is used by bibExec so that parse and check passes can
// batch their writes in a single transaction.
var activeTx *sql.Tx

// bibExec executes a write statement using the active transaction if one is in
// progress, or directly on the DB otherwise.
func bibExec(query string, args ...any) error {
	if activeTx != nil {
		_, err := activeTx.Exec(query, args...)
		return err
	}
	_, err := db.Exec(query, args...)
	return err
}

// bibQuery executes a read query using the active transaction if one is in
// progress, or directly on the DB otherwise.  Callers must close the returned
// Rows.
func bibQuery(query string, args ...any) (*sql.Rows, error) {
	if activeTx != nil {
		return activeTx.Query(query, args...)
	}
	return db.Query(query, args...)
}

// bibQueryRow executes a single-row read using the active transaction if one
// is in progress, or directly on the DB otherwise.
func bibQueryRow(query string, args ...any) *sql.Row {
	if activeTx != nil {
		return activeTx.QueryRow(query, args...)
	}
	return db.QueryRow(query, args...)
}

func beginBibTransaction() {
	var err error
	activeTx, err = db.Begin()
	if err != nil {
		dbInteraction.Error("Could not begin bib transaction: %s", err)
		activeTx = nil
	}
}

func commitBibTransaction() {
	if activeTx != nil {
		if err := activeTx.Commit(); err != nil {
			dbInteraction.Error("Could not commit bib transaction: %s", err)
		}
		activeTx = nil
		bibEntriesModified = false
	}
}

func rollbackBibTransaction() {
	if activeTx != nil {
		activeTx.Rollback()
		activeTx = nil
	}
}

// openEntry snapshots entry.Fields so that subsequent setEntryField / deleteEntryField
// calls only update the in-memory struct (and the cache when active) without issuing any
// DB writes. closeEntry then diffs the snapshot against the current fields and writes only
// the changed fields to the DB.
// When the cache is active and entry is not yet in it, openEntry registers it so that
// future loadEntryFromDb calls for the same key return the same pointer.
func openEntry(entry *TBibTeXEntry) {
	if entrySnapshots == nil {
		entrySnapshots = map[string]map[string]string{}
	}
	if entryCache != nil {
		if _, ok := entryCache[entry.Key]; !ok {
			entryCache[entry.Key] = entry
		}
	}
	snapshot := map[string]string{}
	for k, v := range entry.Fields {
		snapshot[k] = v
	}
	entrySnapshots[entry.Key] = snapshot
}

// closeEntry diffs the snapshot taken by openEntry against the current entry.Fields
// and writes only the changed, added, or deleted fields directly to the DB (bypassing
// upsertBibEntryField so that the cache — already correct — is not re-evaluated).
// Returns true when at least one field differed; the caller is responsible for setting
// bibEntriesModified when appropriate.
func closeEntry(entry *TBibTeXEntry) bool {
	snapshot, open := entrySnapshots[entry.Key]
	if !open {
		return false
	}
	delete(entrySnapshots, entry.Key)

	changed := false

	for field, value := range entry.Fields {
		if snapshot[field] != value {
			changed = true
			var err error
			if value == "" {
				err = bibExec(`DELETE FROM bib_entries WHERE entry_key = ? AND field = ?`, entry.Key, field)
			} else {
				err = bibExec(
					`INSERT INTO bib_entries (entry_key, field, value) VALUES (?, ?, ?)
					   ON CONFLICT(entry_key, field) DO UPDATE SET value = excluded.value;`,
					entry.Key, field, value)
			}
			if err != nil {
				dbInteraction.Warning("bib_entries write failed for %s.%s: %s", entry.Key, field, err)
			}
		}
	}

	for field := range snapshot {
		if _, exists := entry.Fields[field]; !exists {
			changed = true
			if err := bibExec(`DELETE FROM bib_entries WHERE entry_key = ? AND field = ?`, entry.Key, field); err != nil {
				dbInteraction.Warning("bib_entries delete failed for %s.%s: %s", entry.Key, field, err)
			}
		}
	}

	return changed
}

// clearBibTables removes all rows from the three bib tables without dropping them.
func clearBibTables() {
	for _, stmt := range []string{
		`DELETE FROM bib_entries;`,
		`DELETE FROM bib_groups;`,
		`DELETE FROM bib_comments;`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			dbInteraction.Warning("Could not clear bib table: %s", err)
		}
	}
	entryCache = nil
	entrySnapshots = nil
	bibEntriesModified = false
}

// upsertBibEntryField inserts or replaces a single (entry_key, field, value) row.
// An empty value deletes the row instead.
// Outside a transaction it compares the old cache value and only marks
// bibEntriesModified when the value actually changes, then updates the cache.
// Callers (setEntryField, deleteEntryField) must NOT pre-update the cache entry
// before calling this function, otherwise the comparison always finds equality.
func upsertBibEntryField(key, field, value string) {
	var err error
	if value == "" {
		err = bibExec(`DELETE FROM bib_entries WHERE entry_key = ? AND field = ?`, key, field)
	} else {
		err = bibExec(
			`INSERT INTO bib_entries (entry_key, field, value) VALUES (?, ?, ?)
			   ON CONFLICT(entry_key, field) DO UPDATE SET value = excluded.value;`,
			key, field, value)
	}
	if err != nil {
		dbInteraction.Warning("bib_entries write failed for %s.%s: %s", key, field, err)
	}
	if entryCache != nil {
		if value == "" {
			if e, ok := entryCache[key]; ok {
				if _, exists := e.Fields[field]; exists {
					if activeTx == nil {
						bibEntriesModified = true
					}
					delete(e.Fields, field)
				}
			}
		} else {
			e, ok := entryCache[key]
			if !ok {
				if activeTx == nil {
					bibEntriesModified = true
				}
				e = &TBibTeXEntry{Key: key, Fields: map[string]string{}}
				entryCache[key] = e
				e.Fields[field] = value
			} else if e.Fields[field] != value {
				if activeTx == nil {
					bibEntriesModified = true
				}
				e.Fields[field] = value
			}
		}
	} else if activeTx == nil {
		bibEntriesModified = true
	}
}

// deleteBibEntryField removes a single field row from bib_entries.
// Outside a transaction it checks for an actual change before marking modified.
func deleteBibEntryField(key, field string) {
	if err := bibExec(`DELETE FROM bib_entries WHERE entry_key = ? AND field = ?`, key, field); err != nil {
		dbInteraction.Warning("bib_entries delete failed for %s.%s: %s", key, field, err)
	}
	if entryCache != nil {
		if e, ok := entryCache[key]; ok {
			if _, exists := e.Fields[field]; exists {
				if activeTx == nil {
					bibEntriesModified = true
				}
				delete(e.Fields, field)
			}
		}
	} else if activeTx == nil {
		bibEntriesModified = true
	}
}

// deleteBibEntry removes all rows for a given entry key from bib_entries.
// Outside a transaction it checks for an actual change before marking modified.
func deleteBibEntry(key string) {
	if err := bibExec(`DELETE FROM bib_entries WHERE entry_key = ?`, key); err != nil {
		dbInteraction.Warning("bib_entries delete failed for %s: %s", key, err)
	}
	if entryCache != nil {
		if _, ok := entryCache[key]; ok {
			if activeTx == nil {
				bibEntriesModified = true
			}
			delete(entryCache, key)
		}
	} else if activeTx == nil {
		bibEntriesModified = true
	}
}

// loadEntryFromDb returns a TBibTeXEntry snapshot of all fields for key.
// Returns an entry with an empty Fields map (Exists() == false) when key is absent.
func loadEntryFromDb(key string) *TBibTeXEntry {
	if entryCache != nil {
		if e, ok := entryCache[key]; ok {
			return e
		}
		return &TBibTeXEntry{Key: key, Fields: map[string]string{}}
	}
	rows, err := bibQuery(`SELECT field, value FROM bib_entries WHERE entry_key = ?`, key)
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries for %s: %s", key, err)
		return &TBibTeXEntry{Key: key, Fields: map[string]string{}}
	}
	defer rows.Close()

	fields := map[string]string{}
	for rows.Next() {
		var field, value string
		if err := rows.Scan(&field, &value); err != nil {
			dbInteraction.Warning("Could not scan bib_entries row for %s: %s", key, err)
			continue
		}
		fields[field] = value
	}
	return &TBibTeXEntry{Key: key, Fields: fields}
}

// bibEntryExists reports whether bib_entries contains any row for key.
func bibEntryExists(key string) bool {
	if entryCache != nil {
		e, ok := entryCache[key]
		return ok && e.Fields[EntryTypeField] != ""
	}
	row := bibQueryRow(`SELECT 1 FROM bib_entries WHERE entry_key = ? AND field = ? LIMIT 1`, key, EntryTypeField)
	var dummy int
	return row.Scan(&dummy) == nil
}

// countBibEntries returns the number of distinct entry keys that have an entry-type row.
func countBibEntries() int {
	if entryCache != nil {
		n := 0
		for _, e := range entryCache {
			if e.Fields[EntryTypeField] != "" {
				n++
			}
		}
		return n
	}
	row := bibQueryRow(`SELECT COUNT(*) FROM bib_entries WHERE field = ?`, EntryTypeField)
	var n int
	row.Scan(&n)
	return n
}

// forEachBibEntryKey calls fn for every distinct entry key that has an entry-type row.
// Keys are collected into a slice before fn is called so the DB cursor is closed
// before any writes that fn might trigger.
// fn returns true to continue iteration, false to stop early (graceful quit).
func forEachBibEntryKey(fn func(key string) bool) {
	if entryCache != nil {
		for key, e := range entryCache {
			if e.Fields[EntryTypeField] != "" {
				if !fn(key) {
					return
				}
			}
		}
		return
	}
	rows, err := db.Query(`SELECT entry_key FROM bib_entries WHERE field = ?`, EntryTypeField)
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries for entry keys: %s", err)
		return
	}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			dbInteraction.Warning("Could not scan bib_entries entry key: %s", err)
			continue
		}
		keys = append(keys, key)
	}
	rows.Close()
	for _, key := range keys {
		if !fn(key) {
			return
		}
	}
}

// saveBibGroupsToDb writes l.GroupEntries to bib_groups using bibExec (transaction-aware).
func saveBibGroupsToDb(l *TBibTeXLibrary) {
	for group, entries := range l.GroupEntries {
		for entry := range entries.Elements() {
			if err := bibExec(`INSERT INTO bib_groups (group_name, entry_key) VALUES (?, ?)
			                     ON CONFLICT DO NOTHING;`, group, entry); err != nil {
				dbInteraction.Warning("bib_groups insert failed: %s", err)
			}
		}
	}
}

// saveBibCommentsToDb writes l.Comments to bib_comments using bibExec (transaction-aware).
func saveBibCommentsToDb(l *TBibTeXLibrary) {
	for i, comment := range l.Comments {
		if err := bibExec(`INSERT INTO bib_comments (position, content) VALUES (?, ?)
		                     ON CONFLICT DO NOTHING;`, i, comment); err != nil {
			dbInteraction.Warning("bib_comments insert failed: %s", err)
		}
	}
}

// loadGroupsFromDb populates l.GroupEntries from the bib_groups table.
func loadGroupsFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT group_name, entry_key FROM bib_groups`)
	if err != nil {
		dbInteraction.Warning("Could not query bib_groups: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var group, entry string
		if err := rows.Scan(&group, &entry); err != nil {
			dbInteraction.Warning("Could not scan bib_groups row: %s", err)
			continue
		}
		l.GroupEntries.AddValueToStringSetMap(group, entry)
	}
}

// loadCommentsFromDb populates l.Comments from the bib_comments table.
func loadCommentsFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT content FROM bib_comments ORDER BY position`)
	if err != nil {
		dbInteraction.Warning("Could not query bib_comments: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			dbInteraction.Warning("Could not scan bib_comments row: %s", err)
			continue
		}
		l.Comments = append(l.Comments, content)
	}
}

// buildKeyAliasesFromDb rebuilds the in-memory key alias and hint maps from fields
// stored in bib_entries that the parser would normally add via processPreferredAliasValue
// and processDblpValue.  Must be called on the fast path where no parse takes place.
func buildKeyAliasesFromDb(l *TBibTeXLibrary) {
	if entryCache != nil {
		for key, e := range entryCache {
			if alias := e.Fields[PreferredAliasField]; alias != "" {
				l.AddKeyAlias(alias, key)
				l.AddKeyHint(alias, key)
			}
			if dblp := e.Fields[DBLPField]; dblp != "" {
				l.AddKeyAlias(KeyForDBLP(dblp), key)
				l.AddKeyHint(KeyForDBLP(dblp), key)
			}
		}
		return
	}
	rows, err := db.Query(
		`SELECT entry_key, field, value FROM bib_entries WHERE field IN (?, ?)`,
		PreferredAliasField, DBLPField)
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries for key aliases: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, field, value string
		if err := rows.Scan(&key, &field, &value); err != nil {
			dbInteraction.Warning("Could not scan key alias row: %s", err)
			continue
		}
		switch field {
		case PreferredAliasField:
			l.AddKeyAlias(value, key)
			l.AddKeyHint(value, key)
		case DBLPField:
			l.AddKeyAlias(KeyForDBLP(value), key)
			l.AddKeyHint(KeyForDBLP(value), key)
		}
	}
}

// buildTitleIndexFromDb rebuilds l.TitleIndex from the title field in bib_entries.
func buildTitleIndexFromDb(l *TBibTeXLibrary) {
	l.TitleIndex = TStringSetMap{}
	if entryCache != nil {
		for key, e := range entryCache {
			if title := e.Fields[TitleField]; title != "" {
				l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(title), key)
			}
		}
		return
	}
	rows, err := db.Query(`SELECT entry_key, value FROM bib_entries WHERE field = ?`, TitleField)
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries for titles: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, title string
		if err := rows.Scan(&key, &title); err != nil {
			dbInteraction.Warning("Could not scan title row: %s", err)
			continue
		}
		l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(title), key)
	}
}

// --- ValidBibDb / timestamp ---

// ValidBibDb returns true when the bib_entries table is newer than the bib file
// and all mapping tables whose content affects entry values.
func (l *TBibTeXLibrary) ValidBibDb() bool {
	bibDbTime := tableModTime("bib_entries")
	if bibDbTime == 0 {
		return false
	}
	bibFile := l.FilesRoot + l.BaseName + BibFileExtension
	return bibDbTime > fileModTime(bibFile) &&
		bibDbTime > tableModTime("filter_cross_field_mappings") &&
		bibDbTime > tableModTime("filter_entry_field_mappings") &&
		bibDbTime > tableModTime("filter_generic_field_mappings") &&
		bibDbTime > tableModTime("name_mappings") &&
		bibDbTime > tableModTime("key_oldies")
}

// refreshBibDbTimestamp marks bib_entries as current. Must be called AFTER WriteBibTeXFile
// so the DB timestamp exceeds the bib file timestamp, keeping ValidBibDb true.
func refreshBibDbTimestamp() {
	setTableDate("bib_entries", time.Now().UnixMicro())
}

// --- bib entry iterators (used by write path) ---

// forEachBibEntryType calls fn(key, entryType) for every entry stored in bib_entries.
func forEachBibEntryType(fn func(key, entryType string)) {
	if entryCache != nil {
		for key, e := range entryCache {
			if t := e.Fields[EntryTypeField]; t != "" {
				fn(key, t)
			}
		}
		return
	}
	rows, err := db.Query(`SELECT entry_key, value FROM bib_entries WHERE field = ?`, EntryTypeField)
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries entry types: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, entryType string
		if err := rows.Scan(&key, &entryType); err != nil {
			dbInteraction.Warning("Could not scan bib_entries row: %s", err)
			continue
		}
		fn(key, entryType)
	}
}

// forEachBibField calls fn(key, field, value) for every field row in bib_entries.
func forEachBibField(fn func(key, field, value string)) {
	if entryCache != nil {
		for key, e := range entryCache {
			for field, value := range e.Fields {
				fn(key, field, value)
			}
		}
		return
	}
	rows, err := db.Query(`SELECT entry_key, field, value FROM bib_entries`)
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries fields: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, field, value string
		if err := rows.Scan(&key, &field, &value); err != nil {
			dbInteraction.Warning("Could not scan bib_entries row: %s", err)
			continue
		}
		fn(key, field, value)
	}
}

// forEachBibGroup calls fn(groupName, entryKey) for every row in bib_groups.
func forEachBibGroup(fn func(groupName, entryKey string)) {
	rows, err := db.Query(`SELECT group_name, entry_key FROM bib_groups`)
	if err != nil {
		dbInteraction.Warning("Could not query bib_groups: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var groupName, entryKey string
		if err := rows.Scan(&groupName, &entryKey); err != nil {
			dbInteraction.Warning("Could not scan bib_groups row: %s", err)
			continue
		}
		fn(groupName, entryKey)
	}
}

// forEachBibComment calls fn(content) for each comment in insertion order.
func forEachBibComment(fn func(content string)) {
	rows, err := db.Query(`SELECT content FROM bib_comments ORDER BY position`)
	if err != nil {
		dbInteraction.Warning("Could not query bib_comments: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			dbInteraction.Warning("Could not scan bib_comments row: %s", err)
			continue
		}
		fn(content)
	}
}
