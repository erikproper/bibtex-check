/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: DBLPXMLImporter
 *
 * Streaming importer from DBLP XML exports (.xml.gz) into a dedicated SQLite DB.
 * Field values are stored VERBATIM: HTML entity references (&iacute; etc.) are
 * preserved as "&name;" strings, HTML inline tags (<i>, <sup> etc.) are
 * reconstructed as "<tag>...</tag>" text, and Unicode characters from ISO-8859-1
 * decoding are stored as-is. Conversion to LaTeX happens at read time via
 * dblpRawToLaTeX (html_commands_map → html_character_map → unicode_map).
 * The title index is built during import by applying dblpRawToLaTeX before indexing.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 18.05.2026
 *
 */

package main

import (
	"compress/gzip"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// dblpDisambigSuffix matches the trailing disambiguation number that DBLP
// appends to person names in the bibtex= attribute (e.g. "Robert Winter 0001").
// These suffixes are DBLP-internal and are stripped when building dblp_name_bibtex.
var dblpDisambigSuffix = regexp.MustCompile(` \d{4}$`)


// --- DBLP SQLite DB ---

var dblpDB *sql.DB

const dblpDBFileName = "dblp.sqlite3"

// dblpEffectiveFolder returns the folder used for dblp.sqlite3 and XML.gz files.
// If global_folder is set in the config, that is used; otherwise falls back to
// bibTeXFolder so that no config change is required for single-library setups.
func dblpEffectiveFolder() string {
	if globalFolder != "" {
		return globalFolder
	}
	return bibTeXFolder
}

func dblpDBPath() string { return dblpEffectiveFolder() + dblpDBFileName }

func openDblpDB() bool {
	var err error
	dblpDB, err = sql.Open(sqliteDatabaseDriver, dblpDBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open DBLP database %s: %s\n", dblpDBPath(), err)
		return false
	}
	ensureDblpTablesExist()
	checkAndRebuildTitleIndexIfNeeded()
	return true
}

func closeDblpDB() {
	if dblpDB != nil {
		dblpDB.Close()
		dblpDB = nil
	}
}

// moveDblpToBackup moves dblp.sqlite3 to the backup folder.
// os.Rename is used first (instant on the same filesystem); if that fails
// (e.g. cross-device), it falls back to copy + remove.
func moveDblpToBackup() error {
	src := dblpDBPath()
	if err := os.MkdirAll(backupFolder, 0755); err != nil {
		return err
	}
	dst := filepath.Join(backupFolder, filepath.Base(src)+"."+timestamp())
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-device fallback: copy then remove original.
	if !BackupFile(src) {
		return fmt.Errorf("could not copy %s to backup", src)
	}
	return os.Remove(src)
}

func ensureDblpTablesExist() {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS dblp_entries (
			dblp_key TEXT    NOT NULL,
			field    TEXT    NOT NULL,
			position INTEGER NOT NULL DEFAULT 0,
			value    TEXT    NOT NULL,
			PRIMARY KEY (dblp_key, field, position)
		);`,
		`CREATE TABLE IF NOT EXISTS dblp_persons (
			dblp_key TEXT    NOT NULL,
			role     TEXT    NOT NULL,
			position INTEGER NOT NULL,
			name     TEXT    NOT NULL DEFAULT '',
			orcid    TEXT    NOT NULL DEFAULT '',
			PRIMARY KEY (dblp_key, role, position)
		);`,
		`CREATE TABLE IF NOT EXISTS dblp_title_index (
			field         TEXT NOT NULL,
			indexed_title TEXT NOT NULL,
			dblp_key      TEXT NOT NULL,
			PRIMARY KEY (field, indexed_title, dblp_key)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_dblp_title ON dblp_title_index (field, indexed_title);`,
		`CREATE TABLE IF NOT EXISTS dblp_name_bibtex (
			plain_name  TEXT PRIMARY KEY,
			bibtex_name TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS dblp_load_log (
			xml_file    TEXT    PRIMARY KEY,
			loaded_at   INTEGER NOT NULL,
			entry_count INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS dblp_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
	} {
		if _, err := dblpDB.Exec(stmt); err != nil {
			fmt.Fprintf(os.Stderr, "DBLP schema error: %s\n", err)
		}
	}
}

func clearDblpTables() {
	// Drop and recreate so that schema changes (e.g. added/removed columns) take
	// effect without requiring a manual migration step.
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS dblp_entries;`,
		`DROP TABLE IF EXISTS dblp_persons;`,
		`DROP TABLE IF EXISTS dblp_title_index;`,
	} {
		if _, err := dblpDB.Exec(stmt); err != nil {
			fmt.Fprintf(os.Stderr, "Could not drop DBLP table: %s\n", err)
		}
	}
	ensureDblpTablesExist()
}

// prepareDblpTablesForImport drops and recreates the three main import tables
// WITHOUT any indexes. Indexes degrade INSERT performance 10x at large row
// counts due to B-tree rebalancing; building them once after all data is
// loaded (via buildDblpImportIndexes) is dramatically faster.
func prepareDblpTablesForImport() {
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS dblp_entries;`,
		`DROP TABLE IF EXISTS dblp_persons;`,
		`DROP TABLE IF EXISTS dblp_title_index;`,
		`CREATE TABLE dblp_entries (
			dblp_key TEXT    NOT NULL,
			field    TEXT    NOT NULL,
			position INTEGER NOT NULL DEFAULT 0,
			value    TEXT    NOT NULL
		);`,
		`CREATE TABLE dblp_persons (
			dblp_key TEXT    NOT NULL,
			role     TEXT    NOT NULL,
			position INTEGER NOT NULL,
			name     TEXT    NOT NULL DEFAULT '',
			orcid    TEXT    NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE dblp_title_index (
			field         TEXT NOT NULL,
			indexed_title TEXT NOT NULL,
			dblp_key      TEXT NOT NULL
		);`,
	} {
		if _, err := dblpDB.Exec(stmt); err != nil {
			fmt.Fprintf(os.Stderr, "DBLP import schema error: %s\n", err)
		}
	}
}

// buildDblpImportIndexes creates the unique indexes after a full import.
// SQLite builds indexes via a bulk sort pass which is much faster than
// per-row B-tree maintenance during INSERT.
func buildDblpImportIndexes() {
	for _, stmt := range []string{
		`CREATE UNIQUE INDEX idx_dblp_entries ON dblp_entries (dblp_key, field, position);`,
		`CREATE UNIQUE INDEX idx_dblp_persons ON dblp_persons (dblp_key, role, position);`,
		`CREATE UNIQUE INDEX idx_dblp_title_pk ON dblp_title_index (field, indexed_title, dblp_key);`,
		`CREATE INDEX idx_dblp_title ON dblp_title_index (field, indexed_title);`,
	} {
		if _, err := dblpDB.Exec(stmt); err != nil {
			fmt.Fprintf(os.Stderr, "DBLP index build error: %s\n", err)
		}
	}
}

func csvFileHash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := md5.New()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

func rebuildDblpTitleIndex() {
	dblpDB.Exec(`DELETE FROM dblp_title_index`)
	rows, err := dblpDB.Query(
		`SELECT field, value, dblp_key FROM dblp_entries WHERE field IN ('title', 'booktitle') AND position = 0`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rebuildDblpTitleIndex: query error: %s\n", err)
		return
	}
	defer rows.Close()
	tx, err := dblpDB.Begin()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rebuildDblpTitleIndex: begin error: %s\n", err)
		return
	}
	st, err := tx.Prepare(`INSERT OR REPLACE INTO dblp_title_index (field, indexed_title, dblp_key) VALUES (?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return
	}
	defer st.Close()
	for rows.Next() {
		var field, value, key string
		rows.Scan(&field, &value, &key)
		value = strings.TrimSuffix(value, ".")
		if idx := TeXStringIndexer(dblpRawToLaTeX(value)); idx != "" {
			st.Exec(field, idx, key)
		}
	}
	tx.Commit()
}

func storeCsvMapHashes() {
	for _, kv := range [][2]string{
		{"html_commands_map_hash", csvFileHash(globalFolder + "html_commands_map.csv")},
		{"html_character_map_hash", csvFileHash(globalFolder + "html_character_map.csv")},
		{"unicode_map_hash", csvFileHash(globalFolder + "unicode_map.csv")},
	} {
		dblpDB.Exec(`INSERT OR REPLACE INTO dblp_meta (key, value) VALUES (?, ?)`, kv[0], kv[1])
	}
}

func checkAndRebuildTitleIndexIfNeeded() {
	type hashEntry struct {
		key  string
		hash string
	}
	entries := []hashEntry{
		{"html_commands_map_hash", csvFileHash(globalFolder + "html_commands_map.csv")},
		{"html_character_map_hash", csvFileHash(globalFolder + "html_character_map.csv")},
		{"unicode_map_hash", csvFileHash(globalFolder + "unicode_map.csv")},
	}
	needRebuild := false
	for _, e := range entries {
		var stored string
		dblpDB.QueryRow(`SELECT value FROM dblp_meta WHERE key = ?`, e.key).Scan(&stored)
		if stored != e.hash {
			needRebuild = true
			break
		}
	}
	if !needRebuild {
		return
	}
	fmt.Fprintf(os.Stderr, "CSV map(s) changed — rebuilding DBLP title index...\n")
	rebuildDblpTitleIndex()
	for _, e := range entries {
		dblpDB.Exec(`INSERT OR REPLACE INTO dblp_meta (key, value) VALUES (?, ?)`, e.key, e.hash)
	}
	fmt.Fprintf(os.Stderr, "DBLP title index rebuilt.\n")
}

func dblpEntryCount() int {
	if dblpDB == nil {
		return 0
	}
	row := dblpDB.QueryRow(`SELECT COUNT(DISTINCT dblp_key) FROM dblp_entries`)
	var n int
	row.Scan(&n)
	return n
}

// --- XML parser types ---

type dblpXMLPerson struct {
	Name   string
	Bibtex string
	ORCID  string
}

type dblpXMLField struct {
	Name     string
	Value    string
	Position int
}

// dblpKnownEntryTypes is the set of element names that are direct children of
// the <dblp> root and represent publications or person homepages.
var dblpKnownEntryTypes = map[string]bool{
	"article": true, "inproceedings": true, "proceedings": true,
	"book": true, "incollection": true, "inbook": true,
	"phdthesis": true, "mastersthesis": true,
	"www": true, "data": true, "misc": true,
}

// dblpMultiValuedFields lists child elements that may appear more than once
// within a single DBLP entry; each occurrence gets a distinct position value.
var dblpMultiValuedFields = map[string]bool{
	"ee": true, "url": true, "cite": true, "note": true,
}

// dblpTitleIndexedFields lists the entry fields whose normalised values are
// stored in dblp_title_index for fast title-based duplicate detection.
var dblpTitleIndexedFields = map[string]bool{
	"title": true, "booktitle": true,
}

// xmlCollectText reads XML tokens until the matching close tag and returns the
// concatenated text, storing HTML inline elements verbatim as "<tag>...</tag>" so
// that dblpRawToLaTeX can convert them to LaTeX at read time via html_commands_map.
func xmlCollectText(d *xml.Decoder) (string, error) {
	var buf strings.Builder
	var stack []string

	for {
		tok, err := d.Token()
		if err != nil {
			return buf.String(), err
		}
		switch t := tok.(type) {
		case xml.CharData:
			buf.Write(t)
		case xml.StartElement:
			tag := t.Name.Local
			buf.WriteString("<" + tag + ">")
			stack = append(stack, tag)
		case xml.EndElement:
			if len(stack) == 0 {
				return strings.TrimSpace(buf.String()), nil
			}
			tag := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			buf.WriteString("</" + tag + ">")
		}
	}
}


// --- Main import function ---

const dblpTxBatchSize = 50_000

// dblpImportFromReader streams DBLP XML from r into dblpDB.
// progress is called every dblpTxBatchSize entries (may be nil).
// Returns the total entry count and any fatal error.
func dblpImportFromReader(r io.Reader, progress func(n int)) (int, error) {
	decoder := xml.NewDecoder(r)
	decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		if strings.EqualFold(charset, "iso-8859-1") {
			return transform.NewReader(input, charmap.ISO8859_1.NewDecoder()), nil
		}
		return input, nil
	}
	decoder.Entity = xmlEntityPassthrough
	decoder.Strict = false

	// Advance to the <dblp> root element.
	for {
		tok, err := decoder.Token()
		if err != nil {
			return 0, fmt.Errorf("seeking <dblp> root: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "dblp" {
			break
		}
	}

	tx, err := dblpDB.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}

	const qEntry = `INSERT INTO dblp_entries
		(dblp_key, field, position, value) VALUES (?, ?, ?, ?)`
	const qPerson = `INSERT INTO dblp_persons
		(dblp_key, role, position, name, orcid) VALUES (?, ?, ?, ?, ?)`
	const qTitle = `INSERT INTO dblp_title_index
		(field, indexed_title, dblp_key) VALUES (?, ?, ?)`
	const qNameMap = `INSERT OR IGNORE INTO dblp_name_bibtex (plain_name, bibtex_name) VALUES (?, ?)`

	// rebatch commits the current transaction and opens a fresh one, re-preparing
	// the statements. Called every dblpTxBatchSize entries.
	var stEntry, stPerson, stTitle, stNameMap *sql.Stmt
	prepareStmts := func() error {
		var e error
		if stEntry, e = tx.Prepare(qEntry); e != nil {
			return e
		}
		if stPerson, e = tx.Prepare(qPerson); e != nil {
			return e
		}
		if stTitle, e = tx.Prepare(qTitle); e != nil {
			return e
		}
		if stNameMap, e = tx.Prepare(qNameMap); e != nil {
			return e
		}
		return nil
	}
	closeStmts := func() {
		if stEntry != nil {
			stEntry.Close()
		}
		if stPerson != nil {
			stPerson.Close()
		}
		if stTitle != nil {
			stTitle.Close()
		}
		if stNameMap != nil {
			stNameMap.Close()
		}
	}
	if err := prepareStmts(); err != nil {
		tx.Rollback()
		return 0, fmt.Errorf("prepare statements: %w", err)
	}

	rebatch := func() error {
		closeStmts()
		if err := tx.Commit(); err != nil {
			return err
		}
		tx, err = dblpDB.Begin()
		if err != nil {
			return err
		}
		return prepareStmts()
	}

	count := 0
	var parseErr error

outer:
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			parseErr = fmt.Errorf("XML token near entry %d: %w", count, err)
			break
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if !dblpKnownEntryTypes[se.Name.Local] {
			decoder.Skip()
			continue
		}

		entryType := se.Name.Local
		var dblpKey, mdate, pubType string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "key":
				dblpKey = a.Value
			case "mdate":
				mdate = a.Value
			case "publtype":
				pubType = a.Value
			}
		}
		if dblpKey == "" {
			decoder.Skip()
			continue
		}

		// Collect child elements of this entry.
		var authors, editors []dblpXMLPerson
		var fields []dblpXMLField
		fieldPos := map[string]int{}

	childLoop:
		for {
			child, cerr := decoder.Token()
			if cerr != nil {
				parseErr = fmt.Errorf("reading entry %s: %w", dblpKey, cerr)
				break outer
			}
			switch ct := child.(type) {
			case xml.StartElement:
				name := ct.Name.Local
				var bibtex, orcid string
				for _, a := range ct.Attr {
					switch a.Name.Local {
					case "bibtex":
						bibtex = a.Value
					case "orcid":
						orcid = a.Value
					}
				}
				text, terr := xmlCollectText(decoder)
				if terr != nil {
					parseErr = fmt.Errorf("field %s in %s: %w", name, dblpKey, terr)
					break outer
				}
				switch name {
				case "author":
					authors = append(authors, dblpXMLPerson{Name: text, Bibtex: bibtex, ORCID: orcid})
				case "editor":
					editors = append(editors, dblpXMLPerson{Name: text, Bibtex: bibtex, ORCID: orcid})
				default:
					// Store text content verbatim; LaTeX conversion via dblpRawToLaTeX happens at read time.
					// The bibtex= attribute is not used for field values.
					pos := 0
					if dblpMultiValuedFields[name] {
						pos = fieldPos[name]
						fieldPos[name]++
					}
					fields = append(fields, dblpXMLField{Name: name, Value: text, Position: pos})
				}
			case xml.EndElement:
				break childLoop
			}
		}

		// Write entry to the database.
		stEntry.Exec(dblpKey, "entrytype", 0, entryType)
		if mdate != "" {
			stEntry.Exec(dblpKey, "mdate", 0, mdate)
		}
		if pubType != "" {
			stEntry.Exec(dblpKey, "publtype", 0, pubType)
		}
		for _, f := range fields {
			// Store verbatim text content; LaTeX conversion happens at read time.
			value := f.Value
			if dblpTitleIndexedFields[f.Name] {
				value = strings.TrimSuffix(value, ".")
			}
			stEntry.Exec(dblpKey, f.Name, f.Position, value)
			if dblpTitleIndexedFields[f.Name] {
				// Apply read-time pipeline to build title index from the LaTeX form.
				if idx := TeXStringIndexer(dblpRawToLaTeX(value)); idx != "" {
					stTitle.Exec(f.Name, idx, dblpKey)
				}
			}
		}
		storePerson := func(role string, i int, p dblpXMLPerson) {
			// Store plain text name verbatim; dblpRawToLaTeX converts at read time.
			stPerson.Exec(dblpKey, role, i, p.Name, p.ORCID)
		}
		for i, p := range authors {
			storePerson("author", i, p)
		}
		for i, p := range editors {
			storePerson("editor", i, p)
		}

				// For person-homepage (www) entries, record all author name variants
		// in dblp_name_bibtex, mapping plain text names to the canonical LaTeX form
		// from the bibtex= attribute (with disambiguation suffix stripped).
		if entryType == "www" {
			var canonicalName string
			for _, p := range authors {
				if p.Bibtex != "" {
					// applyUnicodeMap handles any stray Unicode in bibtex= values.
					canonicalName = dblpDisambigSuffix.ReplaceAllString(applyUnicodeMap(p.Bibtex), "")
					break
				}
			}
			if canonicalName != "" {
				for _, p := range authors {
					plain := p.Name // verbatim text content (Unicode)
					if plain != canonicalName {
						stNameMap.Exec(plain, canonicalName)
					}
				}
			}
		}

		count++
		if count%dblpTxBatchSize == 0 {
			if progress != nil {
				progress(count)
			}
			if rerr := rebatch(); rerr != nil {
				parseErr = fmt.Errorf("rebatch at %d: %w", count, rerr)
				break outer
			}
		}
	}

	closeStmts()
	if parseErr != nil {
		tx.Rollback()
		return count, parseErr
	}
	if err := tx.Commit(); err != nil {
		return count, fmt.Errorf("final commit: %w", err)
	}
	return count, nil
}

// countingReader wraps an io.Reader and tracks the total number of bytes read.
type countingReader struct {
	r io.Reader
	n int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.n += int64(n)
	return n, err
}

// --- CLI: -load_dblp_xml ---

func doLoadDblpXml(args []string) {
	xmlGzPath := args[0]
	if !FileExists(xmlGzPath) {
		fmt.Fprintf(os.Stderr, "File not found: %s\n", xmlGzPath)
		os.Exit(1)
	}

	fi, err := os.Stat(xmlGzPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot stat %s: %s\n", xmlGzPath, err)
		os.Exit(1)
	}
	totalBytes := fi.Size()

	if !openDblpDB() {
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Counting existing DBLP entries...\n")
	existing := dblpEntryCount()
	closeDblpDB()

	if existing > 0 {
		fmt.Fprintf(os.Stderr, "Backing up existing DBLP database (%d entries)...\n", existing)
		if err := moveDblpToBackup(); err != nil {
			fmt.Fprintf(os.Stderr, "Backup failed: %s\n", err)
			os.Exit(1)
		}
	}

	if !openDblpDB() {
		os.Exit(1)
	}
	defer closeDblpDB()

	// Performance pragmas for bulk import.
	dblpDB.Exec(`PRAGMA synchronous = OFF`)
	dblpDB.Exec(`PRAGMA journal_mode = OFF`)
	dblpDB.Exec(`PRAGMA cache_size = -524288`) // 512 MB

	// Recreate the main tables without indexes. Indexes degrade INSERT
	// performance 10x at large row counts; they are built in one bulk
	// sort pass via buildDblpImportIndexes after all rows are loaded.
	prepareDblpTablesForImport()

	f, err := os.Open(xmlGzPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open %s: %s\n", xmlGzPath, err)
		os.Exit(1)
	}
	defer f.Close()

	cr := &countingReader{r: f}
	gz, err := gzip.NewReader(cr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not read gzip from %s: %s\n", xmlGzPath, err)
		os.Exit(1)
	}
	defer gz.Close()

	fmt.Fprintf(os.Stderr, "Importing DBLP XML from %s...\n", xmlGzPath)
	start := time.Now()

	count, err := dblpImportFromReader(gz, func(n int) {
		pct := float64(cr.n) * 100.0 / float64(totalBytes)
		fmt.Fprintf(os.Stderr, "  %d entries imported (%.0fs, %.0f%%)...\n",
			n, time.Since(start).Seconds(), pct)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Import failed after %d entries: %s\n", count, err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Applying www name mappings to person entries...\n")
	res, uerr := dblpDB.Exec(`UPDATE dblp_persons
		SET name = (SELECT bibtex_name FROM dblp_name_bibtex WHERE plain_name = dblp_persons.name)
		WHERE name IN (SELECT plain_name FROM dblp_name_bibtex)`)
	if uerr != nil {
		fmt.Fprintf(os.Stderr, "Warning: name-map update failed: %s\n", uerr)
	} else if n, _ := res.RowsAffected(); n > 0 {
		fmt.Fprintf(os.Stderr, "  Updated %d person name entries from www mappings.\n", n)
	}

	fmt.Fprintf(os.Stderr, "Building indexes...\n")
	buildDblpImportIndexes()
	storeCsvMapHashes()

	dblpDB.Exec(
		`INSERT OR REPLACE INTO dblp_load_log (xml_file, loaded_at, entry_count) VALUES (?, ?, ?)`,
		xmlGzPath, time.Now().UnixMicro(), count)

	fmt.Fprintf(os.Stderr, "DBLP import complete: %d entries in %.1fs\n",
		count, time.Since(start).Seconds())
}

// --- CLI: -update_dblp ---

const dblpXMLIndexURL = "https://dblp.uni-trier.de/xml/"

var reDblpXMLFilename = regexp.MustCompile(`dblp-\d{4}-\d{2}-\d{2}\.xml\.gz`)
var reDblpUndatedDate = regexp.MustCompile(`dblp\.xml\.gz[^0-9]*(\d{4}-\d{2}-\d{2})`)

// doUpdateDblp fetches the DBLP XML index page, identifies the latest dated
// release (or falls back to the undated dblp.xml.gz stored with today's date),
// and downloads it to dblpEffectiveFolder() if not already present.
// The previous .xml.gz is left in place (files have distinct dated names).
func doUpdateDblp() {
	fmt.Fprintf(os.Stderr, "Fetching DBLP XML index from %s...\n", dblpXMLIndexURL)
	resp, err := http.Get(dblpXMLIndexURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not fetch DBLP XML index: %s\n", err)
		os.Exit(1)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not read DBLP XML index: %s\n", err)
		os.Exit(1)
	}

	var latest, downloadURL string
	matches := reDblpXMLFilename.FindAllString(string(body), -1)
	if len(matches) > 0 {
		sort.Strings(matches)
		latest = matches[len(matches)-1]
		downloadURL = dblpXMLIndexURL + latest
	} else if m := reDblpUndatedDate.FindStringSubmatch(string(body)); m != nil {
		// No dated files; use the modification date from the directory listing.
		latest = "dblp-" + m[1] + ".xml.gz"
		downloadURL = dblpXMLIndexURL + "dblp.xml.gz"
		fmt.Fprintf(os.Stderr, "No dated files found; using dblp.xml.gz (dated %s) → %s\n", m[1], latest)
	} else if strings.Contains(string(body), "dblp.xml.gz") {
		// Undated file present but no parseable date; fall back to today's date.
		latest = "dblp-" + time.Now().Format("2006-01-02") + ".xml.gz"
		downloadURL = dblpXMLIndexURL + "dblp.xml.gz"
		fmt.Fprintf(os.Stderr, "No dated files found; using dblp.xml.gz → %s\n", latest)
	} else {
		fmt.Fprintf(os.Stderr, "No DBLP XML files found at %s\n", dblpXMLIndexURL)
		os.Exit(1)
	}

	destPath := dblpEffectiveFolder() + latest
	if FileExists(destPath) {
		fmt.Fprintf(os.Stderr, "Already have %s — nothing to do.\n", destPath)
		return
	}

	fmt.Fprintf(os.Stderr, "Downloading %s → %s\n", downloadURL, destPath)

	resp2, err := http.Get(downloadURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Download failed: %s\n", err)
		os.Exit(1)
	}
	defer resp2.Body.Close()
	total := resp2.ContentLength

	f, err := os.Create(destPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not create %s: %s\n", destPath, err)
		os.Exit(1)
	}
	defer f.Close()

	cr := &countingReader{r: resp2.Body}
	start := time.Now()

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				mb := float64(cr.n) / 1e6
				elapsed := time.Since(start).Seconds()
				if total > 0 {
					fmt.Fprintf(os.Stderr, "  %.0f / %.0f MB (%.0f%%) %.0fs\n",
						mb, float64(total)/1e6, float64(cr.n)*100/float64(total), elapsed)
				} else {
					fmt.Fprintf(os.Stderr, "  %.0f MB %.0fs\n", mb, elapsed)
				}
			case <-done:
				return
			}
		}
	}()

	if _, err = io.Copy(f, cr); err != nil {
		close(done)
		fmt.Fprintf(os.Stderr, "\nDownload error: %s\n", err)
		os.Remove(destPath)
		os.Exit(1)
	}
	close(done)

	fmt.Fprintf(os.Stderr, "Downloaded %s (%.0f MB) in %.1fs\n",
		latest, float64(cr.n)/1e6, time.Since(start).Seconds())
	doLoadDblpXml([]string{destPath})
}
