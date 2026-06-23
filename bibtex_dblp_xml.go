/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: DBLPXMLImporter
 *
 * Streaming importer from DBLP XML exports (.xml.gz) into the file-based
 * DBLP store under ~/BiBTeX.Generics/DBLP/.
 * Field values are stored VERBATIM: HTML entity references (&iacute; etc.) are
 * preserved as "&name;" strings, HTML inline tags (<i>, <sup> etc.) are
 * reconstructed as "<tag>...</tag>" text, and Unicode characters from ISO-8859-1
 * decoding are stored as-is. Conversion to LaTeX happens at read time via
 * dblpRawToLaTeX (html_commands_map → html_character_map → unicode_map).
 *
 * Import is two-pass: the first pass collects name mappings from www (person
 * homepage) entries; the second pass writes entry files with names already
 * mapped to their canonical bibtex= forms.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 19.05.2026
 *
 */

package main

import (
	"compress/gzip"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// dblpDisambigSuffix matches the trailing disambiguation number that DBLP
// appends to person names in the bibtex= attribute (e.g. "Robert Winter 0001").
// These suffixes are DBLP-internal and are stripped when building name maps.
var dblpDisambigSuffix = regexp.MustCompile(` \d{4}$`)

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
// stored in the title index for fast title-based duplicate detection.
var dblpTitleIndexedFields = map[string]bool{
	"title": true, "booktitle": true,
}

// xmlCollectText reads XML tokens until the matching close tag and returns the
// concatenated text, storing HTML inline elements verbatim as "<tag>...</tag>".
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

// newDblpDecoder creates an XML decoder for DBLP XML with ISO-8859-1 support
// and the entity passthrough map. The caller must advance past the <dblp> root.
func newDblpDecoder(r io.Reader) *xml.Decoder {
	d := xml.NewDecoder(r)
	d.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		if strings.EqualFold(charset, "iso-8859-1") {
			return transform.NewReader(input, charmap.ISO8859_1.NewDecoder()), nil
		}
		return input, nil
	}
	d.Entity = xmlEntityPassthrough
	d.Strict = false
	return d
}

// advanceToDblpRoot reads tokens until the <dblp> root element is found.
func advanceToDblpRoot(d *xml.Decoder) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return fmt.Errorf("seeking <dblp> root: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "dblp" {
			return nil
		}
	}
}

// --- Key collection (manifest repair) ---

// dblpCollectKeysFromXML streams the DBLP XML reading only the key= and mdate=
// attributes of each entry start element (skipping the entire entry body),
// and returns the resulting manifest.
// progress is called every dblpProgressInterval entries (may be nil).
func dblpCollectKeysFromXML(r io.Reader, progress func(n int)) (TDblpManifest, error) {
	d := newDblpDecoder(r)
	if err := advanceToDblpRoot(d); err != nil {
		return nil, err
	}
	m := make(TDblpManifest)
	count := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return m, fmt.Errorf("XML token near entry %d: %w", count, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if !dblpKnownEntryTypes[se.Name.Local] {
			d.Skip()
			continue
		}
		var dblpKey, mdate string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "key":
				dblpKey = a.Value
			case "mdate":
				mdate = a.Value
			}
		}
		d.Skip()
		if dblpKey != "" {
			m.add(dblpKey, mdate)
		}
		count++
		if count%dblpProgressInterval == 0 && progress != nil {
			progress(count)
		}
	}
	return m, nil
}

// rebuildDblpManifests streams the DBLP XML at xmlGzPath, collecting the key and
// mdate of each entry, then writes manifest.csv files for those entries that also
// exist on disk. Parallel stat calls check disk presence: faster than a directory
// walk because stats are parallelisable and benefit from kernel path-cache warmth,
// while WalkDir must traverse the entire tree sequentially in a single goroutine.
func rebuildDblpManifests(xmlGzPath string) (TDblpManifest, error) {
	f, err := os.Open(xmlGzPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	fmt.Fprintf(os.Stderr, "  Scanning XML...\n")
	xmlManifest, err := dblpCollectKeysFromXML(gz, func(n int) {
		fmt.Fprintf(os.Stderr, "\r  %d entries scanned...", n)
	})
	xmlTotal := 0
	for _, entries := range xmlManifest {
		xmlTotal += len(entries)
	}
	fmt.Fprintf(os.Stderr, "\r\033[K  %d entries in XML.\n", xmlTotal)
	if err != nil {
		return nil, err
	}

	type statJob struct {
		dblpKey string
		mdate   string
	}
	type statHit struct {
		dblpKey string
		mdate   string
	}
	entriesDir := dblpFolder() + "entries/"
	jobs := make(chan statJob, dblpStatWorkers*4)
	hits := make(chan statHit, dblpStatWorkers*4)

	var wg sync.WaitGroup
	for range dblpStatWorkers {
		wg.Go(func() {
			for j := range jobs {
				path := filepath.Join(entriesDir, j.dblpKey, "data.json")
				if _, err := os.Lstat(path); err == nil {
					hits <- statHit{j.dblpKey, j.mdate}
				}
			}
		})
	}
	go func() {
		for parentDir, entries := range xmlManifest {
			for entryName, me := range entries {
				var dblpKey string
				if parentDir == "." {
					dblpKey = entryName
				} else {
					dblpKey = parentDir + "/" + entryName
				}
				jobs <- statJob{dblpKey, me.Mdate}
			}
		}
		close(jobs)
		wg.Wait()
		close(hits)
	}()

	fmt.Fprintf(os.Stderr, "  Checking disk entries...\n")
	filtered := make(TDblpManifest)
	found := 0
	for h := range hits {
		filtered.add(h.dblpKey, h.mdate)
		found++
		if found%100_000 == 0 {
			fmt.Fprintf(os.Stderr, "\r  %d / %d entries on disk (%d%%)...", found, xmlTotal, pct(found, xmlTotal))
		}
	}
	fmt.Fprintf(os.Stderr, "\r\033[K  %d / %d entries on disk (%d%%).\n", found, xmlTotal, pct(found, xmlTotal))
	writeDblpManifests(filtered)
	return filtered, nil
}

// pct returns 100*n/total, or 0 if total is zero.
func pct(n, total int) int {
	if total == 0 {
		return 0
	}
	return 100 * n / total
}

// doRebuildDblpTitleIndex walks every data.json in the file store and rebuilds the
// title index in place. Much faster than a full XML re-import; no XML file required.
// Useful when the title index is incomplete (e.g. after entries were fetched on-demand
// without being indexed, or after -repair_dblp_manifest cleared the index).
func doRebuildDblpTitleIndex() {
	entriesRoot := dblpFolder() + "entries/"

	// Trash the existing title index so we start clean.
	titlesDir := dblpFolder() + "titles/"
	if _, err := os.Stat(titlesDir); err == nil {
		fmt.Fprintf(os.Stderr, "Moving old title index to trash...\n")
		if err := moveToDblpTrash(titlesDir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not move old title index to trash: %s\n", err)
		}
	}

	total := 0
	if meta := readDblpMeta(); meta != nil {
		total = meta.EntryCount
	}
	if total > 0 {
		fmt.Fprintf(os.Stderr, "Rebuilding title index from %s (%d entries)...\n", entriesRoot, total)
	} else {
		fmt.Fprintf(os.Stderr, "Rebuilding title index from %s...\n", entriesRoot)
	}

	// Accumulate hash→keys in memory to avoid per-entry read-before-write in
	// appendToIndexFile. Writing all link files in one batch at the end eliminates
	// the duplicate-check read on each entry and avoids cache-thrashing on large stores.
	titleLinks := make(map[string][]string, 1<<20)

	var count, errors int
	start := time.Now()
	lastReport := start

	err := filepath.WalkDir(entriesRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "data.json" {
			return err
		}
		rel := strings.TrimPrefix(path, entriesRoot)
		dblpKey := strings.TrimSuffix(rel, "/data.json")

		je := readDblpJSONEntry(dblpKey)
		if je == nil {
			errors++
			return nil
		}
		for _, fieldName := range []string{"title", "booktitle"} {
			if je.Fields != nil {
				if value := je.Fields[fieldName]; value != "" {
					if hash := dblpTitleHash(value); hash != "" {
						titleLinks[hash] = append(titleLinks[hash], dblpKey)
					}
				}
			}
		}
		count++
		if now := time.Now(); now.Sub(lastReport) >= 5*time.Second {
			if total > 0 {
				fmt.Fprintf(os.Stderr, "  %d / %d entries indexed (%.0fs)...\n", count, total, now.Sub(start).Seconds())
			} else {
				fmt.Fprintf(os.Stderr, "  %d entries indexed (%.0fs)...\n", count, now.Sub(start).Seconds())
			}
			lastReport = now
		}
		return nil
	})

	// Write all accumulated title links — one file per hash, no read needed since
	// the old index was moved to trash before this run.
	if err == nil && len(titleLinks) > 0 {
		nHashes := len(titleLinks)
		fmt.Fprintf(os.Stderr, "Writing %d title link files...\n", nHashes)
		written := 0
		writeStart := time.Now()
		lastWriteReport := writeStart
		for hash, keys := range titleLinks {
			path := dblpTitleLinkPath(hash)
			if mkErr := os.MkdirAll(filepath.Dir(path), 0755); mkErr != nil {
				errors++
				continue
			}
			data := strings.Join(keys, "\n") + "\n"
			if wErr := os.WriteFile(path, []byte(data), 0644); wErr != nil {
				errors++
			} else {
				written++
			}
			if now := time.Now(); now.Sub(lastWriteReport) >= 5*time.Second {
				fmt.Fprintf(os.Stderr, "  %d / %d link files written (%.0fs)...\n",
					written, nHashes, now.Sub(writeStart).Seconds())
				lastWriteReport = now
			}
		}
		fmt.Fprintf(os.Stderr, "  %d link files written.\n", written)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error walking file store: %s\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Title index rebuilt from %d entries (%d unreadable) in %.0fs.\n",
		count, errors, time.Since(start).Seconds())
}

// doRepairDblpManifest rebuilds the entries.manifest from a DBLP XML export and
// moves the title index to trash for a clean rebuild on the next import.
// The XML arg must name a file present in dblpFolder().
func doRepairDblpManifest(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: -repair_dblp_manifest <filename.xml.gz>\n")
		os.Exit(1)
	}
	xmlFilename := filepath.Base(args[0])
	xmlGzPath := dblpFolder() + xmlFilename
	if !FileExists(xmlGzPath) {
		fmt.Fprintf(os.Stderr, "File not found in DBLP store: %s\n", xmlGzPath)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Rebuilding entry manifests from XML...\n")
	start := time.Now()
	if _, err := rebuildDblpManifests(xmlGzPath); err != nil {
		fmt.Fprintf(os.Stderr, "Manifest rebuild failed: %s\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "  Entry manifests rebuilt (%.0fs).\n", time.Since(start).Seconds())

	// Move the title index to trash; the next doLoadDblpXml rebuilds it fresh.
	// crossref/person/ORCID indexes are maintained incrementally and left in place.
	titlesDir := dblpFolder() + "titles/"
	if _, err := os.Stat(titlesDir); err == nil {
		if err := moveToDblpTrash(titlesDir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not move title index to trash: %s\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "  Title index moved to trash — will be rebuilt on next import.\n")
		}
	}

	writeDblpCurrentXML(xmlGzPath)
	fmt.Fprintf(os.Stderr, "Manifest repair complete.\n")
}

// dblpBuildCrossrefIndex streams the DBLP XML at r, collects every entry's
// crossref field, then wipes crossrefs/ and writes clean, sorted, deduplicated
// children.txt files. progress is called every dblpProgressInterval entries.
func dblpBuildCrossrefIndex(r io.Reader, progress func(n int)) error {
	d := newDblpDecoder(r)
	if err := advanceToDblpRoot(d); err != nil {
		return err
	}

	crossrefs := make(map[string][]string) // parentKey → []childKey
	count := 0

	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("building crossref index: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if !dblpKnownEntryTypes[se.Name.Local] {
			d.Skip()
			continue
		}

		var dblpKey string
		for _, attr := range se.Attr {
			if attr.Name.Local == "key" {
				dblpKey = attr.Value
				break
			}
		}

		var crossref string
	entryLoop:
		for {
			child, cerr := d.Token()
			if cerr != nil {
				break
			}
			switch ct := child.(type) {
			case xml.StartElement:
				if ct.Name.Local == "crossref" {
					var val strings.Builder
					for {
						inner, ierr := d.Token()
						if ierr != nil {
							break
						}
						if _, isEnd := inner.(xml.EndElement); isEnd {
							break
						}
						if cd, isText := inner.(xml.CharData); isText {
							val.WriteString(string(cd))
						}
					}
					crossref = val.String()
				} else {
					d.Skip()
				}
			case xml.EndElement:
				_ = ct
				break entryLoop
			}
		}

		if dblpKey != "" && crossref != "" {
			crossrefs[crossref] = append(crossrefs[crossref], dblpKey)
		}

		count++
		if count%dblpProgressInterval == 0 && progress != nil {
			progress(count)
		}
	}

	// Move old index to trash, then write clean, deduplicated children.txt files.
	if crossrefsDir := dblpFolder() + "crossrefs/"; FileExists(crossrefsDir) {
		moveToDblpTrash(crossrefsDir) // non-fatal
	}
	for parentKey, children := range crossrefs {
		sort.Strings(children)
		prev := ""
		for _, child := range children {
			if child != prev {
				writeDblpCrossrefChild(parentKey, child) // non-fatal
				prev = child
			}
		}
	}

	return nil
}

// runCrossrefIndexRebuild opens xmlGzPath, runs dblpBuildCrossrefIndex with
// progress output, and returns whether the rebuild succeeded.
// total is the expected entry count used for percentage display; pass 0 to show
// a bare count instead.
func runCrossrefIndexRebuild(xmlGzPath string, total int) bool {
	f, err := os.Open(xmlGzPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not open %s for crossref index: %s\n", xmlGzPath, err)
		return false
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not read gzip for crossref index: %s\n", err)
		return false
	}
	defer gz.Close()

	var lastN int
	err = dblpBuildCrossrefIndex(gz, func(n int) {
		lastN = n
		if total > 0 {
			fmt.Fprintf(os.Stderr, "\r  %d / %d (%.0f%%)", n, total, 100*float64(n)/float64(total))
		} else {
			fmt.Fprintf(os.Stderr, "\r  %d entries scanned...", n)
		}
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "\r\033[KWarning: crossref index rebuild failed: %s\n", err)
		return false
	}
	fmt.Fprintf(os.Stderr, "\r\033[K  Crossref index rebuilt (%d entries scanned).\n", lastN)
	return true
}

// doRebuildDblpCrossrefIndex is the CLI handler for -rebuild_dblp_crossref_index.
// It streams the last imported XML file rather than scanning individual data.json files.
func doRebuildDblpCrossrefIndex() {
	xmlFilename := readDblpCurrentXML()
	if xmlFilename == "" {
		fmt.Fprintf(os.Stderr, "No DBLP import on record; run -load_dblp_xml first.\n")
		os.Exit(1)
	}
	xmlGzPath := dblpFolder() + xmlFilename
	if !FileExists(xmlGzPath) {
		fmt.Fprintf(os.Stderr, "XML file not found: %s\n", xmlGzPath)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Rebuilding DBLP crossref index from %s...\n", xmlFilename)
	start := time.Now()
	total := 0
	if meta := readDblpMeta(); meta != nil {
		total = meta.EntryCount
	}
	if !runCrossrefIndexRebuild(xmlGzPath, total) {
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Done (%.0fs).\n", time.Since(start).Seconds())
}

// --- First pass: name map ---

// dblpBuildNameMap does a first streaming pass over the DBLP XML, collecting
// only www (person homepage) entries to build a plain-name → canonical-name map
// and a canonical-name → ORCID map. The canonical name comes from the bibtex=
// attribute of the first author that carries one; the disambiguation suffix
// (e.g. " 0001") is stripped. The orcid= attribute of that same author element
// is stored in the returned orcidMap.
func dblpBuildNameMap(r io.Reader) (nameMap, orcidMap map[string]string, err error) {
	d := newDblpDecoder(r)
	if err := advanceToDblpRoot(d); err != nil {
		return nil, nil, err
	}

	nameMap = make(map[string]string)
	orcidMap = make(map[string]string)
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nameMap, orcidMap, fmt.Errorf("building name map: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local != "www" {
			d.Skip()
			continue
		}

		var authors []dblpXMLPerson
		var urlOrcid string
	childLoop:
		for {
			child, cerr := d.Token()
			if cerr != nil {
				break
			}
			switch ct := child.(type) {
			case xml.StartElement:
				switch ct.Name.Local {
				case "author":
					var bibtex, orcid string
					for _, a := range ct.Attr {
						switch a.Name.Local {
						case "bibtex":
							bibtex = a.Value
						case "orcid":
							orcid = a.Value
						}
					}
					text, _ := xmlCollectText(d)
					authors = append(authors, dblpXMLPerson{Name: text, Bibtex: bibtex, ORCID: orcid})
				case "url":
					text, _ := xmlCollectText(d)
					if urlOrcid == "" && strings.HasPrefix(text, "https://orcid.org/") {
						urlOrcid = strings.TrimPrefix(text, "https://orcid.org/")
					}
				default:
					d.Skip()
				}
			case xml.EndElement:
				break childLoop
			}
		}

		var canonicalName, canonicalORCID string
		for _, p := range authors {
			if p.Bibtex != "" {
				canonicalName = dblpDisambigSuffix.ReplaceAllString(applyUnicodeMap(p.Bibtex), "")
				canonicalORCID = p.ORCID
				break
			}
		}
		if canonicalORCID == "" {
			canonicalORCID = urlOrcid
		}
		if canonicalName != "" {
			if canonicalORCID != "" {
				orcidMap[canonicalName] = canonicalORCID
			}
			for _, p := range authors {
				if p.Name != canonicalName {
					nameMap[p.Name] = canonicalName
				}
			}
		}
	}
	return nameMap, orcidMap, nil
}

// saveDblpNameFiles writes the DBLP name maps from Pass 1 to two CSV files in
// globalFolder: dblp_name_bibtex.csv (alias;canonical) and dblp_name_orcid.csv
// (canonical;orcid). Names are converted to LaTeX format before writing.
func saveDblpNameFiles(nameMap, orcidMap map[string]string) {
	namePath := globalFolder + "dblp_name_bibtex.csv"
	orcidPath := globalFolder + "dblp_name_orcid.csv"

	nameLines := make([]string, 0, len(nameMap))
	for rawAlias, rawCanon := range nameMap {
		al := dblpPersonNameToLaTeX(rawAlias)
		cl := dblpPersonNameToLaTeX(rawCanon)
		if al == "" || cl == "" || al == cl {
			continue
		}
		nameLines = append(nameLines, al+";"+cl)
	}
	sort.Strings(nameLines)
	if err := os.WriteFile(namePath, []byte(strings.Join(nameLines, "\n")+"\n"), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write %s: %s\n", namePath, err)
	} else {
		fmt.Fprintf(os.Stderr, "  Saved %d name mappings to %s\n", len(nameLines), namePath)
	}

	orcidLines := make([]string, 0, len(orcidMap))
	for rawCanon, orcid := range orcidMap {
		cl := dblpPersonNameToLaTeX(rawCanon)
		if cl == "" || orcid == "" {
			continue
		}
		orcidLines = append(orcidLines, cl+";"+orcid)
	}
	sort.Strings(orcidLines)
	if err := os.WriteFile(orcidPath, []byte(strings.Join(orcidLines, "\n")+"\n"), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write %s: %s\n", orcidPath, err)
	} else {
		fmt.Fprintf(os.Stderr, "  Saved %d ORCIDs to %s\n", len(orcidLines), orcidPath)
	}
}

// loadDblpNameFiles reads the two DBLP name CSV files from globalFolder and
// returns (alias→canonical, canonical→orcid) maps, both in LaTeX name format.
// Returns ok=false when either file is absent or unreadable.
func loadDblpNameFiles() (nameMapLatex, orcidMapLatex map[string]string, ok bool) {
	namePath := globalFolder + "dblp_name_bibtex.csv"
	orcidPath := globalFolder + "dblp_name_orcid.csv"

	if !FileExists(namePath) || !FileExists(orcidPath) {
		return nil, nil, false
	}

	nameMapLatex = make(map[string]string)
	processCSVFile(namePath, func(rec []string) {
		if len(rec) == 2 && rec[0] != "" && rec[1] != "" {
			nameMapLatex[rec[0]] = rec[1]
		}
	})

	orcidMapLatex = make(map[string]string)
	processCSVFile(orcidPath, func(rec []string) {
		if len(rec) == 2 && rec[0] != "" && rec[1] != "" {
			orcidMapLatex[rec[0]] = rec[1]
		}
	})

	return nameMapLatex, orcidMapLatex, true
}

// --- Second pass: main import ---

const dblpProgressInterval = 50_000
const dblpStatWorkers = 64

// dblpImportFromReader streams DBLP XML from r, writing one data.json per entry
// and one title link.txt per indexed title. nameMap contains the canonical name
// mappings collected by dblpBuildNameMap. newManifest is updated with every key
// written. progress is called every dblpProgressInterval entries (may be nil).
// Returns the total entry count and any fatal error.
func dblpImportFromReader(r io.Reader, nameMap map[string]string, originalManifest, newManifest TDblpManifest, progress func(n int)) (int, error) {
	d := newDblpDecoder(r)
	if err := advanceToDblpRoot(d); err != nil {
		return 0, err
	}

	applyNameMap := func(p dblpXMLPerson) TDblpJSONPerson {
		name := p.Name
		if mapped, ok := nameMap[name]; ok {
			name = mapped
		}
		return TDblpJSONPerson{Name: name, ORCID: p.ORCID}
	}

	count := 0
	var parseErr error

outer:
	for {
		tok, err := d.Token()
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
			d.Skip()
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
			d.Skip()
			continue
		}

		// mdate-based skip — avoids parsing the entry body entirely.
		manifestEntry, inManifest := originalManifest.get(dblpKey)
		if mdate != "" && inManifest && manifestEntry.Mdate == mdate {
			d.Skip()
			newManifest.add(dblpKey, mdate)
			count++
			if count%dblpProgressInterval == 0 && progress != nil {
				progress(count)
			}
			continue
		}

		var authors, editors []dblpXMLPerson
		var fields []dblpXMLField
		fieldPos := map[string]int{}

	childLoop:
		for {
			child, cerr := d.Token()
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
				text, terr := xmlCollectText(d)
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

		// Build JSON entry.
		je := &TDblpJSONEntry{
			EntryType: entryType,
			Mdate:     mdate,
			PubType:   pubType,
		}

		for _, f := range fields {
			value := f.Value
			if dblpTitleIndexedFields[f.Name] {
				value = strings.TrimSuffix(value, ".")
			}
			if dblpMultiValuedFields[f.Name] {
				if je.Multi == nil {
					je.Multi = make(map[string][]string)
				}
				je.Multi[f.Name] = append(je.Multi[f.Name], value)
			} else {
				if je.Fields == nil {
					je.Fields = make(map[string]string)
				}
				je.Fields[f.Name] = value
			}
		}

		for _, p := range authors {
			je.Authors = append(je.Authors, applyNameMap(p))
		}
		for _, p := range editors {
			je.Editors = append(je.Editors, applyNameMap(p))
		}

		jsonBytes, _ := json.Marshal(je)

		// For changed entries (not new) remove stale index entries before
		// overwriting, so a title or author change doesn't leave ghost links.
		// Also handle duplicate keys within the same XML: if the same dblpKey
		// already appears in newManifest it was written earlier in this run,
		// so clean up that first occurrence's links before reprocessing.
		if inManifest {
			removeKeyFromAllIndexes(dblpKey, readDblpJSONEntry(dblpKey))
		} else if _, seenThisRun := newManifest.get(dblpKey); seenThisRun {
			removeKeyFromAllIndexes(dblpKey, readDblpJSONEntry(dblpKey))
		}

		// mdate changed or entry not in manifest — write to disk and update indexes.
		if err := writeDblpEntryFile(dblpKey, jsonBytes); err != nil {
			parseErr = fmt.Errorf("writing entry %s: %w", dblpKey, err)
			break outer
		}
		newManifest.add(dblpKey, mdate)

		// Write title link files.
		for _, fieldName := range []string{"title", "booktitle"} {
			if je.Fields != nil {
				if value, ok := je.Fields[fieldName]; ok && value != "" {
					hash := dblpTitleHash(value)
					writeDblpTitleLink(hash, dblpKey) // non-fatal
				}
			}
		}

		// Write person indexes: one entry per author/editor, keyed by name and ORCID.
		for _, p := range je.Authors {
			writeDblpPersonEntry(p.Name, dblpKey) // non-fatal
			if p.ORCID != "" {
				writeDblpORCIDEntry(p.ORCID, dblpKey) // non-fatal
			}
		}
		for _, p := range je.Editors {
			writeDblpPersonEntry(p.Name, dblpKey) // non-fatal
			if p.ORCID != "" {
				writeDblpORCIDEntry(p.ORCID, dblpKey) // non-fatal
			}
		}

		count++
		if count%dblpProgressInterval == 0 && progress != nil {
			progress(count)
		}
	}

	return count, parseErr
}

// countingReader wraps an io.Reader and tracks the total number of bytes read.
// n is updated atomically so a ticker goroutine can read it concurrently.
type countingReader struct {
	r io.Reader
	n atomic.Int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.n.Add(int64(n))
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

	if meta := readDblpMeta(); meta != nil {
		fmt.Fprintf(os.Stderr, "Existing DBLP store: %d entries from %s (loaded %s)\n",
			meta.EntryCount, meta.XMLFile, meta.LoadedAt)
	}

	if err := os.MkdirAll(dblpFolder()+"entries/", 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Could not create DBLP entries folder: %s\n", err)
		os.Exit(1)
	}

	// Load existing manifests so we know which entries to prune after import.
	// If entries exist but manifests are absent, rebuild them from the tree first.
	var originalManifest TDblpManifest
	if dblpEntriesDirHasContent() {
		originalManifest = loadDblpManifests()
		if len(originalManifest) == 0 {
			fmt.Fprintf(os.Stderr, "Manifests absent — rebuilding from XML...\n")
			var rebuildErr error
			originalManifest, rebuildErr = rebuildDblpManifests(xmlGzPath)
			if rebuildErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: manifest rebuild failed: %s\n", rebuildErr)
				originalManifest = make(TDblpManifest)
			} else {
				fmt.Fprintf(os.Stderr, "  Manifests rebuilt (%d parent dirs).\n", len(originalManifest))
			}
		} else {
			fmt.Fprintf(os.Stderr, "Loaded manifests: %d parent dirs.\n", len(originalManifest))
		}
	} else {
		originalManifest = make(TDblpManifest)
	}
	newManifest := make(TDblpManifest)

	start := time.Now()

	// First pass: collect name mappings from www entries.
	fmt.Fprintf(os.Stderr, "Pass 1: building name map from www entries...\n")
	f1, err := os.Open(xmlGzPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open %s: %s\n", xmlGzPath, err)
		os.Exit(1)
	}
	gz1, err := gzip.NewReader(f1)
	if err != nil {
		f1.Close()
		fmt.Fprintf(os.Stderr, "Could not read gzip from %s: %s\n", xmlGzPath, err)
		os.Exit(1)
	}
	nameMap, orcidMap, err := dblpBuildNameMap(gz1)
	gz1.Close()
	f1.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: name map build error: %s\n", err)
	}
	fmt.Fprintf(os.Stderr, "  %d name mappings collected (%.0fs).\n", len(nameMap), time.Since(start).Seconds())
	saveDblpNameFiles(nameMap, orcidMap)

	// Second pass: import all entries.
	fmt.Fprintf(os.Stderr, "Pass 2: importing DBLP XML from %s...\n", xmlGzPath)
	f2, err := os.Open(xmlGzPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open %s: %s\n", xmlGzPath, err)
		os.Exit(1)
	}
	defer f2.Close()

	cr := &countingReader{r: f2}
	gz2, err := gzip.NewReader(cr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not read gzip from %s: %s\n", xmlGzPath, err)
		os.Exit(1)
	}
	defer gz2.Close()

	pass2Start := time.Now()
	var processed atomic.Int64

	pass2Done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				n := processed.Load()
				elapsed := time.Since(pass2Start).Seconds()
				pct := float64(cr.n.Load()) * 100.0 / float64(totalBytes)
				mbRead := float64(cr.n.Load()) / 1e6
				mbTotal := float64(totalBytes) / 1e6
				fmt.Fprintf(os.Stderr, "\r  %d entries processed (%.0fs elapsed, %.0f%%, %.1f/%.0f MB)...",
					n, elapsed, pct, mbRead, mbTotal)
			case <-pass2Done:
				return
			}
		}
	}()

	count, err := dblpImportFromReader(gz2, nameMap, originalManifest, newManifest, func(n int) {
		processed.Store(int64(n))
	})
	close(pass2Done)
	fmt.Fprintf(os.Stderr, "\r\033[K  %d entries processed (%.0fs).\n", count, time.Since(pass2Start).Seconds())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Import failed after %d entries: %s\n", count, err)
		os.Exit(1)
	}

	// Prune entries present in the old store but absent from the new XML.
	deleteStaleDblpEntries(originalManifest, newManifest)

	// Write updated manifests.
	fmt.Fprintf(os.Stderr, "Writing manifests...\n")
	writeDblpManifests(newManifest)

	meta := TDblpMeta{
		XMLFile:              xmlGzPath,
		LoadedAt:             time.Now().UTC().Format(time.RFC3339),
		EntryCount:           count,
		HtmlCommandsMapHash:  csvFileHash(globalFolder + "html_commands_map.csv"),
		HtmlCharacterMapHash: csvFileHash(globalFolder + "html_character_map.csv"),
		UnicodeMapHash:       csvFileHash(globalFolder + "unicode_map.csv"),
	}
	if err := writeDblpMeta(meta); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write meta.json: %s\n", err)
	}

	writeDblpCurrentXML(xmlGzPath)

	// Pass 3: rebuild the crossref children index from scratch.
	// Replaces the incremental children.txt writes from pass 2 with a clean,
	// complete rebuild — eliminating any stale entries from previous imports.
	fmt.Fprintf(os.Stderr, "Pass 3: rebuilding crossref index...\n")
	runCrossrefIndexRebuild(xmlGzPath, count)

	fmt.Fprintf(os.Stderr, "DBLP import complete: %d entries in %.1fs\n",
		count, time.Since(start).Seconds())
}

// --- CLI: -update_dblp ---

const dblpXMLIndexURL = "https://dblp.uni-trier.de/xml/"

var reDblpXMLFilename = regexp.MustCompile(`dblp-\d{4}-\d{2}-\d{2}\.xml\.gz`)
var reDblpUndatedDate = regexp.MustCompile(`dblp\.xml\.gz[^0-9]*(\d{4}-\d{2}-\d{2})`)

// doUpdateDblp fetches the DBLP XML index page, identifies the latest dated
// release (or falls back to the undated dblp.xml.gz stored with today's date),
// and downloads it to dblpFolder() if not already present.
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
		latest = "dblp-" + m[1] + ".xml.gz"
		downloadURL = dblpXMLIndexURL + "dblp.xml.gz"
		fmt.Fprintf(os.Stderr, "No dated files found; using dblp.xml.gz (dated %s) → %s\n", m[1], latest)
	} else if strings.Contains(string(body), "dblp.xml.gz") {
		latest = "dblp-" + time.Now().Format("2006-01-02") + ".xml.gz"
		downloadURL = dblpXMLIndexURL + "dblp.xml.gz"
		fmt.Fprintf(os.Stderr, "No dated files found; using dblp.xml.gz → %s\n", latest)
	} else {
		fmt.Fprintf(os.Stderr, "No DBLP XML files found at %s\n", dblpXMLIndexURL)
		os.Exit(1)
	}

	if err := os.MkdirAll(dblpFolder(), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Could not create DBLP folder: %s\n", err)
		os.Exit(1)
	}

	destPath := dblpFolder() + latest
	if FileExists(destPath) {
		if readDblpCurrentXML() == latest {
			fmt.Fprintf(os.Stderr, "Already have %s — nothing to do.\n", latest)
			return
		}
		fmt.Fprintf(os.Stderr, "Found %s but import incomplete — re-importing...\n", latest)
		doLoadDblpXml([]string{destPath})
		cleanupDblpXmlFiles()
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
				n := cr.n.Load()
				mb := float64(n) / 1e6
				elapsed := time.Since(start).Seconds()
				if total > 0 {
					fmt.Fprintf(os.Stderr, "  %.0f / %.0f MB (%.0f%%) %.0fs\n",
						mb, float64(total)/1e6, float64(n)*100/float64(total), elapsed)
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
		latest, float64(cr.n.Load())/1e6, time.Since(start).Seconds())
	doLoadDblpXml([]string{destPath})

	// Import succeeded — keep only the two most recent XML files.
	cleanupDblpXmlFiles()
}

// swapBibTeXNameFormat converts between the two BibTeX name orderings:
//   - "ss, ff"      ↔  "ff ss"
//   - "ss, gg, ff"  ↔  "ff ss, gg"
//
// Returns "" when the input has no ", " separator (natural-order with unknown surname).
func swapBibTeXNameFormat(name string) string {
	parts := strings.SplitN(name, ", ", 3)
	switch len(parts) {
	case 2:
		return parts[1] + " " + parts[0]
	case 3:
		return parts[2] + " " + parts[0] + ", " + parts[1]
	}
	return ""
}

// parseSurnameGeneration extracts the surname and optional generation from a
// surname-first BibTeX name ("ss, ff" or "ss, gg, ff").
// "de Boer, Remco C."      → ("de Boer", "")
// "King, Jr., Martin Luther" → ("King", "Jr.")
func parseSurnameGeneration(surnameFirstName string) (surname, generation string) {
	parts := strings.SplitN(surnameFirstName, ", ", 3)
	switch len(parts) {
	case 2:
		return parts[0], ""
	case 3:
		return parts[0], parts[1]
	}
	return "", ""
}

// naturalOrderToSurnameFirst converts "ff ss" → "ss, ff" and "ff ss, gg" → "ss, gg, ff"
// using the known surname and optional generation from the DBLP canonical.
// Returns "" when the alias does not end with the expected surname.
func naturalOrderToSurnameFirst(alias, surname, generation string) string {
	if surname == "" {
		return ""
	}
	// Form 1: "ff ss, gg" with known generation
	if generation != "" && strings.HasSuffix(alias, " "+surname+", "+generation) {
		fn := strings.TrimSuffix(alias, " "+surname+", "+generation)
		if strings.TrimSpace(fn) == "" {
			return ""
		}
		return surname + ", " + generation + ", " + fn
	}
	// Form 2: "ff ss"
	if strings.HasSuffix(alias, " "+surname) {
		fn := strings.TrimSuffix(alias, " "+surname)
		if strings.TrimSpace(fn) == "" {
			return ""
		}
		return surname + ", " + fn
	}
	return ""
}

// doAbsorbDblpNames streams the stored DBLP XML, extracts the www-based name
// variant→canonical map, and feeds each person's names into the library's
// name_mappings table via AddNameMapping. Format variants (surname-first ↔
// natural-order) are generated so that existing library mappings in either
// ordering are merged into the DBLP canonical before adding the DBLP aliases.
// A final RenormaliseNameFields pass applies all new mappings to stored entries.
func doAbsorbDblpNames() {
	if !openLibraryToUpdate() {
		return
	}
	// Load name and ORCID maps. Fast path: pre-built CSV files from Pass 1 of
	// -load_dblp_xml. Fallback: re-scan the gz (slow, for first run or missing files).
	nameMapLatex, orcidMapLatex, filesOK := loadDblpNameFiles()
	if filesOK {
		Library.Progress("Loaded DBLP name maps from cache (%d aliases, %d ORCIDs).",
			len(nameMapLatex), len(orcidMapLatex))
	} else {
		xmlFilename := readDblpCurrentXML()
		if xmlFilename == "" {
			Library.Error("No DBLP import on record; run -load_dblp_xml first.")
			return
		}
		xmlGzPath := dblpFolder() + xmlFilename
		if !FileExists(xmlGzPath) {
			Library.Error("DBLP XML file not found: %s", xmlGzPath)
			return
		}
		f, err := os.Open(xmlGzPath)
		if err != nil {
			Library.Error("Cannot open DBLP XML: %s", err)
			return
		}
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			Library.Error("Cannot read DBLP XML gzip: %s", err)
			return
		}
		Library.Progress("Building DBLP name map from %s ...", xmlFilename)
		rawNameMap, rawOrcidMap, buildErr := dblpBuildNameMap(gz)
		gz.Close()
		f.Close()
		if buildErr != nil {
			Library.Warning("DBLP name map build partial: %s", buildErr)
		}
		Library.Progress("  %d DBLP name variant→canonical pairs found.", len(rawNameMap))

		nameMapLatex = make(map[string]string, len(rawNameMap))
		for rawAlias, rawCanon := range rawNameMap {
			al := dblpPersonNameToLaTeX(rawAlias)
			cl := dblpPersonNameToLaTeX(rawCanon)
			if al != "" && cl != "" && al != cl {
				nameMapLatex[al] = cl
			}
		}
		orcidMapLatex = make(map[string]string, len(rawOrcidMap))
		for rawCanon, orcid := range rawOrcidMap {
			cl := dblpPersonNameToLaTeX(rawCanon)
			if cl != "" && orcid != "" {
				orcidMapLatex[cl] = orcid
			}
		}
	}

	// Group aliases by their DBLP canonical (names already in LaTeX format).
	groups := make(map[string][]string) // canonicalLaTeX → []aliasLaTeX
	for aliasLaTeX, canonicalLaTeX := range nameMapLatex {
		groups[canonicalLaTeX] = append(groups[canonicalLaTeX], aliasLaTeX)
	}

	// mergeIfKnown absorbs an existing contributor whose name matches form into
	// the contributor identified by dblpCanonical. Uses NameToContributorID for
	// O(1) lookup across both canonical and alias forms.
	mergeIfKnown := func(dblpCanonical, form string) {
		if form == "" || form == dblpCanonical {
			return
		}
		if existingID, ok := Library.NameToContributorID[form]; ok {
			existingCanonical := Library.ContributorByID[existingID].Name
			if existingCanonical != dblpCanonical {
				Library.AddNameMapping(dblpCanonical, existingCanonical)
			}
		}
	}

	// isKnown returns true if any of the supplied name forms is already a known contributor.
	isKnown := func(forms ...string) bool {
		for _, form := range forms {
			if form != "" {
				if _, ok := Library.NameToContributorID[form]; ok {
					return true
				}
			}
		}
		return false
	}

	absorbed := 0
	for canonicalLaTeX, aliases := range groups {
		surname, generation := parseSurnameGeneration(canonicalLaTeX)
		naturalCanonical := swapBibTeXNameFormat(canonicalLaTeX)

		// Skip groups where none of the DBLP name forms matches an existing contributor.
		// Absorbing unknown persons would create spurious contributors not grounded in
		// any of our bib entries.
		allForms := []string{canonicalLaTeX, naturalCanonical}
		for _, alias := range aliases {
			allForms = append(allForms, alias, swapBibTeXNameFormat(alias),
				naturalOrderToSurnameFirst(alias, surname, generation))
		}
		if !isKnown(allForms...) {
			continue
		}

		// Merge any existing library group whose name is a format variant of the canonical.
		mergeIfKnown(canonicalLaTeX, naturalCanonical)

		// Merge existing library groups whose name is a format variant of any alias.
		for _, alias := range aliases {
			mergeIfKnown(canonicalLaTeX, alias)
			mergeIfKnown(canonicalLaTeX, swapBibTeXNameFormat(alias))
			mergeIfKnown(canonicalLaTeX, naturalOrderToSurnameFirst(alias, surname, generation))
		}

		// Add the natural-order canonical form as an alias (bridges both orderings).
		if naturalCanonical != "" && naturalCanonical != canonicalLaTeX {
			Library.AddNameMapping(canonicalLaTeX, naturalCanonical)
		}

		// Add all DBLP aliases.
		for _, alias := range aliases {
			Library.AddNameMapping(canonicalLaTeX, alias)
			absorbed++
		}
	}
	Library.Progress("Absorbed %d DBLP name mappings.", absorbed)

	// Set ORCIDs for known contributors that do not yet have one.
	orcidsSet := 0
	for canonicalLaTeX, orcid := range orcidMapLatex {
		for _, form := range []string{canonicalLaTeX, swapBibTeXNameFormat(canonicalLaTeX)} {
			id, ok := Library.NameToContributorID[form]
			if !ok {
				continue
			}
			contrib := Library.ContributorByID[id]
			if contrib.ORCID != "" {
				break
			}
			contrib.ORCID = orcid
			upsertContributorToDB(id, contrib.Name, orcid)
			orcidsSet++
			break
		}
	}
	// File-store fallback: some contributors have their ORCID listed only as a
	// url in their DBLP www entry, not as an orcid= attribute on the author
	// element. The CSV-based map above misses those. For each contributor still
	// without an ORCID, do a fast existence check on the DBLP persons index file
	// (one stat call); only if that file exists do we proceed to read it and look
	// for an ORCID URL. Non-DBLP contributors are skipped after a single stat.
	for id, contrib := range Library.ContributorByID {
		if contrib.ORCID != "" {
			continue
		}
		// DBLP persons index is keyed by the natural-order (first-last) form.
		naturalForm := swapBibTeXNameFormat(contrib.Name)
		hash := dblpPersonNameHash(naturalForm)
		if hash == "" || !FileExists(dblpPersonEntriesPath(hash)) {
			hash = dblpPersonNameHash(contrib.Name)
			if hash == "" || !FileExists(dblpPersonEntriesPath(hash)) {
				continue
			}
			naturalForm = contrib.Name
		}
		orcid := resolveNameToORCID(naturalForm)
		if orcid == "" {
			continue
		}
		contrib.ORCID = orcid
		upsertContributorToDB(id, contrib.Name, orcid)
		orcidsSet++
	}

	if orcidsSet > 0 {
		Library.Progress("Set ORCID for %d contributor(s) from DBLP.", orcidsSet)
	}

	Library.Progress("Re-normalising author/editor fields...")
	Library.RenormaliseNameFields()
}

// cleanupDblpXmlFiles removes all but the two most recent dblp-*.xml.gz files
// from dblpFolder(). Called only after a successful import.
func cleanupDblpXmlFiles() {
	des, err := os.ReadDir(dblpFolder())
	if err != nil {
		return
	}
	var xmlFiles []string
	for _, de := range des {
		if !de.IsDir() && reDblpXMLFilename.MatchString(de.Name()) {
			xmlFiles = append(xmlFiles, de.Name())
		}
	}
	sort.Strings(xmlFiles) // dblp-YYYY-MM-DD.xml.gz sorts chronologically by name
	if len(xmlFiles) <= 2 {
		return
	}
	for _, name := range xmlFiles[:len(xmlFiles)-2] {
		path := dblpFolder() + name
		fmt.Fprintf(os.Stderr, "Removing old XML: %s\n", path)
		os.Remove(path)
	}
}
