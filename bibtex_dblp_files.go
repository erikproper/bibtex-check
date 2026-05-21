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
)

// dblpFolder returns the root of the DBLP file store.
// Falls back to bibTeXFolder+"DBLP/" when global_folder is not configured.
func dblpFolder() string {
	if globalFolder != "" {
		return globalFolder + "DBLP/"
	}
	return bibTeXFolder + "DBLP/"
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

func writeDblpEntryFile(dblpKey string, je *TDblpJSONEntry) error {
	path := dblpEntryFilePath(dblpKey)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(je)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// appendToIndexFile appends a single line to a hash-keyed index file,
// creating parent directories as needed.
func appendToIndexFile(path, line string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
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

// jsonEntryToLibEntry converts a TDblpJSONEntry to a TBibTeXEntry.
func jsonEntryToLibEntry(dblpKey string, je *TDblpJSONEntry) *TBibTeXEntry {
	entry := &TBibTeXEntry{Key: KeyForDBLP(dblpKey), Fields: map[string]string{}}
	entry.Fields[EntryTypeField] = je.EntryType
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

// --- Manifest ---

const dblpManifestFilename = "manifest.csv"

// TDblpManifest maps parent directories (relative to entries/) to entry names with their mdate.
// e.g. m["conf/ijcai"]["2023"] = "2024-01-15" for DBLP key "conf/ijcai/2023".
// An empty mdate string means the mdate is unknown (e.g. after a key-only repair).
type TDblpManifest map[string]map[string]string

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
		m[parentDir] = make(map[string]string)
	}
	m[parentDir][entryName] = mdate
}

// mdate returns the stored mdate for dblpKey and whether it was found in the manifest.
func (m TDblpManifest) mdate(dblpKey string) (string, bool) {
	parentDir, entryName := dblpEntryParentAndName(dblpKey)
	if entries, ok := m[parentDir]; ok {
		mdate, found := entries[entryName]
		return mdate, found
	}
	return "", false
}

// loadDblpManifests reads all manifest.csv files under entries/ into memory.
// Returns an empty manifest when none are found.
func loadDblpManifests() TDblpManifest {
	m := make(TDblpManifest)
	entriesDir := dblpFolder() + "entries/"
	filepath.WalkDir(entriesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != dblpManifestFilename {
			return nil
		}
		relDir, _ := filepath.Rel(entriesDir, filepath.Dir(path))
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		entries := make(map[string]string)
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line == "" {
				continue
			}
			if i := strings.IndexByte(line, ','); i >= 0 {
				entries[line[:i]] = line[i+1:] // name,mdate format
			} else {
				entries[line] = "" // old format — mdate unknown
			}
		}
		m[filepath.ToSlash(relDir)] = entries
		return nil
	})
	return m
}

// writeDblpManifests writes one manifest.csv per parent directory in m.
func writeDblpManifests(m TDblpManifest) {
	entriesDir := dblpFolder() + "entries/"
	for parentDir, entries := range m {
		if len(entries) == 0 {
			continue
		}
		names := make([]string, 0, len(entries))
		for name := range entries {
			names = append(names, name)
		}
		sort.Strings(names)
		var sb strings.Builder
		for _, name := range names {
			sb.WriteString(name)
			if mdate := entries[name]; mdate != "" {
				sb.WriteByte(',')
				sb.WriteString(mdate)
			}
			sb.WriteByte('\n')
		}
		os.WriteFile(entriesDir+parentDir+"/"+dblpManifestFilename, []byte(sb.String()), 0644)
	}
}

// deleteStaleDblpEntries deletes entries that appear in original but not in updated,
// then prunes any parent directories that became empty.
func deleteStaleDblpEntries(original, updated TDblpManifest) {
	entriesDir := dblpFolder() + "entries/"
	for parentDir, originalEntries := range original {
		updatedEntries := updated[parentDir]
		for entryName := range originalEntries {
			if _, ok := updatedEntries[entryName]; ok {
				continue
			}
			entryDir := entriesDir + parentDir + "/" + entryName
			os.Remove(entryDir + "/data.json")
			os.Remove(entryDir) // succeeds only when empty
		}
		if len(updatedEntries) == 0 {
			os.Remove(entriesDir + parentDir + "/" + dblpManifestFilename)
			os.Remove(entriesDir + parentDir)
		}
	}
}

// rebuildDblpManifests walks entries/ and recreates all manifest.csv files from
// existing data.json files. Used when manifests are absent or corrupted.
func rebuildDblpManifests() error {
	entriesDir := dblpFolder() + "entries/"
	m := make(TDblpManifest)
	err := filepath.WalkDir(entriesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "data.json" {
			return nil
		}
		relEntry, _ := filepath.Rel(entriesDir, filepath.Dir(path))
		relEntry = filepath.ToSlash(relEntry)
		parentDir, entryName := dblpEntryParentAndName(relEntry)
		mdate := ""
		if je := readDblpJSONEntry(relEntry); je != nil {
			mdate = je.Mdate
		}
		if m[parentDir] == nil {
			m[parentDir] = make(map[string]string)
		}
		m[parentDir][entryName] = mdate
		return nil
	})
	if err != nil {
		return err
	}
	writeDblpManifests(m)
	return nil
}

// dblpEntriesDirHasContent reports whether entries/ has any subdirectories,
// without fully walking it.
func dblpEntriesDirHasContent() bool {
	entriesDir := dblpFolder() + "entries/"
	des, err := os.ReadDir(entriesDir)
	return err == nil && len(des) > 0
}
