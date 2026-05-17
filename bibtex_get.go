/*
 *
 * Module: bibtex_get
 *
 * Implements the -get command: reads bib.config + <file_name>.map from the
 * current working directory, extracts matching entries from the library, and
 * writes <file_name>.bib.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 10.05.2026
 *
 */

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TBibGetConfig mirrors the bib.config JSON file.
type TBibGetConfig struct {
	FileName   string `json:"file_name"`
	IncludeDOI  bool   `json:"include_doi"`
	IncludeISBN bool   `json:"include_isbn"`
	BiberMode   bool   `json:"biber_mode"`
	Shorten     bool   `json:"shorten"`
	IncludeURL  bool   `json:"include_url"`
}

// readBibGetConfig reads bib.config from the current working directory.
// Fields absent from the JSON keep their defaults: include_doi=true,
// include_isbn=true, include_url=true, biber_mode=false, shorten=false.
func readBibGetConfig() (TBibGetConfig, bool) {
	data, err := os.ReadFile("bib.config")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot read bib.config:", err)
		return TBibGetConfig{}, false
	}
	cfg := TBibGetConfig{
		IncludeDOI:  true,
		IncludeISBN: true,
		IncludeURL:  true,
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot parse bib.config:", err)
		return TBibGetConfig{}, false
	}
	if cfg.FileName == "" {
		fmt.Fprintln(os.Stderr, "bib.config: file_name is required")
		return TBibGetConfig{}, false
	}
	return cfg, true
}

// TBibGetPair holds one row from the .map file.
type TBibGetPair struct {
	localKey     string
	canonicalKey string
}

// readBibGetMap reads <fileName>.map (local_key;canonical_key CSV).
// Legacy files using a space delimiter are also accepted; legacyFormat is
// returned true when any line used space so the caller can rewrite the file.
// First local key wins when a canonical key appears more than once.
func readBibGetMap(fileName string) (pairs []TBibGetPair, legacyFormat bool, ok bool) {
	canonicalSeen := map[string]bool{}

	ok = processFile(fileName+".map", func(line string) {
		delim := csvDelimiter
		if !strings.Contains(line, delim) {
			delim = " "
			legacyFormat = true
		}
		parts := strings.SplitN(line, delim, 2)
		if len(parts) != 2 {
			return
		}
		local := strings.TrimSpace(parts[0])
		canonical := strings.TrimSpace(parts[1])
		if local == "" || canonical == "" {
			return
		}
		if !canonicalSeen[canonical] {
			canonicalSeen[canonical] = true
			pairs = append(pairs, TBibGetPair{local, canonical})
		}
	})

	return
}

// rewriteBibGetMap writes pairs back to <fileName>.map using the canonical
// semicolon delimiter, normalising legacy space-separated files in place.
func rewriteBibGetMap(fileName string, pairs []TBibGetPair) {
	path := fileName + ".map"
	FileRename(path, path+".old")
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot rewrite map file:", err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, p := range pairs {
		w.WriteString(p.localKey + csvDelimiter + p.canonicalKey + "\n")
	}
	w.Flush()
}

// TShortenMappings maps field name to an ordered list of (from, to) pairs.
type TShortenMappings map[string][][2]string

// readShortenMappings reads <basePath><ShortenMappingsFilePath> (field;from;to CSV).
func readShortenMappings(basePath string) TShortenMappings {
	result := TShortenMappings{}
	processFile(basePath+ShortenMappingsFilePath, func(line string) {
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

// applyShorten applies shorten_mappings substitutions to a field value.
func applyShorten(mappings TShortenMappings, field, value string) string {
	for _, pair := range mappings[field] {
		value = strings.ReplaceAll(value, pair[0], pair[1])
	}
	return value
}

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

// bibGetNonExportFields is the set of fields never written to a get-output file.
var bibGetNonExportFields = func() TStringSet {
	s := TStringSetNew()
	s.Add(
		GroupsField, DBLPField, PreferredAliasField, EntryTypeField,
		LocalURLField, "date-added", "date-modified",
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
) string {
	entry := loadEntryFromDb(canonicalKey)
	if !entry.Exists() {
		return ""
	}

	result := "@" + entry.EntryType() + "{" + outputKey + ",\n"

	for _, field := range BibTeXAllowedEntryFields[entry.EntryType()].Set().ElementsSorted() {
		if bibGetNonExportFields.Contains(field) {
			continue
		}
		if field == EntryTypeField {
			continue
		}
		if !cfg.IncludeDOI && field == "doi" {
			continue
		}
		if !cfg.IncludeISBN && (field == "isbn" || field == "issn") {
			continue
		}

		value := entry.FieldValue(field)
		if value == "" {
			continue
		}

		// URL handling: when include_url is false, skip url unless urldate is present.
		if field == "url" && !cfg.IncludeURL {
			if entry.FieldValue("urldate") == "" {
				continue
			}
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

		result += FormatBibTeXFieldAssignment("", field, l.MapEntryFieldValue(canonicalKey, field, value))
	}

	result += "}\n"
	return result
}

// doGet implements the -get command.
func doGet() {
	cfg, ok := readBibGetConfig()
	if !ok {
		os.Exit(1)
	}

	// Resolve the map file path relative to CWD.
	mapFilePath := cfg.FileName
	if !filepath.IsAbs(mapFilePath) {
		cwd, err := os.Getwd()
		if err == nil {
			mapFilePath = filepath.Join(cwd, mapFilePath)
		}
	}

	pairs, legacyFormat, ok := readBibGetMap(mapFilePath)
	if !ok {
		fmt.Fprintln(os.Stderr, "Cannot read map file:", mapFilePath+".map")
		os.Exit(1)
	}

	// Resolve canonical keys through the key alias / oldies table.
	mapModified := legacyFormat
	for i, p := range pairs {
		resolved := Library.MapEntryKey(p.canonicalKey)
		if resolved != "" && resolved != p.canonicalKey {
			pairs[i].canonicalKey = resolved
			mapModified = true
		}
	}
	if mapModified {
		rewriteBibGetMap(mapFilePath, pairs)
	}

	var shorten TShortenMappings
	if cfg.Shorten {
		basePath := Library.FilesRoot + Library.BaseName
		shorten = readShortenMappings(basePath)
	}

	// Build canonical->localKey index (first local key wins, already enforced by readBibGetMap).
	canonicalToLocal := map[string]string{}
	for _, p := range pairs {
		resolved := Library.MapEntryKey(p.canonicalKey)
		if resolved == "" {
			resolved = p.canonicalKey
		}
		if _, seen := canonicalToLocal[resolved]; !seen {
			canonicalToLocal[resolved] = p.localKey
		}
	}

	// Collect crossref parents not already in the map and add them.
	// Use preferred alias (or canonical key) as local key for auto-added parents.
	type autoParent struct {
		localKey     string
		canonicalKey string
	}
	var autoParents []autoParent
	autoParentLocal := map[string]string{} // canonical -> local key used

	for resolved := range canonicalToLocal {
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
		localKey := Library.PreferredKey(resolvedCrossref)
		if localKey == "" {
			localKey = resolvedCrossref
		}
		autoParentLocal[resolvedCrossref] = localKey
		autoParents = append(autoParents, autoParent{localKey, resolvedCrossref})
	}

	// effectiveLocalKey returns the local key to use for a canonical in the crossref field.
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
	outPath := cfg.FileName + BibFileExtension
	if !filepath.IsAbs(outPath) {
		cwd, err := os.Getwd()
		if err == nil {
			outPath = filepath.Join(cwd, outPath)
		}
	}

	Reporting.Progress(ProgressWritingGetBib, outPath)

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot create output directory:", err)
		os.Exit(1)
	}

	file, err := os.Create(outPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot create output file:", err)
		os.Exit(1)
	}
	defer file.Close()

	w := bufio.NewWriter(file)

	// Non-bookish entries first (children before parents).
	for _, p := range pairs {
		resolved := Library.MapEntryKey(p.canonicalKey)
		if resolved == "" {
			resolved = p.canonicalKey
		}
		entryType := Library.EntryType(resolved)
		if BibTeXBookish.Contains(entryType) {
			continue
		}
		crossref := Library.EntryFieldValueity(resolved, "crossref")
		crossrefLocal := ""
		if crossref != "" {
			resolvedCrossref := Library.MapEntryKey(crossref)
			if resolvedCrossref == "" {
				resolvedCrossref = crossref
			}
			crossrefLocal = effectiveLocalKey(resolvedCrossref)
		}
		w.WriteString(Library.entryGetString(resolved, p.localKey, crossrefLocal, cfg, shorten))
		w.WriteString("\n")
	}

	// Bookish entries from map.
	for _, p := range pairs {
		resolved := Library.MapEntryKey(p.canonicalKey)
		if resolved == "" {
			resolved = p.canonicalKey
		}
		entryType := Library.EntryType(resolved)
		if !BibTeXBookish.Contains(entryType) {
			continue
		}
		w.WriteString(Library.entryGetString(resolved, p.localKey, "", cfg, shorten))
		w.WriteString("\n")
	}

	// Auto-added crossref parents.
	for _, ap := range autoParents {
		w.WriteString(Library.entryGetString(ap.canonicalKey, ap.localKey, "", cfg, shorten))
		w.WriteString("\n")
	}

	w.Flush()
}

// appendToMapFile appends alias;canonicalKey to the map file named in bib.config.
// A no-op when bib.config is absent. Reports an error when the file cannot be opened.
// Skips the append when the canonical key is already present in the file.
func appendToMapFile(alias, canonicalKey string) {
	data, err := os.ReadFile("bib.config")
	if err != nil {
		return
	}
	var cfg TBibGetConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot parse bib.config:", err)
		return
	}
	if cfg.FileName == "" {
		return
	}
	pairs, _, _ := readBibGetMap(cfg.FileName)
	for _, p := range pairs {
		if p.canonicalKey == canonicalKey {
			return
		}
	}
	f, err := os.OpenFile(cfg.FileName+".map", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot open map file:", err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s%s%s\n", alias, csvDelimiter, canonicalKey)
}
