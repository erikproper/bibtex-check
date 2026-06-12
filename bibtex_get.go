/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: Sync
 *
 * Implements -sync (primary) and the deprecated -get alias.
 *
 * -sync dispatches on the "mode" field in the sync config:
 *   "full"  — writes the entire library to the configured bib file
 *   ""/"pull" — subset export: reads <file_name>.map and writes matching entries
 *
 * Config is read from (in order):
 *   <base>.exchange/<basename>.config
 *   <base>.exchange/bib.config
 *   CWD/bib.config   (backward compat for the deprecated -get invocation)
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 01.06.2026
 *
 */

package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TBibGetConfig mirrors the sync config JSON file.
type TBibGetConfig struct {
	Mode          string `json:"mode"`              // "full", "pull", or "" (default = pull)
	FileNames     string `json:"file_names"`        // canonical: semicolon-separated list of file names
	FileName      string `json:"file_name,omitempty"` // legacy alias for file_names (read-only); also used programmatically for single-file ops
	KeyMapping    bool   `json:"key_mapping"`       // true (default): use aliases from .keys; false: use canonical keys
	IncludeDOI    bool   `json:"include_doi"`
	IncludeISBN   bool   `json:"include_isbn"`
	IncludeDblp   bool   `json:"include_dblp"`
	BiberMode     bool   `json:"biber_mode"`
	Shorten       bool   `json:"shorten"`
	ShortenFile   string `json:"shorten_file"`
	IncludeURL    bool   `json:"include_url"`
	UrldateAsNote bool   `json:"urldate_as_note"`
	Hyphenations  bool   `json:"hyphenations"`   // insert \- hints from global_folder/hyphenations.csv
	TrustHints    bool   `json:"trust_hints"`    // harvest: auto-accept key-hint matches without confirmation
	CollectKeys   bool   `json:"collect_keys"`   // harvest: add source keys to hints DB when unambiguous
	TrustedSubset bool   `json:"trusted_subset"` // subset: apply changes/adds/deletes without confirmation
	PDFFiles      string `json:"pdf_files"`      // subset/full: "" | "global" | "local"
}

// migrateRawConfigFileNames migrates "file_name" → "file_names" in a raw JSON map.
// When both keys are present their values are joined with ";".
// Returns true when the map was modified (write-back needed).
func migrateRawConfigFileNames(rawMap map[string]json.RawMessage) bool {
	legacyVal, hasLegacy := rawMap["file_name"]
	if !hasLegacy {
		return false
	}
	var legacy string
	json.Unmarshal(legacyVal, &legacy)
	if newVal, hasNew := rawMap["file_names"]; hasNew {
		var current string
		json.Unmarshal(newVal, &current)
		combined := strings.Trim(current+";"+legacy, ";")
		rawMap["file_names"], _ = json.Marshal(combined)
	} else {
		rawMap["file_names"] = legacyVal
	}
	delete(rawMap, "file_name")
	return true
}

// readBibGetConfig reads bib.config from the current working directory.
// Migrates "file_name" → "file_names" on first use (writes back the updated file).
// Fields absent from the JSON keep their defaults: include_doi=true,
// include_isbn=true, include_url=true, key_mapping=true, biber_mode=false,
// shorten=false, include_dblp=false, urldate_as_note=false.
func readBibGetConfig() (TBibGetConfig, bool) {
	const path = "bib.config"
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot read bib.config:", err)
		return TBibGetConfig{}, false
	}
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawMap); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot parse bib.config:", err)
		return TBibGetConfig{}, false
	}
	needsWriteBack := migrateRawConfigFileNames(rawMap)

	// Seed all absent option fields so bib.config is fully self-documenting.
	for _, opt := range []struct {
		key string
		val json.RawMessage
	}{
		{"key_mapping", json.RawMessage(`true`)},
		{"include_doi", json.RawMessage(`true`)},
		{"include_isbn", json.RawMessage(`true`)},
		{"include_url", json.RawMessage(`true`)},
		{"include_dblp", json.RawMessage(`false`)},
		{"biber_mode", json.RawMessage(`false`)},
		{"shorten", json.RawMessage(`false`)},
		{"urldate_as_note", json.RawMessage(`false`)},
		{"hyphenations", json.RawMessage(`false`)},
		{"trust_hints", json.RawMessage(`false`)},
		{"collect_keys", json.RawMessage(`false`)},
	} {
		if _, present := rawMap[opt.key]; !present {
			rawMap[opt.key] = opt.val
			needsWriteBack = true
		}
	}

	if needsWriteBack {
		if written, marshalErr := json.MarshalIndent(rawMap, "", "  "); marshalErr == nil {
			os.WriteFile(path, append(written, '\n'), 0644)
			data = written
		}
	}
	cfg := TBibGetConfig{
		KeyMapping:  true,
		IncludeDOI:  true,
		IncludeISBN: true,
		IncludeURL:  true,
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot parse bib.config:", err)
		return TBibGetConfig{}, false
	}
	if cfg.FileNames == "" {
		fmt.Fprintln(os.Stderr, "bib.config: file_names is required")
		return TBibGetConfig{}, false
	}
	return cfg, true
}

// TBibGetPair holds one row from the .keys file.
// When localKey is empty, the entry is a bare canonical key pending alias resolution.
type TBibGetPair struct {
	localKey     string
	canonicalKey string
}

// readKeysFile reads <fileName>.keys (local_key;canonical_key CSV).
// Lines without a ';' are bare canonical keys; localKey is left empty and
// the caller must resolve them against the open library.
// modified is true when a bare-key line was found (requiring a write-back).
// Migration from the legacy .map extension is handled externally by bib.sync.
func readKeysFile(fileName string) (pairs []TBibGetPair, modified bool, ok bool) {
	keysPath := fileName + KeysFileExtension

	if !FileExists(keysPath) {
		return nil, false, true // absent is valid — treat as empty
	}

	rawCanonicalSeen := map[string]bool{}

	ok = processFile(keysPath, func(line string) {
		if !strings.Contains(line, csvDelimiter) {
			// Legacy space delimiter
			if strings.Contains(line, " ") {
				parts := strings.SplitN(line, " ", 2)
				local := strings.TrimSpace(parts[0])
				canonical := strings.TrimSpace(parts[1])
				if local == "" || canonical == "" {
					return
				}
				if rawCanonicalSeen[canonical] {
					fmt.Fprintf(os.Stderr, "WARNING: %s: %q appears more than once (first kept)\n", keysPath, canonical)
					return
				}
				rawCanonicalSeen[canonical] = true
				pairs = append(pairs, TBibGetPair{local, canonical})
				modified = true
				return
			}
			// Bare canonical key — will be resolved to alias;canonical by caller.
			bare := strings.TrimSpace(line)
			if bare != "" {
				pairs = append(pairs, TBibGetPair{"", bare})
				modified = true
			}
			return
		}
		parts := strings.SplitN(line, csvDelimiter, 2)
		if len(parts) != 2 {
			return
		}
		local := strings.TrimSpace(parts[0])
		canonical := strings.TrimSpace(parts[1])
		if local == "" || canonical == "" {
			return
		}
		if rawCanonicalSeen[canonical] {
			fmt.Fprintf(os.Stderr, "WARNING: %s: %q appears more than once (first kept)\n", keysPath, canonical)
			return
		}
		rawCanonicalSeen[canonical] = true
		pairs = append(pairs, TBibGetPair{local, canonical})
	})

	return
}

// rewriteKeysFile writes pairs back to <fileName>.keys.
// When keyMapping is true, each line is localKey;canonicalKey.
// When keyMapping is false, each line is canonicalKey only.
func rewriteKeysFile(fileName string, pairs []TBibGetPair, keyMapping bool) {
	path := fileName + KeysFileExtension
	FileRename(path, path+".old")
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot rewrite keys file:", err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, p := range pairs {
		if keyMapping {
			w.WriteString(p.localKey + csvDelimiter + p.canonicalKey + "\n")
		} else {
			w.WriteString(p.canonicalKey + "\n")
		}
	}
	w.Flush()
}

// TSelectStatement is one parsed statement from a .select file.
type TSelectStatement struct {
	Kind   string   // "group", "groups", "name", "orcid"
	Values []string // one or more quoted values
}

// readSelectFile parses <fileName>.select. Each statement is:
//   group   "name";
//   groups  "name1" "name2";
//   name    "Canonical Author Name";
//   orcid   "0000-0001-2345-6789";
// Blank lines and lines starting with # are ignored.
// Returns (statements, fileExists).
func readSelectFile(fileName string) ([]TSelectStatement, bool) {
	selectPath := fileName + ".select"
	if !FileExists(selectPath) {
		return nil, false
	}
	var stmts []TSelectStatement
	var badLines []string
	processFile(selectPath, func(line string) {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ";"))
		if line == "" || strings.HasPrefix(line, "#") {
			return
		}
		idx := strings.IndexByte(line, '"')
		if idx < 0 {
			badLines = append(badLines, line)
			return
		}
		kind := strings.TrimSpace(line[:idx])
		rest := line[idx:]
		var values []string
		for {
			start := strings.IndexByte(rest, '"')
			if start < 0 {
				break
			}
			rest = rest[start+1:]
			end := strings.IndexByte(rest, '"')
			if end < 0 {
				break
			}
			v := rest[:end]
			if v != "" {
				values = append(values, v)
			}
			rest = rest[end+1:]
		}
		if kind != "" && len(values) > 0 {
			stmts = append(stmts, TSelectStatement{kind, values})
		} else {
			badLines = append(badLines, line)
		}
	})
	for _, bl := range badLines {
		fmt.Fprintf(os.Stderr, "WARNING: %s: unrecognised line (expected: kind \"value\";): %q\n", selectPath, bl)
	}
	return stmts, true
}

// expandSelectStmts resolves .select statements into a set of canonical library keys,
// excluding any keys already present in the explicit keys set.
func expandSelectStmts(stmts []TSelectStatement, alreadyIncluded map[string]bool) []string {
	seen := map[string]bool{}
	var extra []string
	add := func(key string) {
		if !alreadyIncluded[key] && !seen[key] {
			seen[key] = true
			extra = append(extra, key)
		}
	}
	for _, s := range stmts {
		switch s.Kind {
		case "group", "groups":
			for _, grp := range s.Values {
				grpSet := Library.GroupEntries[grp]
				for key := range grpSet.Elements() {
					resolved := Library.MapEntryKey(key)
					if resolved == "" {
						resolved = key
					}
					add(resolved)
				}
			}
		case "name":
			for _, name := range s.Values {
				orcid := resolveNameToORCID(name)
				var dblpKeys []string
				if orcid != "" {
					dblpKeys = readDblpORCIDEntries(orcid)
				} else {
					dblpKeys = readDblpPersonEntries(name)
				}
				for _, dk := range dblpKeys {
					if libKey := Library.LookupDBLPKey(dk); libKey != "" {
						add(libKey)
					}
				}
			}
		case "orcid":
			for _, orcid := range s.Values {
				for _, dk := range readDblpORCIDEntries(orcid) {
					if libKey := Library.LookupDBLPKey(dk); libKey != "" {
						add(libKey)
					}
				}
			}
		}
	}
	return extra
}

// TShortenMappings maps field name to an ordered list of (from, to) pairs.
type TShortenMappings map[string][][2]string

// readShortenMappingsFile reads a shorten-mappings CSV from the given file path.
func readShortenMappingsFile(path string) TShortenMappings {
	result := TShortenMappings{}
	processFile(path, func(line string) {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			return
		}
		parts := strings.SplitN(line, csvDelimiter, 3)
		if len(parts) != 3 {
			return
		}
		field := strings.TrimSpace(parts[0])
		from := strings.TrimSpace(parts[1])
		to := strings.TrimSpace(parts[2])
		if field == "" || from == "" {
			return
		}
		result[field] = append(result[field], [2]string{from, to})
	})
	return result
}

// readShortenMappings loads shorten_mappings from the DB, reloading from the global
// shorten_mappings.csv first if the file is newer than the cached table timestamp.
func readShortenMappings() TShortenMappings {
	maybeReloadShortenMappingsDb()
	return loadShortenMappingsFromDb()
}

// applyShorten applies shorten_mappings substitutions to a field value.
func applyShorten(mappings TShortenMappings, field, value string) string {
	for _, pair := range mappings[field] {
		value = strings.ReplaceAll(value, pair[0], pair[1])
	}
	return value
}

// mergeShortenMappings merges override into base: for each field, override entries
// replace base entries with the same "from" value; remaining base entries are kept first.
func mergeShortenMappings(base, override TShortenMappings) TShortenMappings {
	result := TShortenMappings{}
	for field, pairs := range base {
		overrideFroms := map[string]bool{}
		for _, p := range override[field] {
			overrideFroms[p[0]] = true
		}
		for _, p := range pairs {
			if !overrideFroms[p[0]] {
				result[field] = append(result[field], p)
			}
		}
	}
	for field, pairs := range override {
		result[field] = append(result[field], pairs...)
	}
	return result
}

// THyphenations maps lowercase word → word-with-\- hints (as stored in hyphenations.csv).
type THyphenations map[string]string

// readHyphenations loads hyphenations.csv from global_folder.
// Each line contains only the hinted form with \- markers; the original word
// is derived by stripping \- (e.g. "ma\-ni\-fe\-sto" → key "manifesto").
// Blank lines and lines starting with # are ignored.
func readHyphenations() THyphenations {
	result := THyphenations{}
	path := globalFolder + "hyphenations.csv"
	if !FileExists(path) {
		return result
	}
	var bad []string
	processFile(path, func(line string) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return
		}
		stripped := strings.ReplaceAll(line, `\-`, "")
		if stripped == line {
			bad = append(bad, line)
			return
		}
		if stripped == "" {
			bad = append(bad, line)
			return
		}
		key := strings.ToLower(stripped)
		if existing, dup := result[key]; dup {
			fmt.Fprintf(os.Stderr, "WARNING: %s: duplicate entry for %q — keeping %q, ignoring %q\n", path, stripped, existing, line)
			return
		}
		result[key] = line
	})
	for _, bl := range bad {
		fmt.Fprintf(os.Stderr, "WARNING: %s: line has no \\- markers — skipped: %q\n", path, bl)
	}
	dbInteraction.Progress("Hyphenations: %d entr%s from %s", len(result), map[bool]string{true: "y", false: "ies"}[len(result) == 1], path)
	return result
}

// splitWordWrappers separates leading non-letter characters (e.g. `{`), the
// bare alphabetic core, and trailing non-letter characters (e.g. `},`).
func splitWordWrappers(word string) (prefix, bare, suffix string) {
	start := 0
	for start < len(word) {
		r, size := utf8.DecodeRuneInString(word[start:])
		if unicode.IsLetter(r) {
			break
		}
		start += size
	}
	end := len(word)
	for end > start {
		r, size := utf8.DecodeLastRuneInString(word[:end])
		if unicode.IsLetter(r) {
			break
		}
		end -= size
	}
	return word[:start], word[start:end], word[end:]
}

// applyHyphenation replaces words in value with their \- hinted forms.
// Leading/trailing wrapper characters (braces, punctuation) are preserved.
// Matching is case-insensitive; the case of the first letter is preserved.
func applyHyphenation(h THyphenations, value string) string {
	if len(h) == 0 {
		return value
	}
	words := strings.Fields(value)
	for i, word := range words {
		prefix, bare, suffix := splitWordWrappers(word)
		if bare == "" {
			continue
		}
		if hinted, ok := h[strings.ToLower(bare)]; ok {
			if len(bare) > 0 && unicode.IsUpper(rune(bare[0])) {
				r := []rune(hinted)
				if len(r) > 0 && unicode.IsLower(r[0]) {
					r[0] = unicode.ToUpper(r[0])
					hinted = string(r)
				}
			}
			words[i] = prefix + hinted + suffix
		}
	}
	return strings.Join(words, " ")
}

// hyphenateFields is the set of BibTeX fields where \- hints are applied.
// Limited to title fields where long compound words may cause line overflow.
var hyphenateFields = func() TStringSet {
	s := TStringSetNew()
	s.Add("title", "booktitle", "journal")
	return s
}()

// biberMonth maps full month names to biber abbreviations.
var biberMonth = map[string]string{
	"January":   "jan",
	"February":  "feb",
	"March":     "mar",
	"April":     "apr",
	"May":       "may",
	"June":      "jun",
	"July":      "jul",
	"August":    "aug",
	"September": "sep",
	"October":   "oct",
	"November":  "nov",
	"December":  "dec",
}

// biberEditionOrdinals maps ordinal strings to numeric strings.
var biberEditionOrdinals = map[string]string{
	"1st": "1", "2nd": "2", "3rd": "3", "4th": "4", "5th": "5",
	"6th": "6", "7th": "7", "8th": "8", "9th": "9", "10th": "10",
}

// applyBiberMode converts month and edition values to biber-friendly form.
func applyBiberMode(field, value string) string {
	switch field {
	case "month":
		if abbr, ok := biberMonth[value]; ok {
			return abbr
		}
	case "edition":
		if num, ok := biberEditionOrdinals[value]; ok {
			return num
		}
	}
	return value
}

// bibEditorNoiseFields is the set of fields excluded from all content fingerprints
// (harvest, subset sync). Includes editor-injected housekeeping fields and local-url,
// which is derived from file existence on disk rather than bibliographic content.
var bibEditorNoiseFields = func() TStringSet {
	s := TStringSetNew()
	s.Add(
		LocalURLField,
		"date-added", "date-modified",
		"owner", "creationdate", "modificationdate",
		JabrefFileField,
		"bdsk-url-1", "bdsk-url-2", "bdsk-url-3", "bdsk-url-4", "bdsk-url-5",
		"bdsk-url-6", "bdsk-url-7", "bdsk-url-8", "bdsk-url-9",
		"bdsk-file-1", "bdsk-file-2", "bdsk-file-3", "bdsk-file-4", "bdsk-file-5",
		"bdsk-file-6", "bdsk-file-7", "bdsk-file-8", "bdsk-file-9",
	)
	return s
}()

// bibContentFingerprint returns an MD5 of all bibliographically meaningful field=value
// pairs in e. Fields are sorted before hashing so the result is independent of Go's
// random map iteration order. Editor-injected noise fields are excluded so that
// BibDesk/JabRef housekeeping writes do not falsely register as content changes.
// Used by both harvest and subset-sync change detection.
func bibContentFingerprint(e TBibTeXEntry) string {
	fields := make([]string, 0, len(e.Fields))
	for f, v := range e.Fields {
		if v != "" && !bibEditorNoiseFields.Contains(f) {
			fields = append(fields, f+"="+v)
		}
	}
	sort.Strings(fields)
	h := md5.Sum([]byte(strings.Join(fields, ";")))
	return hex.EncodeToString(h[:])
}

// bibGetNonExportFields is the set of fields never written to a get-output file.
// local-url and preferredalias are excluded here but allowed through for mode="subset"
// via explicit per-field checks in entryGetString.
var bibGetNonExportFields = func() TStringSet {
	s := TStringSetNew()
	s.Add(
		GroupsField, EntryTypeField,
		"date-added", "date-modified",
		"researchgate", "abstract", "ketwords", "repositum",
		"owner", "creationdate", "modificationdate", JabrefFileField,
		"bdsk-url-1", "bdsk-url-2", "bdsk-url-3", "bdsk-url-4", "bdsk-url-5",
		"bdsk-url-6", "bdsk-url-7", "bdsk-url-8", "bdsk-url-9",
		"bdsk-file-1", "bdsk-file-2", "bdsk-file-3", "bdsk-file-4", "bdsk-file-5",
		"bdsk-file-6", "bdsk-file-7", "bdsk-file-8", "bdsk-file-9",
	)
	return s
}()

// entryGetString produces the BibTeX export string for one entry.
// outputKey is the local key used as the @Type{key} identifier.
// crossrefLocalKey is the local key of the crossref parent (empty if none).
func (l *TBibTeXLibrary) entryGetString(
	canonicalKey, outputKey, crossrefLocalKey string,
	cfg TBibGetConfig,
	shorten TShortenMappings,
	hyphenations THyphenations,
) string {
	entry := loadEntryFromDb(canonicalKey)
	if !entry.Exists() {
		return ""
	}

	result := "@" + entry.EntryType() + "{" + outputKey + ",\n"

	// When urldate_as_note is set, fold urldate into the note field.
	var urldateNote string
	if cfg.UrldateAsNote {
		if urldate := entry.FieldValue("urldate"); urldate != "" {
			if note := entry.FieldValue("note"); note == "" {
				urldateNote = "Last visited on: " + urldate
			} else {
				urldateNote = note + ", last visited on: " + urldate
			}
		}
	}

	isSubset := cfg.Mode == "subset"

	for _, field := range BibTeXAllowedEntryFields[entry.EntryType()].Set().ElementsSorted() {
		if field == EntryTypeField {
			continue
		}
		if !isSubset {
			if bibGetNonExportFields.Contains(field) {
				continue
			}
			if field == LocalURLField || field == PreferredAliasField {
				continue
			}
			if !cfg.IncludeDOI && field == "doi" {
				continue
			}
			if !cfg.IncludeISBN && (field == "isbn" || field == "issn") {
				continue
			}
			if !cfg.IncludeDblp && field == DBLPField {
				continue
			}
		}

		value := entry.FieldValue(field)
		if value == "" {
			continue
		}

		// URL handling: when include_url is false, skip url unless urldate is present.
		if !isSubset && field == "url" && !cfg.IncludeURL {
			if entry.FieldValue("urldate") == "" {
				continue
			}
		}

		// urldate_as_note: suppress urldate and original note (written merged after loop).
		if cfg.UrldateAsNote && urldateNote != "" && (field == "urldate" || field == "note") {
			continue
		}

		// crossref: write the local key directly — skip MapEntryFieldValue because
		// it would resolve the local key back to the canonical via MapEntryKey.
		if field == "crossref" {
			if crossrefLocalKey != "" {
				result += FormatBibTeXFieldAssignment("", field, crossrefLocalKey)
			}
			continue
		}

		if cfg.BiberMode {
			value = applyBiberMode(field, value)
		}
		if cfg.Shorten {
			value = applyShorten(shorten, field, value)
		}
		if cfg.Hyphenations && hyphenateFields.Contains(field) {
			value = applyHyphenation(hyphenations, value)
		}

		mapped := l.MapEntryFieldValue(canonicalKey, field, value)
		if field == LocalURLField {
			mapped = l.resolvedLocalURL(mapped)
		}
		result += FormatBibTeXFieldAssignment("", field, mapped)
	}

	if urldateNote != "" {
		result += FormatBibTeXFieldAssignment("", "note", urldateNote)
	}

	result += "}\n"
	return result
}

// doGetWithConfig implements the subset bib export with a pre-read config.
// baseDir is the directory used to resolve relative file_name paths; pass ""
// to resolve relative to the current working directory (deprecated -get behaviour).
// writePullSync performs the subset bib export assuming the library is already open.
// Returns the full list of (canonicalKey, outputKey) pairs written, including
// .select extras and auto-added crossref parents, for use by subset-sync fingerprinting.
func writePullSync(cfg TBibGetConfig, baseDir string) []TBibGetPair {
	resolveRelative := func(path string) string {
		if filepath.IsAbs(path) {
			return path
		}
		if baseDir != "" {
			return filepath.Join(baseDir, path)
		}
		cwd, err := os.Getwd()
		if err == nil {
			return filepath.Join(cwd, path)
		}
		return path
	}

	// Resolve the keys file path (base name without extension).
	mapFilePath := resolveRelative(cfg.FileName)

	pairs, keysModified, ok := readKeysFile(mapFilePath)
	if !ok {
		fmt.Fprintln(os.Stderr, "Cannot read keys file:", mapFilePath+KeysFileExtension)
		return nil
	}

	// Resolve all canonical keys through the key alias / oldies table.
	// Bare keys (localKey == "") are resolved to their preferred alias when
	// key_mapping is on; in no-mapping mode bare keys stay as canonical keys.
	resolvedSeen := map[string]bool{}
	for i, p := range pairs {
		resolved := Library.MapEntryKey(p.canonicalKey)
		if resolved == "" {
			resolved = p.canonicalKey
		}
		pairs[i].canonicalKey = resolved
		if p.localKey == "" && cfg.KeyMapping {
			// Bare key: assign preferred alias (or canonical if none exists).
			preferred := Library.PreferredKey(resolved)
			if preferred == "" {
				preferred = resolved
			}
			pairs[i].localKey = preferred
			keysModified = true
		}
		if pairs[i].localKey != p.localKey || pairs[i].canonicalKey != p.canonicalKey {
			keysModified = true
		}
		// When key_mapping is off, any two-column pair must be rewritten as single-column.
		if !cfg.KeyMapping && p.localKey != "" {
			keysModified = true
		}
		// Homonym check: warn when the same canonical maps to a different local key.
		if existing, seen := resolvedSeen[resolved]; seen {
			_ = existing
			fmt.Fprintf(os.Stderr, "WARNING: %s: canonical key %q appears more than once (first kept)\n", mapFilePath+KeysFileExtension, resolved)
			pairs[i] = TBibGetPair{} // mark as skip
			continue
		}
		resolvedSeen[resolved] = true
	}
	// Remove skip-marked entries.
	filtered := pairs[:0]
	for _, p := range pairs {
		if p.canonicalKey != "" {
			filtered = append(filtered, p)
		}
	}
	pairs = filtered

	if keysModified {
		rewriteKeysFile(mapFilePath, pairs, cfg.KeyMapping)
	}

	// Expand .select statements into additional canonical keys.
	selectStmts, selectFileFound := readSelectFile(mapFilePath)
	explicitKeys := map[string]bool{}
	for _, p := range pairs {
		explicitKeys[p.canonicalKey] = true
	}
	extraCanonicals := expandSelectStmts(selectStmts, explicitKeys)

	// Progress: file name, all options, source counts.
	{
		on := func(b bool) string {
			if b {
				return "on"
			}
			return "off"
		}
		dbInteraction.Progress("Sync pull: %s", cfg.FileName)
		dbInteraction.Progress("  doi=%-3s  isbn=%-3s  url=%-3s  dblp=%-3s  key_mapping=%-3s",
			on(cfg.IncludeDOI), on(cfg.IncludeISBN), on(cfg.IncludeURL), on(cfg.IncludeDblp), on(cfg.KeyMapping))
		dbInteraction.Progress("  biber=%-3s  shorten=%-3s  urldate_as_note=%-3s  hyphenations=%-3s",
			on(cfg.BiberMode), on(cfg.Shorten), on(cfg.UrldateAsNote), on(cfg.Hyphenations))
		dbInteraction.Progress("  Keys  : %d entr%s from %s", len(pairs), map[bool]string{true: "y", false: "ies"}[len(pairs) == 1], mapFilePath+KeysFileExtension)
		if selectFileFound {
			dbInteraction.Progress("  Select: %d statement(s) → %d extra entr%s from %s", len(selectStmts), len(extraCanonicals), map[bool]string{true: "y", false: "ies"}[len(extraCanonicals) == 1], mapFilePath+".select")
		} else {
			dbInteraction.Progress("  Select: %s not found (no .select file)", mapFilePath+".select")
		}
	}

	var shorten TShortenMappings
	if cfg.Shorten {
		shorten = readShortenMappings()
		if cfg.ShortenFile != "" {
			localPath := resolveRelative(cfg.ShortenFile)
			shorten = mergeShortenMappings(shorten, readShortenMappingsFile(localPath))
		}
	}

	var hyphenations THyphenations
	if cfg.Hyphenations {
		hyphenations = readHyphenations()
	}

	// Build canonical->outputKey index from explicit .keys pairs.
	// When key_mapping=false, canonical keys are used as output keys throughout.
	canonicalToLocal := map[string]string{}
	for _, p := range pairs {
		outputKey := p.localKey
		if !cfg.KeyMapping {
			outputKey = p.canonicalKey
		}
		canonicalToLocal[p.canonicalKey] = outputKey
	}

	// Convert .select extras to TBibGetPair and register in canonicalToLocal.
	var extraPairs []TBibGetPair
	for _, canonical := range extraCanonicals {
		outputKey := canonical
		if cfg.KeyMapping {
			if preferred := Library.PreferredKey(canonical); preferred != "" {
				outputKey = preferred
			}
		}
		extraPairs = append(extraPairs, TBibGetPair{outputKey, canonical})
		canonicalToLocal[canonical] = outputKey
	}

	// Collect crossref parents not already covered and auto-add them.
	type autoParent struct {
		localKey     string
		canonicalKey string
	}
	var autoParents []autoParent
	autoParentLocal := map[string]string{}

	allCoveredCanonicalsIncludingExtras := make([]string, 0, len(canonicalToLocal))
	for c := range canonicalToLocal {
		allCoveredCanonicalsIncludingExtras = append(allCoveredCanonicalsIncludingExtras, c)
	}
	for _, resolved := range allCoveredCanonicalsIncludingExtras {
		crossref := Library.EntryFieldValueity(resolved, "crossref")
		if crossref == "" {
			continue
		}
		resolvedCrossref := Library.MapEntryKey(crossref)
		if resolvedCrossref == "" {
			resolvedCrossref = crossref
		}
		if _, inMap := canonicalToLocal[resolvedCrossref]; inMap {
			continue
		}
		if _, alreadyAuto := autoParentLocal[resolvedCrossref]; alreadyAuto {
			continue
		}
		localKey := resolvedCrossref
		if cfg.KeyMapping {
			if preferred := Library.PreferredKey(resolvedCrossref); preferred != "" {
				localKey = preferred
			}
		}
		autoParentLocal[resolvedCrossref] = localKey
		autoParents = append(autoParents, autoParent{localKey, resolvedCrossref})
	}

	// effectiveLocalKey returns the output key for a canonical used in a crossref field.
	effectiveLocalKey := func(canonical string) string {
		if lk, ok := canonicalToLocal[canonical]; ok {
			return lk
		}
		if lk, ok := autoParentLocal[canonical]; ok {
			return lk
		}
		return canonical
	}

	// Write output file.
	outPath := resolveRelative(cfg.FileName + BibFileExtension)

	Library.localURLBase = Library.FilesRoot
	defer func() { Library.localURLBase = "" }()

	// Build new content into a buffer so we can MD5 it before touching the file.
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	if cfg.Mode != "subset" {
		w.WriteString("%\n% THIS FILE IS AUTOMATICALLY GENERATED.\n% THEREFORE, DO NOT EDIT THIS FILE!!\n%\n\n")
	}

	// writeEntry emits one entry, resolving its crossref to an output key.
	writeEntry := func(canonical, outputKey string) {
		crossref := Library.EntryFieldValueity(canonical, "crossref")
		crossrefLocal := ""
		if crossref != "" {
			resolvedCrossref := Library.MapEntryKey(crossref)
			if resolvedCrossref == "" {
				resolvedCrossref = crossref
			}
			crossrefLocal = effectiveLocalKey(resolvedCrossref)
		}
		w.WriteString(Library.entryGetString(canonical, outputKey, crossrefLocal, cfg, shorten, hyphenations))
		w.WriteString("\n")
	}

	// Non-bookish entries first (children before parents): explicit .keys pairs then .select extras.
	for _, p := range pairs {
		if !BibTeXBookish.Contains(Library.EntryType(p.canonicalKey)) {
			writeEntry(p.canonicalKey, canonicalToLocal[p.canonicalKey])
		}
	}
	for _, p := range extraPairs {
		if !BibTeXBookish.Contains(Library.EntryType(p.canonicalKey)) {
			writeEntry(p.canonicalKey, p.localKey)
		}
	}

	// Bookish entries: explicit .keys pairs then .select extras.
	for _, p := range pairs {
		if BibTeXBookish.Contains(Library.EntryType(p.canonicalKey)) {
			w.WriteString(Library.entryGetString(p.canonicalKey, canonicalToLocal[p.canonicalKey], "", cfg, shorten, hyphenations))
			w.WriteString("\n")
		}
	}
	for _, p := range extraPairs {
		if BibTeXBookish.Contains(Library.EntryType(p.canonicalKey)) {
			w.WriteString(Library.entryGetString(p.canonicalKey, p.localKey, "", cfg, shorten, hyphenations))
			w.WriteString("\n")
		}
	}

	// Auto-added crossref parents.
	for _, ap := range autoParents {
		w.WriteString(Library.entryGetString(ap.canonicalKey, ap.localKey, "", cfg, shorten, hyphenations))
		w.WriteString("\n")
	}

	w.Flush()
	newContent := buf.Bytes()
	h := md5.New()
	h.Write(newContent)
	newMD5 := hex.EncodeToString(h.Sum(nil))

	md5Path := outPath + ".md5"

	// For pull mode: warn if the bib was manually edited since last generation.
	// Subset mode owns its bib and always overwrites — MD5 check skipped.
	if cfg.Mode != "subset" {
		if existingData, errRead := os.ReadFile(outPath); errRead == nil {
			if storedMD5bytes, errMD5 := os.ReadFile(md5Path); errMD5 == nil {
				storedMD5 := strings.TrimSpace(string(storedMD5bytes))
				hExisting := md5.New()
				hExisting.Write(existingData)
				existingMD5 := hex.EncodeToString(hExisting.Sum(nil))
				if existingMD5 != storedMD5 {
					if !Reporting.WarningYesNoQuestion(QuestionGetBibOverwrite, WarningGetBibFileModified, outPath) {
						return nil
					}
				}
			}
		}
	}

	Reporting.Progress(ProgressWritingGetBib, outPath)

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot create output directory:", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outPath, newContent, 0644); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot write output file:", err)
		os.Exit(1)
	}

	if err := os.WriteFile(md5Path, []byte(newMD5+"\n"), 0644); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot write MD5 file:", err)
	}

	// Return only pairs that were actually written (entry exists in library).
	// Entries deleted during subset phase 1 must not appear in the keys file or state.
	allPairs := make([]TBibGetPair, 0, len(pairs)+len(extraPairs)+len(autoParents))
	for _, p := range pairs {
		if bibEntryExists(p.canonicalKey) {
			allPairs = append(allPairs, p)
		}
	}
	for _, p := range extraPairs {
		if bibEntryExists(p.canonicalKey) {
			allPairs = append(allPairs, p)
		}
	}
	for _, ap := range autoParents {
		if bibEntryExists(ap.canonicalKey) {
			allPairs = append(allPairs, TBibGetPair{ap.localKey, ap.canonicalKey})
		}
	}
	return allPairs
}

// doGetWithConfig opens the library and runs a pull sync.
func doGetWithConfig(cfg TBibGetConfig, baseDir string) {
	if !openLibraryToReport() {
		return
	}
	writePullSync(cfg, baseDir)
}

// defaultSyncConfig returns a TBibGetConfig with sensible defaults.
func defaultSyncConfig() TBibGetConfig {
	return TBibGetConfig{KeyMapping: true, IncludeDOI: true, IncludeISBN: true, IncludeURL: true}
}

// applyJSONOverlay unmarshals data into cfg, overriding only the fields present
// in the JSON. Returns false and prints an error when the JSON is malformed.
func applyJSONOverlay(cfg *TBibGetConfig, data []byte, path string) bool {
	if err := json.Unmarshal(data, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Cannot parse %s: %s\n", path, err)
		return false
	}
	return true
}


// readFileConfig reads the per-file sync config for a single bib file and overlays
// it on baseCfg. If the file exists but has no "mode" key, "pull" is written back.
// If the file does not exist, baseCfg is returned with mode defaulting to "pull".
func readFileConfig(baseCfg TBibGetConfig, name, baseDir string) (TBibGetConfig, bool) {
	cfgPath := filepath.Join(baseDir, name+ConfigFileExtension)
	cfg := baseCfg
	cfg.FileName = name // default: file name matches config name

	rawData, err := os.ReadFile(cfgPath)
	if err != nil {
		// No per-file config; default mode to "pull".
		if cfg.Mode == "" {
			cfg.Mode = "pull"
		}
		return cfg, true
	}

	// Parse raw JSON to detect absent keys before overlay.
	var rawMap map[string]json.RawMessage
	if jsonErr := json.Unmarshal(rawData, &rawMap); jsonErr != nil {
		fmt.Fprintf(os.Stderr, "Cannot parse %s: %s\n", cfgPath, jsonErr)
		return TBibGetConfig{}, false
	}

	// file_name and file_names are not meaningful in per-file configs — warn and strip.
	for _, key := range []string{"file_name", "file_names"} {
		if _, has := rawMap[key]; has {
			fmt.Fprintf(os.Stderr, "WARNING: %s: %q is not allowed in a per-file config (ignored; using %q)\n", cfgPath, key, name)
			delete(rawMap, key)
		}
	}

	needsWriteBack := false

	// Write "mode": "pull" back when absent.
	if _, hasMode := rawMap["mode"]; !hasMode {
		cfg.Mode = "pull"
		rawMap["mode"] = json.RawMessage(`"pull"`)
		needsWriteBack = true
	}

	// Rebuild clean rawData for overlay (file_name/file_names already stripped).
	cleanData, marshalErr := json.Marshal(rawMap)
	if marshalErr != nil {
		fmt.Fprintf(os.Stderr, "Cannot re-encode %s: %s\n", cfgPath, marshalErr)
		return TBibGetConfig{}, false
	}

	if !applyJSONOverlay(&cfg, cleanData, cfgPath) {
		return TBibGetConfig{}, false
	}

	// Per-file operations always use the config file's own name.
	cfg.FileName = name

	if needsWriteBack {
		if data, marshalErr2 := json.MarshalIndent(rawMap, "", "  "); marshalErr2 == nil {
			os.WriteFile(cfgPath, append(data, '\n'), 0644)
		}
	}

	return cfg, true
}

// buildSyncBibContent renders the full library to a byte slice with a progress spinner.
// Non-bookish entries first (crossref-friendly), then bookish — same order as WriteBibTeXFile.
// local-url values are written with the absolute FilesRoot prefix so consumers can locate PDFs.
func buildSyncBibContent(label string, entryTypes map[string]string) []byte {
	Library.localURLBase = Library.FilesRoot
	defer func() { Library.localURLBase = "" }()
	total := len(entryTypes)
	spinner := Library.NewSpinner(fmt.Sprintf(ProgressBuildingSyncBib, label))
	done := 0

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	for entry, entryType := range entryTypes {
		if !BibTeXBookish.Contains(entryType) {
			w.WriteString(Library.EntryString(entry, ""))
			w.WriteString("\n")
			done++
			spinner.Update(done, total)
		}
	}
	for entry, entryType := range entryTypes {
		if BibTeXBookish.Contains(entryType) {
			w.WriteString(Library.EntryString(entry, ""))
			w.WriteString("\n")
			done++
			spinner.Update(done, total)
		}
	}
	for _, comment := range Library.Comments {
		w.WriteString("@" + CommentEntryType + "{" + comment + "}\n\n")
	}
	// BibDesk static groups — identical to WriteBibTeXFile.
	if len(Library.GroupEntries) > 0 {
		w.WriteString("@" + CommentEntryType + "{BibDesk Static Groups{")
		w.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
		w.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
		w.WriteString("<plist version=\"1.0\">\n<array>\n")
		for group, keys := range Library.GroupEntries {
			w.WriteString("\t<dict>\n\t\t<key>group name</key>\n\t\t<string>" + group + "</string>\n")
			w.WriteString("\t\t<key>keys</key>\n\t\t<string>")
			comma := ""
			for key := range keys.Elements() {
				w.WriteString(comma + Library.MapEntryKey(key))
				comma = ","
			}
			w.WriteString("</string>\n\t</dict>\n")
		}
		w.WriteString("</array>\n</plist>\n}}\n\n")
	}

	w.Flush()
	spinner.Stop()
	return buf.Bytes()
}

// writeFullSync writes the full library bib file assuming the library is already open.
func writeFullSync(cfg TBibGetConfig, baseDir string) {
	fileName := cfg.FileName
	if fileName == "" {
		fileName = bibTeXBaseName
	}
	outPath := fileName + BibFileExtension
	if !filepath.IsAbs(outPath) {
		outPath = filepath.Join(baseDir, outPath)
	}

	entryTypes := map[string]string{}
	forEachBibEntryType(func(key, entryType string) {
		entryTypes[key] = entryType
	})

	newContent := buildSyncBibContent(fileName, entryTypes)
	mdatePath := outPath + ".mdate"

	// Detect manual edits: compare stored write-time against current bib mtime.
	// Two O(1) stat/read calls — no file read of the bib needed.
	bibWasEdited := false
	if bibInfo, errBib := os.Stat(outPath); errBib == nil {
		if mdateData, errMD := os.ReadFile(mdatePath); errMD == nil {
			if storedUnix, parseErr := strconv.ParseInt(strings.TrimSpace(string(mdateData)), 10, 64); parseErr == nil {
				bibWasEdited = bibInfo.ModTime().Unix() != storedUnix
			}
		}
	}

	if bibWasEdited {
		// The sync bib was edited externally (e.g. BibDesk): ask before re-importing;
		// in non-interactive mode (scripted / piped) skip re-import and just overwrite.
		doReimport := false
		if !Reporting.InteractionIsOff() {
			// Warn when the DB has also been modified since the last export, so the
			// user knows that re-importing may overwrite in-library changes.
			if mdateData, errMD := os.ReadFile(mdatePath); errMD == nil {
				if storedUnix, parseErr := strconv.ParseInt(strings.TrimSpace(string(mdateData)), 10, 64); parseErr == nil {
					if tableModTime("bib_entries") > storedUnix*1_000_000 {
						dbInteraction.Warning("DB has been modified since the last sync export — re-importing will overwrite those changes.")
					}
				}
			}
			doReimport = Reporting.ConfirmAction(fmt.Sprintf("Sync bib was edited externally — re-import %s?", outPath))
		} else {
			dbInteraction.Progress("Sync bib edited externally — overwriting without re-import: %s", outPath)
		}
		if doReimport {
			dbInteraction.Progress("Re-importing edited sync bib: %s", outPath)
			if !parseSyncBibFile(outPath) {
				fmt.Fprintf(os.Stderr, "WARNING: re-import failed for %s — skipping write\n", outPath)
				return
			}
			// Re-collect entry types from the freshly-parsed DB and regenerate content.
			entryTypes = map[string]string{}
			forEachBibEntryType(func(key, entryType string) {
				entryTypes[key] = entryType
			})
			newContent = buildSyncBibContent(fileName, entryTypes)
		}
	}

	dbInteraction.Progress("Sync full: %s → %s (%d entries)", cfg.FileName, outPath, len(entryTypes))
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot create output directory:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outPath, newContent, 0644); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot write output file:", err)
		os.Exit(1)
	}
	// Record the bib file's mtime so the next run can detect external edits cheaply.
	if bibInfo, err := os.Stat(outPath); err == nil {
		mdate := strconv.FormatInt(bibInfo.ModTime().Unix(), 10)
		if err := os.WriteFile(mdatePath, []byte(mdate+"\n"), 0644); err != nil {
			fmt.Fprintln(os.Stderr, "Cannot write mdate file:", err)
		}
	}
}

// doFullSync opens the library for update (needed for re-import on edit) and writes.
func doFullSync(cfg TBibGetConfig, baseDir string) {
	if !openLibraryToUpdate() {
		return
	}
	writeFullSync(cfg, baseDir)
}

// doSync is the -sync entry point.
//
// bib.config is always read from the current working directory — the same
// convention as the deprecated -get. Run -sync from whichever directory holds
// the relevant bib.config (a project folder, the exchange folder, etc.).
//
// When file_name lists multiple files (";"-separated), all are synced unless
// a filter argument narrows the run to one. Per-file configs (<name>.config)
// are also read from CWD.
//
// Library access: read-only when all files are pull mode; read-write when any
// file is full mode (full-mode sync may re-import an edited bib back into DB).
func doSync(filter string) {
	baseCfg, ok := readBibGetConfig()
	if !ok {
		os.Exit(1)
	}

	var fileNames []string
	for _, name := range strings.Split(baseCfg.FileNames, ";") {
		name = strings.TrimSpace(name)
		if name != "" {
			fileNames = append(fileNames, name)
		}
	}
	if len(fileNames) == 0 {
		fmt.Fprintln(os.Stderr, "sync: file_names is not set in bib.config")
		os.Exit(1)
	}

	if filter != "" {
		found := false
		for _, name := range fileNames {
			if name == filter {
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "sync: %q is not in the file_names list (%s)\n", filter, baseCfg.FileNames)
			os.Exit(1)
		}
		fileNames = []string{filter}
	}

	// Read all per-file configs first so we can determine the required library
	// access level before opening the library.
	type fileEntry struct {
		cfg        TBibGetConfig
		skipPhase2 bool // subset only: skip phase 2 (fresh export done, or up-sync aborted)
	}
	var files []fileEntry
	needsWrite := false
	for _, name := range fileNames {
		cfg, ok := readFileConfig(baseCfg, name, "")
		if !ok {
			os.Exit(1)
		}
		files = append(files, fileEntry{cfg: cfg})
		if cfg.Mode == "full" || cfg.Mode == "harvest" || cfg.Mode == "subset" {
			needsWrite = true
		}
	}

	if needsWrite {
		if !openLibraryToUpdate() {
			return
		}
	} else {
		if !openLibraryToReport() {
			return
		}
	}

	// Phase 1: merge all bib-side changes into the DB before writing any output.
	for i := range files {
		switch files[i].cfg.Mode {
		case "harvest":
			runHarvestSync(files[i].cfg, "")
		case "subset":
			files[i].skipPhase2 = runSubsetPhase1(files[i].cfg, "")
		}
	}

	// Phase 2: write all output bib files from the (now fully updated) DB.
	for _, f := range files {
		switch f.cfg.Mode {
		case "full":
			writeFullSync(f.cfg, "")
		case "harvest":
			// harvest has no bib output
		case "subset":
			if !f.skipPhase2 {
				runSubsetPhase2(f.cfg, "")
			}
		default: // "pull"
			writePullSync(f.cfg, "")
		}
	}
}

// appendToKeysFile appends alias;canonicalKey to the .keys file named in bib.config.
func appendToKeysFile(alias, canonicalKey string) {
	if canonicalKey == "" {
		fmt.Fprintln(os.Stderr, "-map: key not found in library, cannot add to keys file")
		return
	}
	data, err := os.ReadFile("bib.config")
	if err != nil {
		fmt.Fprintln(os.Stderr, "-map: no bib.config in current directory")
		return
	}
	var cfg TBibGetConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintln(os.Stderr, "-map: cannot parse bib.config:", err)
		return
	}
	// Use the first file name from file_names (the most common case for -map).
	firstName := strings.SplitN(strings.TrimSpace(cfg.FileNames), ";", 2)[0]
	if firstName == "" {
		fmt.Fprintln(os.Stderr, "-map: bib.config has no file_names")
		return
	}
	pairs, _, _ := readKeysFile(firstName)
	keysPath := firstName + KeysFileExtension
	for _, p := range pairs {
		if p.canonicalKey == canonicalKey {
			if p.localKey != alias {
				fmt.Fprintf(os.Stderr, "-map: %s is already in %s as %s\n", canonicalKey, keysPath, p.localKey)
			} else {
				fmt.Fprintf(os.Stderr, "-map: %s is already in %s\n", alias, keysPath)
			}
			return
		}
	}
	f, err := os.OpenFile(keysPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "-map: cannot open keys file:", err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s%s%s\n", alias, csvDelimiter, canonicalKey)
}
