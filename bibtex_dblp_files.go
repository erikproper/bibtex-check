/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: DBLPFileStore
 *
 * File-based DBLP store under ~/BiBTeX.Generics/DBLP/.
 * Each entry is entries/<dblp_key>/data.json; title lookups use
 * hash-prefixed link.txt files under titles/.
 * Text values are stored verbatim from the XML; LaTeX conversion via
 * dblpRawToLaTeX happens at read time.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 19.05.2026
 *
 */

package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// dblpFolder returns the root of the DBLP file store.
// Falls back to bibTeXFolder+"DBLP.cache/" when global_folder is not configured.
func dblpFolder() string {
	if globalFolder != "" {
		return globalFolder + "DBLP.cache/"
	}
	return bibTeXFolder + "DBLP.cache/"
}

// maybeMigrateDblpFolder renames the legacy "DBLP/" folder to "DBLP.cache/" on first
// run after upgrading. Called at startup before any DBLP file-store access.
func maybeMigrateDblpFolder() {
	newPath := dblpFolder()
	if _, err := os.Stat(newPath); err == nil {
		return // already using new name
	}
	var oldPath string
	if globalFolder != "" {
		oldPath = globalFolder + "DBLP/"
	} else {
		oldPath = bibTeXFolder + "DBLP/"
	}
	if _, err := os.Stat(oldPath); err == nil {
		if err := os.Rename(oldPath, newPath); err == nil {
			dbInteraction.Progress("Migrated DBLP folder: DBLP/ → DBLP.cache/")
		}
	}
}

// maybeMigrateDblpNameFiles moves the Pass-1 DBLP name CSVs (dblp_name_bibtex.csv,
// dblp_name_orcid.csv) from the legacy globalFolder location into dblpFolder(),
// where they belong alongside the rest of the DBLP-derived cache. Called at
// startup before any access to those files.
func maybeMigrateDblpNameFiles() {
	if globalFolder == "" {
		return // no separate global folder; files were always alongside DBLP.cache
	}
	for _, name := range []string{"dblp_name_bibtex.csv", "dblp_name_orcid.csv"} {
		oldPath := globalFolder + name
		newPath := dblpFolder() + name
		if _, err := os.Stat(newPath); err == nil {
			continue // already migrated
		}
		if _, err := os.Stat(oldPath); err != nil {
			continue // nothing to migrate
		}
		if err := os.MkdirAll(dblpFolder(), 0755); err != nil {
			continue
		}
		if err := os.Rename(oldPath, newPath); err == nil {
			dbInteraction.Progress("Migrated %s: globalFolder → DBLP.cache/", name)
		}
	}
}

func dblpTrashFolder() string { return dblpFolder() + "trash/" }

// moveToDblpTrash moves path into the DBLP trash folder with a timestamp
// suffix. The rename is an O(1) syscall regardless of how many files are inside.
func moveToDblpTrash(path string) error {
	if err := os.MkdirAll(dblpTrashFolder(), 0755); err != nil {
		return err
	}
	dest := dblpTrashFolder() + filepath.Base(path) + "-" + time.Now().Format("20060102-150405")
	return os.Rename(path, dest)
}

// maybeStartDblpTrashCleanup launches a background goroutine to remove the
// contents of the DBLP trash folder when it is non-empty. The goroutine is
// not waited on; if the process exits before it completes the remaining trash
// is cleaned on the next invocation. Skipped when cmdNoGarbageCleaning is set.
func maybeStartDblpTrashCleanup() {
	if cmdNoGarbageCleaning {
		return
	}
	des, err := os.ReadDir(dblpTrashFolder())
	if err != nil || len(des) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "Deleting DBLP trash in background.\n")
	go os.RemoveAll(dblpTrashFolder())
}

func dblpEntryFilePath(dblpKey string) string {
	return dblpFolder() + "entries/" + dblpKey + "/data.json"
}

// dblpTitleLinkPath returns the path of the link.txt for a given title hash.
// Uses a 2/2/full three-level prefix structure for filesystem scalability.
func dblpTitleLinkPath(hash string) string {
	return dblpFolder() + "titles/" + hash[0:2] + "/" + hash[2:4] + "/" + hash + "/link.txt"
}

// dblpCrossrefChildrenPath returns the path of the children.txt for a given
// parent DBLP key. The key is used directly as a path component (same convention
// as entries/), making the index human-navigable.
func dblpCrossrefChildrenPath(parentKey string) string {
	return dblpFolder() + "crossrefs/" + parentKey + "/children.txt"
}

// dblpPersonNameHash returns the MD5 hex digest of the canonical LaTeX form of
// a person name, for use as the persons/ index key. Returns "" for empty names.
func dblpPersonNameHash(canonicalName string) string {
	s := dblpRawToLaTeX(canonicalName)
	if s == "" {
		return ""
	}
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// dblpORCIDHash returns the MD5 hex digest of a raw ORCID string.
// Returns "" for empty ORCIDs.
func dblpORCIDHash(orcid string) string {
	if orcid == "" {
		return ""
	}
	h := md5.Sum([]byte(orcid))
	return hex.EncodeToString(h[:])
}

func dblpPersonEntriesPath(nameHash string) string {
	return dblpFolder() + "persons/" + nameHash[0:2] + "/" + nameHash[2:4] + "/" + nameHash + "/entries.txt"
}

func dblpORCIDEntriesPath(orcidHash string) string {
	return dblpFolder() + "orcids/" + orcidHash[0:2] + "/" + orcidHash[2:4] + "/" + orcidHash + "/entries.txt"
}

func dblpFolderExists() bool {
	_, err := os.Stat(dblpFolder())
	return err == nil
}

// csvFileHash returns the MD5 hex digest of the file at path, or "" on error.
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

// dblpTitleHash returns the MD5 hex digest of the normalised, indexed form of
// a verbatim title value (as stored in data.json). Returns "" for empty titles.
func dblpTitleHash(verbatimTitle string) string {
	idx := TeXStringIndexer(dblpRawToLaTeX(verbatimTitle))
	if idx == "" {
		return ""
	}
	h := md5.Sum([]byte(idx))
	return hex.EncodeToString(h[:])
}

// libraryTitleHash returns the MD5 hex digest of the normalised, indexed form
// of a LaTeX title value (as stored in the library). Produces the same hash as
// dblpTitleHash for equivalent titles since TeXStringIndexer normalises encoding
// differences. Returns "" for empty titles.
func libraryTitleHash(latexTitle string) string {
	idx := TeXStringIndexer(latexTitle)
	if idx == "" {
		return ""
	}
	h := md5.Sum([]byte(idx))
	return hex.EncodeToString(h[:])
}

// --- JSON types ---

// TDblpJSONPerson is the JSON representation of an author or editor.
type TDblpJSONPerson struct {
	Name  string `json:"name"`
	ORCID string `json:"orcid,omitempty"`
}

// TDblpJSONEntry is the JSON representation of a DBLP entry stored in data.json.
// Fields holds single-valued fields (title, year, journal, etc.).
// Multi holds multi-valued fields (ee, url, cite, note).
// All text values are stored verbatim from the XML.
type TDblpJSONEntry struct {
	EntryType string              `json:"entrytype"`
	Mdate     string              `json:"mdate,omitempty"`
	PubType   string              `json:"publtype,omitempty"`
	Fields    map[string]string   `json:"fields,omitempty"`
	Authors   []TDblpJSONPerson   `json:"authors,omitempty"`
	Editors   []TDblpJSONPerson   `json:"editors,omitempty"`
	Multi     map[string][]string `json:"multi,omitempty"`
}

// TDblpMeta is the JSON representation of DBLP/meta.json.
type TDblpMeta struct {
	XMLFile              string `json:"xml_file"`
	LoadedAt             string `json:"loaded_at"`
	EntryCount           int    `json:"entry_count"`
	HtmlCommandsMapHash  string `json:"html_commands_map_hash,omitempty"`
	HtmlCharacterMapHash string `json:"html_character_map_hash,omitempty"`
	UnicodeMapHash       string `json:"unicode_map_hash,omitempty"`
}

// --- Write functions ---

func writeDblpEntryFile(dblpKey string, jsonBytes []byte) error {
	path := dblpEntryFilePath(dblpKey)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, jsonBytes, 0644)
}

// appendToIndexFile appends a single line to a hash-keyed index file,
// creating parent directories as needed.
func appendToIndexFile(path, line string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	// Skip the write when the line is already present — avoids duplicates when
	// an entry's mdate changes but its indexed value (title, person, etc.) does not.
	if existing, err := os.ReadFile(path); err == nil {
		for _, l := range strings.Split(strings.TrimSpace(string(existing)), "\n") {
			if l == line {
				return nil
			}
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, line)
	return err
}

// writeDblpPersonEntry records dblpKey in the persons/ index for canonicalName.
func writeDblpPersonEntry(canonicalName, dblpKey string) error {
	hash := dblpPersonNameHash(canonicalName)
	if hash == "" {
		return nil
	}
	return appendToIndexFile(dblpPersonEntriesPath(hash), dblpKey)
}

// writeDblpORCIDEntry records dblpKey in the orcids/ index for orcid.
func writeDblpORCIDEntry(orcid, dblpKey string) error {
	hash := dblpORCIDHash(orcid)
	if hash == "" {
		return nil
	}
	return appendToIndexFile(dblpORCIDEntriesPath(hash), dblpKey)
}

// writeDblpCrossrefChild appends childKey to the children.txt for parentKey.
func writeDblpCrossrefChild(parentKey, childKey string) error {
	if parentKey == "" {
		return nil
	}
	return appendToIndexFile(dblpCrossrefChildrenPath(parentKey), childKey)
}

// writeDblpTitleLink appends dblpKey to the link.txt for the given title hash.
func writeDblpTitleLink(hash, dblpKey string) error {
	if hash == "" {
		return nil
	}
	return appendToIndexFile(dblpTitleLinkPath(hash), dblpKey)
}

func writeDblpMeta(meta TDblpMeta) error {
	if err := os.MkdirAll(dblpFolder(), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dblpFolder()+"meta.json", append(data, '\n'), 0644)
}

// --- Read functions ---

// readDblpJSONEntry reads and parses data.json for dblpKey. Returns nil when the
// file does not exist or cannot be parsed.
func readDblpJSONEntry(dblpKey string) *TDblpJSONEntry {
	data, err := os.ReadFile(dblpEntryFilePath(dblpKey))
	if err != nil {
		return nil
	}
	var je TDblpJSONEntry
	if err := json.Unmarshal(data, &je); err != nil {
		return nil
	}
	return &je
}

// dblpEntryFromFile builds a TBibTeXEntry from the file store for dblpKey.
// Returns nil when the entry does not exist in the store.
func dblpEntryFromFile(dblpKey string) *TBibTeXEntry {
	je := readDblpJSONEntry(dblpKey)
	if je == nil {
		return nil
	}
	return jsonEntryToLibEntry(dblpKey, je)
}

// dblpEntryTypeToBibTeX maps DBLP-specific entry types to their BibTeX equivalents.
// DBLP's "data" (dataset) and "www" (person homepage) have no BibTeX counterpart;
// both are represented as "misc".
var dblpEntryTypeToBibTeX = map[string]string{
	"data": "misc",
	"www":  "misc",
}

// jsonEntryToLibEntry converts a TDblpJSONEntry to a TBibTeXEntry.
func jsonEntryToLibEntry(dblpKey string, je *TDblpJSONEntry) *TBibTeXEntry {
	entry := &TBibTeXEntry{Key: KeyForDBLP(dblpKey), Fields: map[string]string{}}
	entryType := je.EntryType
	if mapped, ok := dblpEntryTypeToBibTeX[entryType]; ok {
		entryType = mapped
	}
	entry.Fields[EntryTypeField] = entryType
	for field, value := range je.Fields {
		entry.Fields[field] = dblpRawToLaTeX(value)
	}
	// ee: derive doi when possible; otherwise keep first HTTP(S) value as url.
	if ees, ok := je.Multi["ee"]; ok && entry.Fields["doi"] == "" {
		for _, ee := range ees {
			normalised := NormaliseURLValue(nil, ee)
			if strings.HasPrefix(normalised, "https://doi.org/") {
				entry.Fields["doi"] = strings.TrimPrefix(normalised, "https://doi.org/")
				break
			} else if (strings.HasPrefix(normalised, "http://") || strings.HasPrefix(normalised, "https://")) && entry.Fields["url"] == "" {
				entry.Fields["url"] = normalised
			}
		}
	}
	buildNames := func(role string, persons []TDblpJSONPerson) {
		if len(persons) == 0 {
			return
		}
		names := make([]string, 0, len(persons))
		for _, p := range persons {
			names = append(names, dblpPersonNameToLaTeX(p.Name))
		}
		entry.Fields[role] = strings.Join(names, " and ")
	}
	buildNames("author", je.Authors)
	buildNames("editor", je.Editors)
	return entry
}

// dblpWithdrawnInfoFromFile returns (true, mdate) when data.json for dblpKey
// has publtype == "withdrawn". Always reads from disk (not preload cache).
func dblpWithdrawnInfoFromFile(dblpKey string) (bool, string) {
	je := readDblpJSONEntry(dblpKey)
	if je == nil || je.PubType != "withdrawn" {
		return false, ""
	}
	return true, je.Mdate
}

// readIndexFile reads a newline-delimited index file, returning one string per
// non-empty line. Returns nil when the file does not exist.
func readIndexFile(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// readDblpCrossrefChildren returns the DBLP child keys for a given parent key.
func readDblpCrossrefChildren(parentKey string) []string {
	if parentKey == "" {
		return nil
	}
	return readIndexFile(dblpCrossrefChildrenPath(parentKey))
}

// readDblpPersonEntries returns the DBLP entry keys for a given canonical name.
func readDblpPersonEntries(canonicalName string) []string {
	hash := dblpPersonNameHash(canonicalName)
	if hash == "" {
		return nil
	}
	return readIndexFile(dblpPersonEntriesPath(hash))
}

// readDblpORCIDEntries returns the DBLP entry keys for a given ORCID.
func readDblpORCIDEntries(orcid string) []string {
	hash := dblpORCIDHash(orcid)
	if hash == "" {
		return nil
	}
	return readIndexFile(dblpORCIDEntriesPath(hash))
}

// readDblpTitleLinks returns the DBLP keys stored in the link.txt for a title hash.
func readDblpTitleLinks(hash string) []string {
	if hash == "" {
		return nil
	}
	return readIndexFile(dblpTitleLinkPath(hash))
}

func readDblpMeta() *TDblpMeta {
	data, err := os.ReadFile(dblpFolder() + "meta.json")
	if err != nil {
		return nil
	}
	var meta TDblpMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil
	}
	return &meta
}

// readDblpCurrentXML returns the basename of the last successfully imported XML file,
// or "" if no import has completed.
func readDblpCurrentXML() string {
	data, err := os.ReadFile(dblpFolder() + "current.txt")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// writeDblpCurrentXML records xmlPath as the last successfully imported XML file.
// Only the basename is stored so the path is portable across global_folder changes.
func writeDblpCurrentXML(xmlPath string) {
	content := filepath.Base(xmlPath) + "\n"
	if err := os.WriteFile(dblpFolder()+"current.txt", []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write current.txt: %s\n", err)
	}
}

// removeKeyFromIndexFile removes all lines equal to key from the line-delimited file
// at path. Deletes the file (and prunes empty parent directories up to dblpFolder())
// when no lines remain.
func removeKeyFromIndexFile(path, key string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var kept []string
	changed := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		if line == key {
			changed = true
		} else {
			kept = append(kept, line)
		}
	}
	if !changed {
		return
	}
	if len(kept) == 0 {
		os.Remove(path)
		pruneEmptyDblpDirs(filepath.Dir(path))
	} else {
		os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0644)
	}
}

// pruneEmptyDblpDirs removes empty directories from dir up to (but not including)
// dblpFolder(). Stops at the first non-empty directory.
func pruneEmptyDblpDirs(dir string) {
	root := dblpFolder()
	for dir != root && dir != "/" && dir != "." {
		if os.Remove(dir) != nil {
			break
		}
		dir = filepath.Dir(dir)
	}
}

// removeKeyFromAllIndexes removes dblpKey from every index file it contributed to,
// using the provided JSON entry to compute title/crossref/person/ORCID hashes.
func removeKeyFromAllIndexes(dblpKey string, je *TDblpJSONEntry) {
	if je == nil {
		return
	}
	// Title index
	for _, fieldName := range []string{"title", "booktitle"} {
		if je.Fields != nil {
			if value := je.Fields[fieldName]; value != "" {
				if hash := dblpTitleHash(value); hash != "" {
					removeKeyFromIndexFile(dblpTitleLinkPath(hash), dblpKey)
				}
			}
		}
	}
	// Crossref children index
	if je.Fields != nil {
		if parentKey := je.Fields["crossref"]; parentKey != "" {
			removeKeyFromIndexFile(dblpCrossrefChildrenPath(parentKey), dblpKey)
		}
	}
	// Person and ORCID indexes
	for _, p := range append(je.Authors, je.Editors...) {
		if hash := dblpPersonNameHash(p.Name); hash != "" {
			removeKeyFromIndexFile(dblpPersonEntriesPath(hash), dblpKey)
		}
		if p.ORCID != "" {
			if hash := dblpORCIDHash(p.ORCID); hash != "" {
				removeKeyFromIndexFile(dblpORCIDEntriesPath(hash), dblpKey)
			}
		}
	}
}

// --- Manifest ---

const dblpManifestFilename = "manifest.csv"

// TDblpManifestEntry holds the mdate of a DBLP entry's data.json.
type TDblpManifestEntry struct {
	Mdate string
}

// TDblpManifest maps parent directories (relative to entries/) to entry names
// and their manifest data. e.g. m["conf/ijcai"]["2023"] for DBLP key "conf/ijcai/2023".
// Persisted as a single flat entries.manifest file at dblpFolder().
type TDblpManifest map[string]map[string]TDblpManifestEntry

// dblpEntryParentAndName splits a DBLP key into parent directory and entry name.
// "conf/ijcai/2023" → ("conf/ijcai", "2023").
func dblpEntryParentAndName(dblpKey string) (parentDir, entryName string) {
	i := strings.LastIndex(dblpKey, "/")
	if i < 0 {
		return ".", dblpKey
	}
	return dblpKey[:i], dblpKey[i+1:]
}

// add records dblpKey with its mdate in the manifest.
func (m TDblpManifest) add(dblpKey, mdate string) {
	parentDir, entryName := dblpEntryParentAndName(dblpKey)
	if m[parentDir] == nil {
		m[parentDir] = make(map[string]TDblpManifestEntry)
	}
	m[parentDir][entryName] = TDblpManifestEntry{Mdate: mdate}
}

// get returns the manifest entry for dblpKey and whether it was found.
func (m TDblpManifest) get(dblpKey string) (TDblpManifestEntry, bool) {
	parentDir, entryName := dblpEntryParentAndName(dblpKey)
	if entries, ok := m[parentDir]; ok {
		e, found := entries[entryName]
		return e, found
	}
	return TDblpManifestEntry{}, false
}

func dblpEntriesManifestPath() string { return dblpFolder() + "entries.manifest" }

// loadDblpManifests reads the single entries.manifest file at dblpFolder().
// Falls back to the legacy per-directory manifest.csv walk when the file is
// absent, writing the new file immediately so subsequent loads are fast.
func loadDblpManifests() TDblpManifest {
	data, err := os.ReadFile(dblpEntriesManifestPath())
	if err != nil {
		m := loadDblpManifestsLegacy()
		if len(m) > 0 {
			writeDblpManifests(m)
		}
		return m
	}
	m := make(TDblpManifest)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		i := strings.IndexByte(line, ',')
		if i < 0 {
			m.add(line, "")
			continue
		}
		m.add(line[:i], line[i+1:])
	}
	return m
}

// loadDblpManifestsLegacy reads the old per-directory manifest.csv files.
// Used only as a one-time migration path from loadDblpManifests.
func loadDblpManifestsLegacy() TDblpManifest {
	m := make(TDblpManifest)
	entriesDir := dblpFolder() + "entries/"
	filepath.WalkDir(entriesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != dblpManifestFilename {
			return nil
		}
		relDir, _ := filepath.Rel(entriesDir, filepath.Dir(path))
		parentDir := filepath.ToSlash(relDir)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line == "" {
				continue
			}
			i := strings.IndexByte(line, ',')
			if i < 0 {
				m.add(parentDir+"/"+line, "")
				continue
			}
			entryName, mdate := line[:i], line[i+1:]
			if j := strings.IndexByte(mdate, ','); j >= 0 {
				mdate = mdate[:j] // strip legacy MD5 column
			}
			if parentDir == "." {
				m.add(entryName, mdate)
			} else {
				m.add(parentDir+"/"+entryName, mdate)
			}
		}
		return nil
	})
	return m
}

// writeDblpManifests writes all manifest data to a single flat entries.manifest
// at dblpFolder(). Format: dblpKey,mdate per line, sorted.
func writeDblpManifests(m TDblpManifest) {
	keys := make([]string, 0)
	for parentDir, entries := range m {
		for entryName := range entries {
			if parentDir == "." {
				keys = append(keys, entryName)
			} else {
				keys = append(keys, parentDir+"/"+entryName)
			}
		}
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, key := range keys {
		me, _ := m.get(key)
		sb.WriteString(key)
		if me.Mdate != "" {
			sb.WriteByte(',')
			sb.WriteString(me.Mdate)
		}
		sb.WriteByte('\n')
	}
	os.WriteFile(dblpEntriesManifestPath(), []byte(sb.String()), 0644)
}

// deleteStaleDblpEntries deletes entries that appear in original but not in updated,
// then prunes any parent directories that became empty.
// Prints an in-place spinner progress line to stderr; silent when there is nothing to delete.
func deleteStaleDblpEntries(original, updated TDblpManifest) {
	// Pre-count so the progress line can show N/total.
	total := 0
	for parentDir, origEntries := range original {
		updatedEntries := updated[parentDir]
		for entryName := range origEntries {
			if _, ok := updatedEntries[entryName]; !ok {
				total++
			}
		}
	}
	if total == 0 {
		return
	}

	entriesDir := dblpFolder() + "entries/"
	ticker := dbInteraction.NewProgressTicker("Pruning stale entries", total)
	for parentDir, originalEntries := range original {
		updatedEntries := updated[parentDir]
		for entryName := range originalEntries {
			if _, ok := updatedEntries[entryName]; ok {
				continue
			}
			entryDir := entriesDir + parentDir + "/" + entryName
			dblpKey := parentDir + "/" + entryName
			removeKeyFromAllIndexes(dblpKey, readDblpJSONEntry(dblpKey))
			os.Remove(entryDir + "/data.json")
			os.Remove(entryDir) // succeeds only when empty
			if ticker.Step() {
				break
			}
		}
		if len(updatedEntries) == 0 {
			os.Remove(entriesDir + parentDir)
		}
	}
	ticker.Done()
}

// dblpEntriesDirHasContent reports whether entries/ has any subdirectories,
// without fully walking it.
func dblpEntriesDirHasContent() bool {
	entriesDir := dblpFolder() + "entries/"
	des, err := os.ReadDir(entriesDir)
	return err == nil && len(des) > 0
}
