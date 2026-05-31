/*
 *
 * Module: bibtex_library_getpdfs
 *
 * Implements -get_pdfs: downloads missing PDF files for library entries that
 * have a direct-download URL (url field ending in ".pdf").
 *
 * URLs listed in urls_ignore.csv (or the legacy urls.ignore) are skipped.
 * Failed downloads are written to urls_failed.csv at FilesRoot level.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 11.05.2026
 *
 */

package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// downloadPDF fetches url and saves the result to destPath.
// It verifies the downloaded content is a real PDF; HTML paywall redirects
// and other non-PDF responses are rejected with a descriptive error.
func downloadPDF(url, destPath string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; bibtex_check)")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	tmp := destPath + ".download.tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	f.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return copyErr
	}

	if isHTMLFile(tmp) {
		os.Remove(tmp)
		return fmt.Errorf("received HTML (likely a paywall redirect)")
	}
	if !isValidPDF(tmp) {
		os.Remove(tmp)
		return fmt.Errorf("received non-PDF content")
	}

	return os.Rename(tmp, destPath)
}

// writeURLsFailedFile writes a sorted list of CSV lines to path.
func writeURLsFailedFile(path string, lines []string) {
	sort.Strings(lines)
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, line := range lines {
		w.WriteString(line + "\n")
	}
	w.Flush()
}

// GetPDFs downloads missing PDFs for all library entries with a direct-download
// URL (url field ending in ".pdf"), subject to urls_ignore.
// Each failed download is recorded in urls_failed.csv (url; reason; date).
// The file is rewritten on every run so it reflects the current state.
func (l *TBibTeXLibrary) GetPDFs() {
	filesDir := l.FilesRoot + l.FilesFolder
	failedPath := l.FilesRoot + URLsFailedFilePath

	var failedLines []string

	forEachBibEntryKey(func(key string) bool {
		filePath := filesDir + key + ".pdf"
		if FileExists(filePath) {
			return true
		}

		url := l.EntryFieldValueity(key, "url")
		if url != "" && strings.HasSuffix(strings.ToLower(url), ".pdf") {
			if !l.URLsIgnore.Contains(url) {
				needsURLDate := l.EntryFieldValueity(key, "doi") == "" &&
					l.EntryFieldValueity(key, DBLPField) == "" &&
					l.EntryFieldValueity(key, "isbn") == "" &&
					l.EntryFieldValueity(key, "issn") == ""
				missingURLDate := needsURLDate && l.EntryFieldValueity(key, "urldate") == ""

				l.Progress(ProgressDownloadingPDF, key, url)
				if err := downloadPDF(url, filePath); err != nil {
					l.Warning(WarningPDFDownloadFailed, key, url, err)
					failedLines = append(failedLines,
						csvLine(url, err.Error(), time.Now().Format("2006-01-02")))
					if missingURLDate {
						if d, err2 := Reporting.AskForInput(
							"No urldate for " + key + " (download failed). Enter a date (YYYY-MM-DD) or leave blank to skip"); err2 == nil && IsValidDate(d) {
							l.SetEntryFieldValue(key, "urldate", d)
						}
					}
				} else {
					l.Progress(ProgressPDFDownloaded, key, filePath)
					if missingURLDate {
						l.SetEntryFieldValue(key, "urldate", time.Now().Format("2006-01-02"))
					}
				}
			}
		}

		return true
	})

	if len(failedLines) > 0 {
		writeURLsFailedFile(failedPath, failedLines)
	}
}
