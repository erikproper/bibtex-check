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
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
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

// dblpCollectKeysFromXML streams the DBLP XML reading only the key= attribute of each
// entry start element (skipping the entire entry body), and returns the resulting manifest.
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
		for _, a := range se.Attr {
			if a.Name.Local == "key" && a.Value != "" {
				m.add(a.Value, "") // key-only collection; mdate unknown
				break
			}
		}
		d.Skip() // skip all child elements — we only needed the key
		count++
		if count%dblpProgressInterval == 0 && progress != nil {
			progress(count)
		}
	}
	return m, nil
}

// doRepairDblpManifest rebuilds the DBLP manifest files from an XML export
// without touching any data.json files. The manifest written reflects exactly
// the set of DBLP keys present in the XML file.
func doRepairDblpManifest(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: -repair_dblp_manifest <path.xml.gz>\n")
		os.Exit(1)
	}
	xmlGzPath := expandHome(args[0])
	if !FileExists(xmlGzPath) {
		fmt.Fprintf(os.Stderr, "File not found: %s\n", xmlGzPath)
		os.Exit(1)
	}

	f, err := os.Open(xmlGzPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open %s: %s\n", xmlGzPath, err)
		os.Exit(1)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not read gzip from %s: %s\n", xmlGzPath, err)
		os.Exit(1)
	}
	defer gz.Close()

	fmt.Fprintf(os.Stderr, "Collecting DBLP keys from %s...\n", xmlGzPath)
	start := time.Now()
	m, err := dblpCollectKeysFromXML(gz, func(n int) {
		fmt.Fprintf(os.Stderr, "  %d keys collected (%.0fs)...\n", n, time.Since(start).Seconds())
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: XML read error after %d keys: %s\n", len(m), err)
	}

	total := 0
	for _, entries := range m {
		total += len(entries)
	}
	fmt.Fprintf(os.Stderr, "  %d keys in %d parent dirs (%.0fs).\n", total, len(m), time.Since(start).Seconds())

	fmt.Fprintf(os.Stderr, "Writing manifests...\n")
	if err := os.MkdirAll(dblpFolder()+"entries/", 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Could not create entries folder: %s\n", err)
		os.Exit(1)
	}
	writeDblpManifests(m)
	fmt.Fprintf(os.Stderr, "Manifest repair complete.\n")
}

// --- First pass: name map ---

// dblpBuildNameMap does a first streaming pass over the DBLP XML, collecting
// only www (person homepage) entries to build a plain-name → canonical-name map.
// The canonical name comes from the bibtex= attribute of the first author that
// carries one; the disambiguation suffix (e.g. " 0001") is stripped.
func dblpBuildNameMap(r io.Reader) (map[string]string, error) {
	d := newDblpDecoder(r)
	if err := advanceToDblpRoot(d); err != nil {
		return nil, err
	}

	nameMap := make(map[string]string)
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nameMap, fmt.Errorf("building name map: %w", err)
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
	childLoop:
		for {
			child, cerr := d.Token()
			if cerr != nil {
				break
			}
			switch ct := child.(type) {
			case xml.StartElement:
				if ct.Name.Local != "author" {
					d.Skip()
					continue
				}
				var bibtex string
				for _, a := range ct.Attr {
					if a.Name.Local == "bibtex" {
						bibtex = a.Value
					}
				}
				text, _ := xmlCollectText(d)
				authors = append(authors, dblpXMLPerson{Name: text, Bibtex: bibtex})
			case xml.EndElement:
				break childLoop
			}
		}

		var canonicalName string
		for _, p := range authors {
			if p.Bibtex != "" {
				canonicalName = dblpDisambigSuffix.ReplaceAllString(applyUnicodeMap(p.Bibtex), "")
				break
			}
		}
		if canonicalName != "" {
			for _, p := range authors {
				if p.Name != canonicalName {
					nameMap[p.Name] = canonicalName
				}
			}
		}
	}
	return nameMap, nil
}

// --- Second pass: main import ---

const dblpProgressInterval = 50_000

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

		// Skip entries whose mdate hasn't changed — no need to rewrite files or
		// update indexes. Check manifest first (O(1)); fall back to data.json only
		// when the entry is absent from the manifest (e.g. after a repair).
		if mdate != "" {
			skip := false
			if existingMdate, inManifest := originalManifest.mdate(dblpKey); inManifest {
				skip = existingMdate == mdate
			} else if existing := readDblpJSONEntry(dblpKey); existing != nil {
				skip = existing.Mdate == mdate
			}
			if skip {
				d.Skip()
				newManifest.add(dblpKey, mdate)
				count++
				if count%dblpProgressInterval == 0 && progress != nil {
					progress(count)
				}
				continue
			}
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

		if err := writeDblpEntryFile(dblpKey, je); err != nil {
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

		// Write crossref child link: record this entry as a child of its parent.
		if je.Fields != nil {
			if parentKey, ok := je.Fields["crossref"]; ok && parentKey != "" {
				writeDblpCrossrefChild(parentKey, dblpKey) // non-fatal
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

	if meta := readDblpMeta(); meta != nil {
		fmt.Fprintf(os.Stderr, "Existing DBLP store: %d entries from %s (loaded %s)\n",
			meta.EntryCount, meta.XMLFile, meta.LoadedAt)
	}

	if err := os.MkdirAll(dblpFolder()+"entries/", 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Could not create DBLP entries folder: %s\n", err)
		os.Exit(1)
	}

	// Clear derived indexes so they are rebuilt cleanly from the new XML.
	for _, indexDir := range []string{"titles/", "crossrefs/", "persons/", "orcids/"} {
		dir := dblpFolder() + indexDir
		if _, err := os.Stat(dir); err == nil {
			fmt.Fprintf(os.Stderr, "Clearing existing %s index...\n", indexDir)
			if err := os.RemoveAll(dir); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not clear %s index: %s\n", indexDir, err)
			}
		}
	}

	// Load existing manifests so we know which entries to prune after import.
	// If entries exist but manifests are absent, rebuild them from the tree first.
	var originalManifest TDblpManifest
	if dblpEntriesDirHasContent() {
		originalManifest = loadDblpManifests()
		if len(originalManifest) == 0 {
			fmt.Fprintf(os.Stderr, "Manifests absent — rebuilding from existing entries...\n")
			if err := rebuildDblpManifests(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: manifest rebuild failed: %s\n", err)
			} else {
				originalManifest = loadDblpManifests()
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
	nameMap, err := dblpBuildNameMap(gz1)
	gz1.Close()
	f1.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: name map build error: %s\n", err)
	}
	fmt.Fprintf(os.Stderr, "  %d name mappings collected (%.0fs).\n", len(nameMap), time.Since(start).Seconds())

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
	count, err := dblpImportFromReader(gz2, nameMap, originalManifest, newManifest, func(n int) {
		pct := float64(cr.n) * 100.0 / float64(totalBytes)
		fmt.Fprintf(os.Stderr, "  %d entries written (%.0fs, %.0f%%)...\n",
			n, time.Since(pass2Start).Seconds(), pct)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Import failed after %d entries: %s\n", count, err)
		os.Exit(1)
	}

	// Prune entries present in the old store but absent from the new XML.
	fmt.Fprintf(os.Stderr, "Pruning stale entries...\n")
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

	// Import succeeded — keep only the two most recent XML files.
	cleanupDblpXmlFiles()
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
