/*
 *
 * Module: bibtex_library_url_check
 *
 * Plausibility check for url fields: verifies that the URL is reachable and
 * returns meaningful human-readable content. Only applies to entries that have
 * a url field but no doi, isbn, issn, or urldate (those fields already anchor the entry).
 *
 * Results are cached in entry_metadata (url_check_date, url_check_status) and
 * re-checked after urlCheckMaxAgeDays days.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 */

package main

import (
	"bufio"
	"fmt"
	stdlib_html "html"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

const urlCheckMinBytes = 300

// dateFromEntryKey extracts YYYY-MM-DD from a key of the form EP-YYYY-MM-DD-HH-MM-SS.
// Returns "" when the key does not match that pattern.
func dateFromEntryKey(key string) string {
	if len(key) >= 13 && IsValidDate(key[3:13]) {
		return key[3:13]
	}
	return ""
}

// urlParkingPatterns are substrings that signal a parked/error domain rather
// than genuine human content.
var urlParkingPatterns = []string{
	"domain for sale",
	"this domain is for sale",
	"buy this domain",
	"domain parking",
	"parked domain",
	"godaddy.com/offers",
	"sedoparking.com",
	"hugedomains.com",
	"sav.com",
}

// urlCheckPlausible fetches rawURL and returns (ok, reason).
// ok is true when the URL is reachable and the page looks like genuine human content.
func urlCheckPlausible(rawURL string) (bool, string) {
	client := newBibCheckHTTPClient()

	req, err := newBibCheckHTTPRequest(rawURL)
	if err != nil {
		return false, "invalid URL: " + err.Error()
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")

	resp, err := client.Do(req)
	if err != nil {
		return false, "unreachable: " + err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml") {
		// Non-HTML is fine (e.g. a PDF download) — consider it plausible.
		return true, "ok"
	}

	// Read up to 8 KB to check content.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return false, "read error: " + err.Error()
	}
	if len(body) < urlCheckMinBytes {
		return false, fmt.Sprintf("suspiciously small response (%d bytes)", len(body))
	}

	lower := strings.ToLower(string(body))
	for _, pat := range urlParkingPatterns {
		if strings.Contains(lower, pat) {
			return false, "parking page detected"
		}
	}

	return true, "ok"
}

// CheckURLPlausibility checks whether the url field of entry points to live
// human-readable content. Only runs when:
//   - url is set
//   - doi, dblp, isbn, issn are absent (url is the primary identifier)
//   - urldate is absent (entry already has an access date)
//   - the cached check is absent or older than urlCheckMaxAgeDays
//
// On failure: warns and offers r=remove url / w=waive / s=skip.
// On success: silently records the check date.
func (l *TBibTeXLibrary) CheckURLPlausibility(entry *TBibTeXEntry) {
	if !Online {
		return
	}
	url := entry.FieldValue("url")
	if url == "" {
		return
	}
	if entry.FieldValue("doi") != "" || entry.FieldValue(DBLPField) != "" ||
		entry.FieldValue("isbn") != "" || entry.FieldValue("issn") != "" ||
		entry.FieldValue("urldate") != "" {
		return
	}

	key := entry.Key

	// URL is in the ignore list: skip all HTTP work; just derive urldate from year
	// (own or crossref parent, then key date as last resort) so the entry stops
	// qualifying for future URL checks.
	if l.URLsIgnore.Contains(url) {
		year := entry.FieldValue("year")
		if year == "" {
			if crossref := entry.FieldValue("crossref"); crossref != "" {
				year = l.EntryFieldValueity(l.MapEntryKey(crossref), "year")
			}
		}
		var urldate string
		if year != "" {
			urldate = year + "-12-20"
		} else if d := dateFromEntryKey(key); d != "" {
			urldate = d
		} else {
			l.Warning("URL in ignore list but no year to derive urldate: %s — %s", key, url)
			return
		}
		l.Warning(WarningURLDead, "in ignore list", url, urldate)
		l.setEntryField(entry, "urldate", urldate)
		return
	}

	// For PDF URLs: if the local file is absent, attempt to download it now
	// (same logic as -check_pdfs) rather than just doing an HTTP plausibility check.
	// URLCheckNeeded already excludes doi/dblp/isbn/issn, so urldate is always needed here.
	if strings.HasSuffix(strings.ToLower(url), ".pdf") {
		filesDir := l.FilesRoot + l.FilesFolder
		filePath := filesDir + key + ".pdf"
		if !FileExists(filePath) {
			l.Progress("Downloading PDF for %s: %s", key, url)
			if err := downloadPDF(url, filePath); err != nil {
				l.Warning(WarningPDFDownloadFailed, key, url, err)
			} else {
				l.Progress(ProgressPDFDownloaded, key, filePath)
				l.SetEntryFieldValue(key, "urldate", time.Now().Format("2006-01-02"))
			}
			return // handled as a PDF download — skip the HTML plausibility check
		}
	}

	// Skip if already checked — UNLESS the previous run left urldate unset because
	// year was missing but a year is now resolvable via the entry or its crossref parent.
	lastDate := l.GetMetadata(key, MetaPropUrlCheckDate)
	if lastDate != "" {
		prevDead := l.GetMetadata(key, MetaPropUrlCheckStatus) == "dead"
		if !prevDead {
			return
		}
		hasYear := entry.FieldValue("year") != ""
		if !hasYear {
			if crossref := entry.FieldValue("crossref"); crossref != "" {
				hasYear = l.EntryFieldValueity(l.MapEntryKey(crossref), "year") != ""
			}
		}
		if !hasYear {
			hasYear = dateFromEntryKey(key) != ""
		}
		if !hasYear {
			return
		}
	}

	l.Progress("Checking URL for %s: %s", key, url)
	ok, reason := urlCheckPlausible(url)
	today := time.Now().Format("2006-01-02")

	l.SetMetadata(key, MetaPropUrlCheckDate, today)

	if ok {
		l.SetMetadata(key, MetaPropUrlCheckStatus, "ok")
		l.setEntryField(entry, "urldate", today)
		return
	}

	// URL is dead — stamp urldate so the entry is no longer considered undated.
	l.SetMetadata(key, MetaPropUrlCheckStatus, "dead")

	year := entry.FieldValue("year")
	if year == "" {
		if crossref := entry.FieldValue("crossref"); crossref != "" {
			year = l.EntryFieldValueity(l.MapEntryKey(crossref), "year")
		}
	}
	var urldate string
	if year != "" {
		urldate = year + "-12-20"
	} else if d := dateFromEntryKey(key); d != "" {
		urldate = d
	} else {
		l.Warning("URL appears unreachable or lacks human content (%s): %s — year field missing, urldate not set", reason, url)
		return
	}
	l.Warning(WarningURLDead, reason, url, urldate)
	l.setEntryField(entry, "urldate", urldate)
}

// htmlTitlePattern matches a <title>...</title> block (case-insensitive, dotall).
var htmlTitlePattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// htmlOgTitlePattern matches <meta property="og:title" content="..."> in either attribute order.
var htmlOgTitlePattern = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:title["'][^>]+content=["']([^"']+)["']`)
var htmlOgTitlePatternFlipped = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+property=["']og:title["']`)

// fetchHTMLTitle fetches rawURL and returns the page title extracted from the HTML.
// Returns ("", nil) when the URL is non-HTML (e.g. PDF) or the title cannot be found.
func fetchHTMLTitle(rawURL string) (string, error) {
	if strings.HasSuffix(strings.ToLower(rawURL), ".pdf") {
		return "", nil
	}

	client := newBibCheckHTTPClient()
	req, err := newBibCheckHTTPRequest(rawURL)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml") {
		return "", nil
	}

	// 32 KB is enough to cover <head> for any reasonable page.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
	if err != nil {
		return "", err
	}
	html := string(body)

	// og:title is more descriptive than <title> (often includes site suffix).
	for _, pat := range []*regexp.Regexp{htmlOgTitlePattern, htmlOgTitlePatternFlipped} {
		if m := pat.FindStringSubmatch(html); m != nil {
			return stdlib_html.UnescapeString(strings.TrimSpace(m[1])), nil
		}
	}
	if m := htmlTitlePattern.FindStringSubmatch(html); m != nil {
		raw := strings.Join(strings.Fields(m[1]), " ") // collapse whitespace
		return stdlib_html.UnescapeString(raw), nil
	}
	return "", nil
}

// CheckTitleFromURL fetches the URL of a title-less entry and offers the page
// title as a candidate. The user can accept it (y), skip it (n), or type an
// alternative title directly. Only runs when title is empty, url is set, and
// the entry has no crossref (a crossref parent may supply the title later).
func (l *TBibTeXLibrary) CheckTitleFromURL(entry *TBibTeXEntry) {
	if entry.FieldValue("title") != "" || entry.FieldValue("url") == "" {
		return
	}
	if entry.FieldValue("crossref") != "" {
		return
	}

	fetchedTitle, err := fetchHTMLTitle(entry.FieldValue("url"))
	if err != nil || fetchedTitle == "" {
		return
	}

	l.Warning("Entry %s has no title; fetched from URL: %s", entry.Key, fetchedTitle)

	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(os.Stderr, "QUESTION: Accept title (y), skip (n), or type alternative: ")
	for {
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		switch input {
		case "y":
			entry.Fields["title"] = fetchedTitle
			l.setEntryField(entry, "title", fetchedTitle)
			return
		case "n", "":
			return
		default:
			entry.Fields["title"] = input
			l.setEntryField(entry, "title", input)
			return
		}
	}
}

// URLCheckNeeded reports whether entry qualifies for a URL plausibility check.
func URLCheckNeeded(l *TBibTeXLibrary, entry *TBibTeXEntry) bool {
	if entry.FieldValue("url") == "" {
		return false
	}
	if entry.FieldValue("doi") != "" || entry.FieldValue(DBLPField) != "" ||
		entry.FieldValue("isbn") != "" || entry.FieldValue("issn") != "" ||
		entry.FieldValue("urldate") != "" {
		return false
	}
	return true
}

// CheckAllURLs runs CheckURLPlausibility for every qualifying entry.
// Runs as part of the normal check routine; respects -step N.
func (l *TBibTeXLibrary) CheckAllURLs() {
	stepN := Reporting.StepSize()
	checked := 0
	forEachBibEntryKey(func(key string) bool {
		entry := l.buildEntry(key)
		if !URLCheckNeeded(l, entry) {
			return true
		}
		l.CheckURLPlausibility(entry)
		flushWorkingDbToHome()
		checked++
		if stepN > 0 && checked >= stepN {
			return false
		}
		return true
	})
	l.Progress("URL check complete: %d entries checked", checked)
}

