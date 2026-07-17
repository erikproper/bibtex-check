/*
 *
 * Module: bibtex_library_align_titles
 *
 * This module implements detection and reporting of volume/edition information
 * embedded in title and booktitle fields that should be extracted into the
 * corresponding volume/edition fields.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 30.05.2026
 *
 */

package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// reVolumeKw matches a volume keyword with optional paired LaTeX braces.
const reVolumeKw = `(?:\{(?:volume|vol\.|band|bd\.)\}|volume|vol\.|band|bd\.)`

// reVolumeNum matches a volume token: optionally braced integer or Roman numeral,
// with an optional range suffix (e.g. 1-3, I--III).
const reVolumeNum = `(?:\{(?:\d+|[IVX]+)(?:--?(?:\d+|[IVX]+))?\}|(?:\d+|[IVX]+)(?:--?(?:\d+|[IVX]+))?)`

// reEditionKw matches an edition keyword with optional paired LaTeX braces.
const reEditionKw = `(?:\{(?:edition|ed\.|auflage|aufl\.)\}|edition|ed\.|auflage|aufl\.)`

// reEditionNum matches an edition token: optionally braced integer (with optional
// decimal and/or ordinal suffix) or Roman numeral.
const reEditionNum = `(?:\{(?:\d+(?:\.\d+)?(?:st|nd|rd|th)?|[IVX]+)\}|\d+(?:\.\d+)?(?:st|nd|rd|th)?|[IVX]+)`

// titleVolumeRe detects keyword-first volume markers: "Volume N", "Band N", etc.
// Groups: (1) separator before keyword, (2) raw volume token, (3) trailing text.
var titleVolumeRe = regexp.MustCompile(
	`(?i)(\s*--\s*|,\s*|\.\s*)` + reVolumeKw + `\s+(` + reVolumeNum + `)(.*)`)

// titleEditionKwRe detects keyword-first edition markers: "Edition N", "Auflage N", etc.
// Groups: (1) separator, (2) raw edition token, (3) trailing text.
var titleEditionKwRe = regexp.MustCompile(
	`(?i)(\s*--\s*|,\s*|\.\s*)` + reEditionKw + `\s+(` + reEditionNum + `)(.*)`)

// titleEditionNumFirstRe detects number-first edition markers: "2nd Edition",
// "2. Auflage", etc.
// The optional dot after the number handles German ordinal notation.
// Groups: (1) separator, (2) raw edition token, (3) trailing text.
var titleEditionNumFirstRe = regexp.MustCompile(
	`(?i)(\s*--\s*|,\s*|\.\s*)(` + reEditionNum + `)\.?\s+` + reEditionKw + `(.*)`)

// titleGapCleaners repairs the separator gap left after removing the volume
// keyword and number. Applied in order; the patterns are mutually exclusive.
var titleGapCleaners = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`--\s*--\s*`), "-- "}, // Y -- -- Z →  Y -- Z
	{regexp.MustCompile(`--\s*,\s*`), "-- "},  // Y -- , Z  →  Y -- Z
	{regexp.MustCompile(`--\s*:\s*`), "-- "},  // Y -- : Z  →  Y -- Z
	{regexp.MustCompile(`\s*--\s*\(`), " ("},  // Y -- ( Z  →  Y ( Z
	{regexp.MustCompile(`,\s*:\s*`), ": "},    // Y, : Z    →  Y: Z
	{regexp.MustCompile(`,\s*--\s*`), " -- "}, // Y, -- Z   →  Y -- Z
	{regexp.MustCompile(`,\s*,`), ","},         // Y, , Z    →  Y, Z
	{regexp.MustCompile(`,\s*\(`), " ("},      // Y, ( Z    →  Y ( Z
	{regexp.MustCompile(`,\s*\}`), "}"},        // Y, }      →  Y}  (closing brace)
	{regexp.MustCompile(`\s*--\s*\}`), "}"},    // Y -- }    →  Y}  (closing brace)
	{regexp.MustCompile(`,\s*$`), ""},          // Y,        →  Y  (Z empty)
	{regexp.MustCompile(`\s*--\s*$`), ""},      // Y --      →  Y  (Z empty)
	{regexp.MustCompile(`\.\s*$`), ""},         // Y.        →  Y  (Z empty, period sep)
	{regexp.MustCompile(`\.\s*\}`), "}"},       // Y. }      →  Y}  (closing brace)
}

type titleAlignHit struct {
	fields      []string // "title", "booktitle", or both when equal
	original    string   // field value as stored
	kind        string   // "volume" or "edition"
	extracted   string   // extracted value (braces stripped, leading zeros removed)
	sep         string   // separator before the keyword
	suffix      string   // text following the extracted token
	proposed    string   // suggested clean title
	targetField string   // current value of the volume or edition field
}

// proposedCleanTitle rebuilds the title by removing the keyword+number and
// cleaning the separator gap between what came before and after it.
func proposedCleanTitle(base, sep, suffix string) string {
	gap := sep + suffix
	for _, c := range titleGapCleaners {
		gap = c.re.ReplaceAllString(gap, c.repl)
	}
	return strings.TrimRight(base+gap, " ")
}

// detectTitleVolumes scans title and booktitle for embedded volume markers.
// When both fields carry the same value the hit is reported once, listing both
// field names.
func detectTitleVolumes(l *TBibTeXLibrary, key string) []titleAlignHit {
	entryType := l.EntryFieldValueity(key, EntryTypeField)
	if !BibTeXAllowedEntryFields[entryType].Set().Contains("volume") {
		return nil
	}
	if entryType == "proceedings" {
		publisher := l.EntryFieldValueity(key, "publisher")
		if strings.Contains(strings.ToLower(publisher), "springer") {
			return nil
		}
	}
	var hits []titleAlignHit
	checked := map[string]bool{}
	for _, field := range []string{TitleField, "booktitle"} {
		val := l.EntryFieldValueity(key, field)
		if val == "" || checked[val] {
			continue
		}
		checked[val] = true
		m := titleVolumeRe.FindStringSubmatchIndex(val)
		if m == nil {
			continue
		}
		var fields []string
		for _, f := range []string{TitleField, "booktitle"} {
			if l.EntryFieldValueity(key, f) == val {
				fields = append(fields, f)
			}
		}
		base := val[:m[0]]
		sep := val[m[2]:m[3]]
		extracted := strings.Trim(val[m[4]:m[5]], "{}")
		if stripped := strings.TrimLeft(extracted, "0"); stripped != "" {
			extracted = stripped
		}
		suffix := val[m[6]:m[7]]
		hits = append(hits, titleAlignHit{
			fields:      fields,
			kind:        "volume",
			original:    val,
			extracted:   extracted,
			sep:         sep,
			suffix:      suffix,
			proposed:    proposedCleanTitle(base, sep, suffix),
			targetField: l.EntryFieldValueity(key, "volume"),
		})
	}
	return hits
}

// detectTitleEditions scans title and booktitle for embedded edition markers.
// When both fields carry the same value the hit is reported once, listing both
// field names.
func detectTitleEditions(l *TBibTeXLibrary, key string) []titleAlignHit {
	entryType := l.EntryFieldValueity(key, EntryTypeField)
	if !BibTeXAllowedEntryFields[entryType].Set().Contains("edition") {
		return nil
	}
	var hits []titleAlignHit
	checked := map[string]bool{}
	for _, field := range []string{TitleField, "booktitle"} {
		val := l.EntryFieldValueity(key, field)
		if val == "" || checked[val] {
			continue
		}
		checked[val] = true
		m := titleEditionKwRe.FindStringSubmatchIndex(val)
		if m == nil {
			m = titleEditionNumFirstRe.FindStringSubmatchIndex(val)
		}
		if m == nil {
			continue
		}
		var fields []string
		for _, f := range []string{TitleField, "booktitle"} {
			if l.EntryFieldValueity(key, f) == val {
				fields = append(fields, f)
			}
		}
		base := val[:m[0]]
		sep := val[m[2]:m[3]]
		extracted := strings.Trim(val[m[4]:m[5]], "{}")
		suffix := val[m[6]:m[7]]
		hits = append(hits, titleAlignHit{
			fields:      fields,
			kind:        "edition",
			original:    val,
			extracted:   extracted,
			sep:         sep,
			suffix:      suffix,
			proposed:    proposedCleanTitle(base, sep, suffix),
			targetField: l.EntryFieldValueity(key, "edition"),
		})
	}
	return hits
}

// waiverPropForKind returns the metadata property key for the alignment waiver
// of the given kind ("volume", "edition", or "country").
func waiverPropForKind(kind string) string {
	switch kind {
	case "volume":
		return MetaPropAlignVolumeWaived
	case "edition":
		return MetaPropAlignEditionWaived
	default:
		return MetaPropAlignCountryWaived
	}
}

// applyAlignHit writes newTitle to every affected field and, for volume/edition
// hits where the target field is currently empty, sets it to the extracted value.
func (l *TBibTeXLibrary) applyAlignHit(key string, h titleAlignHit, newTitle string) {
	for _, field := range h.fields {
		l.SetEntryFieldValue(key, field, newTitle)
		l.UpdateEntryFieldAlias(key, field, h.original, newTitle)
	}
	if h.kind != "country" && h.targetField == "" {
		l.SetEntryFieldValue(key, h.kind, h.extracted)
		l.setLineage(key, h.kind, "dblp", false)
	}
	bibEntriesModified = true
}

// handleAlignHit displays one alignment candidate and prompts the user for an
// action.  Returns true when the user chose to quit the entire scan.
func (l *TBibTeXLibrary) handleAlignHit(key string, h titleAlignHit, autoAccept bool) bool {
	if autoAccept {
		l.applyAlignHit(key, h, h.proposed)
		return false
	}
	l.printWarningLine("Title/booktitle alignment (%s) for %s", h.kind, key)
	fmt.Fprintf(os.Stderr, "Field(s): %s\n", strings.Join(h.fields, ", "))
	fmt.Fprintf(os.Stderr, "Current:  %s\n", h.original)
	if h.kind != "country" {
		label := strings.ToUpper(h.kind[:1]) + h.kind[1:]
		value := h.extracted
		if h.targetField != "" && strings.Trim(h.targetField, "{}") != h.extracted {
			value += fmt.Sprintf(" (%s field: %s)", h.kind, h.targetField)
		}
		fmt.Fprintf(os.Stderr, "%-9s %s\n", label+":", value)
	}
	fmt.Fprintf(os.Stderr, "Proposed: %s\n", h.proposed)
	fmt.Fprint(os.Stderr, "QUESTION: Action (a=accept, m=modify, s=skip, w=waive, q=quit): ")
	for {
		option := readStdinLine()
		switch option {
		case "a":
			l.applyAlignHit(key, h, h.proposed)
			return false
		case "m":
			newTitle, err := l.AskForInput("New title (Enter = keep proposed)")
			if err != nil || newTitle == "" {
				newTitle = h.proposed
			}
			l.applyAlignHit(key, h, newTitle)
			return false
		case "s":
			return false
		case "w":
			l.SetMetadata(key, waiverPropForKind(h.kind), time.Now().Format("2006-01-02"))
			return false
		case "q":
			return true
		default:
			fmt.Fprint(os.Stderr, "(a/m/s/w/q): ")
		}
	}
}

// detectBooktitleCountryIssues scans a single entry's booktitle (and title, when
// equal to booktitle for bookish entries) for country-name forms that would be
// normalised to a different canonical by NormaliseBooktitleLocationNames — i.e.,
// unbraced forms like "USA" whose canonical is the brace-protected "{USA}".
// Waived entries are excluded by the caller (CheckAlignBooktitleCountries).
func detectBooktitleCountryIssues(l *TBibTeXLibrary, key string) []titleAlignHit {
	bt := l.EntryFieldValueity(key, "booktitle")
	if bt == "" {
		return nil
	}
	normalised := l.NormaliseBooktitleLocationNames(bt)
	if normalised == bt {
		return nil
	}
	// Determine which fields share this booktitle value.
	fields := []string{"booktitle"}
	if l.EntryFieldValueity(key, TitleField) == bt {
		fields = []string{TitleField, "booktitle"}
	}
	return []titleAlignHit{{
		fields:   fields,
		kind:     "country",
		original: bt,
		proposed: normalised,
	}}
}

// CheckAlignBooktitleCountries scans all entries for unbraced country names in
// booktitle fields. It works in two phases:
//  1. Collect all candidates and print a full report (current → proposed).
//  2. Ask whether to accept all, fix interactively, or quit without changes.
func (l *TBibTeXLibrary) CheckAlignBooktitleCountries() {
	type keyedHit struct {
		key string
		hit titleAlignHit
	}
	var allHits []keyedHit

	total := countBibEntries()
	ticker := l.NewProgressTicker("Scanning for booktitle country normalisation candidates", total)
	forEachBibEntryKey(func(key string) bool {
		if ticker.Step() {
			return false
		}
		if l.HasMetadata(key, MetaPropAlignCountryWaived) {
			return true
		}
		for _, h := range detectBooktitleCountryIssues(l, key) {
			allHits = append(allHits, keyedHit{key, h})
		}
		return true
	})
	ticker.Done()

	if len(allHits) == 0 {
		fmt.Fprintf(os.Stderr, "No booktitle country normalisation candidates found.\n")
		return
	}

	// Phase 1: report.
	for _, kh := range allHits {
		fmt.Printf("Key:      %s\n", kh.key)
		fmt.Printf("Field(s): %s\n", strings.Join(kh.hit.fields, ", "))
		fmt.Printf("Current:  %s\n", kh.hit.original)
		fmt.Printf("Proposed: %s\n", kh.hit.proposed)
		fmt.Println()
	}
	fmt.Fprintf(os.Stderr, "Found %d candidate(s)\n", len(allHits))

	// Phase 2: action.
	fmt.Fprint(os.Stderr, "QUESTION: Proceed? (a=accept all, i=interactive, q=quit): ")
	for {
		option := readStdinLine()
		switch option {
		case "a":
			for _, kh := range allHits {
				l.applyAlignHit(kh.key, kh.hit, kh.hit.proposed)
			}
			return
		case "i":
			for _, kh := range allHits {
				SpinnerInterrupt()
				if l.handleAlignHit(kh.key, kh.hit, false) {
					return
				}
			}
			return
		case "q":
			return
		default:
			fmt.Fprint(os.Stderr, "(a/i/q): ")
		}
	}
}

// CheckAlignTitles scans all entries for embedded volume/edition information
// and presents an interactive prompt for each non-waived candidate.
func (l *TBibTeXLibrary) CheckAlignTitles(autoAccept bool) {
	total := countBibEntries()
	found := 0
	quit := false
	ticker := l.NewProgressTicker("  Scanning for title/volume/edition alignment candidates", total)
	forEachBibEntryKey(func(key string) bool {
		if quit {
			return false
		}
		if ticker.Step() {
			return false
		}
		hits := append(detectTitleVolumes(l, key), detectTitleEditions(l, key)...)
		for _, h := range hits {
			if l.HasMetadata(key, waiverPropForKind(h.kind)) {
				continue
			}
			found++
			SpinnerInterrupt()
			if l.handleAlignHit(key, h, autoAccept) {
				quit = true
				return false
			}
		}
		return true
	})
	ticker.Done()
	if found > 0 {
		fmt.Fprintf(os.Stderr, "  Found %d title/volume/edition alignment candidate(s)\n", found)
	}
}


