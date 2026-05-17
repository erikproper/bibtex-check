/*
 *
 * Module: bibtex_library_pdf
 *
 * PDF health checking: detects invalid or content-less PDF files in the
 * library files folder and offers an interactive open/trash/confirm-OK/skip workflow.
 *
 * Auto-handled cases (no user prompt):
 *   - HTML files disguised as PDF (paywall redirects): deleted.
 *   - PostScript files disguised as PDF: converted via ps2pdf, then re-checked.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 11.05.2026
 *
 */

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// isValidPDF reports whether path exists and begins with the PDF magic bytes "%PDF".
func isValidPDF(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	magic := make([]byte, 4)
	n, _ := f.Read(magic)
	return n == 4 && string(magic) == "%PDF"
}

// isHTMLFile reports whether path contains HTML content — detects paywall
// redirect pages that were saved with a .pdf extension.
func isHTMLFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	header := make([]byte, 512)
	n, _ := f.Read(header)
	s := strings.ToLower(string(header[:n]))
	return strings.Contains(s, "<html") ||
		strings.Contains(s, "<!doctype") ||
		strings.Contains(s, "http/1.")
}

// isPostScriptFile reports whether path is a PostScript file (magic bytes "%!").
func isPostScriptFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	magic := make([]byte, 2)
	n, _ := f.Read(magic)
	return n == 2 && string(magic) == "%!"
}

// convertPSToPDF converts the PostScript file at path to PDF in-place using ps2pdf.
// The original file is replaced by the converted PDF on success.
func convertPSToPDF(path string) error {
	tmp := path + ".ps2pdf.tmp"
	if err := exec.Command("ps2pdf", path, tmp).Run(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// pdfHasContent reports whether a PDF has any readable content.
// It first tries pdftotext; if that finds nothing it escalates to tesseract OCR
// (covering legitimate bitmap scans).  Returns determined=false — and does NOT
// flag the file — when neither tool is available or runnable.
func pdfHasContent(path string) (hasContent, determined bool) {
	out, err := exec.Command("pdftotext", path, "-").Output()
	if err != nil {
		return false, false // pdftotext not available or failed
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return true, true
	}
	// pdftotext found nothing — escalate to OCR for bitmap scans.
	ocrOut, ocrErr := exec.Command("tesseract", path, "stdout", "-l", "eng").Output()
	if ocrErr != nil {
		return false, false // tesseract not available; can't determine
	}
	return len(strings.TrimSpace(string(ocrOut))) > 0, true
}

// trashPDF moves the file at path to the macOS Trash folder (~/.Trash/).
// If a file with the same name already exists in Trash, a timestamp suffix is added.
// Returns false if the move fails.
func trashPDF(path string) bool {
	trashDir := os.Getenv("HOME") + "/.Trash/"
	base := filepath.Base(path)
	dest := trashDir + base
	if FileExists(dest) {
		ts := time.Now().Format("20060102-150405")
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		dest = trashDir + stem + "-" + ts + ext
	}
	return os.Rename(path, dest) == nil
}

// handleBrokenPDF prompts the user for what to do with a suspect PDF.
// Options: o=open in viewer, t=trash, k=keep-as-ok (suppress future warnings), s=skip, q=quit.
// Returns true if the user chose to quit the loop.
func (l *TBibTeXLibrary) handleBrokenPDF(key, path, reason string) bool {
	options := TStringSetNew()
	options.Add("o", "t", "k", "s", "q")

	for {
		answer := l.WarningQuestion(
			"What to do? (o=open, t=trash, k=keep-as-ok, s=skip, q=quit)",
			options, WarningBrokenPDF, key, reason, path)

		switch answer {
		case "o":
			exec.Command("open", path).Start() //nolint:errcheck
		case "t":
			if !trashPDF(path) {
				l.Warning("Could not move %s to Trash.", path)
			}
			return false
		case "k":
			l.PDFConfirmedOk[key] = time.Now().Format("2006-01-02")
			l.pdfConfirmedOkModified = true
			return false
		case "s":
			return false
		case "q":
			return true
		}
	}
}

// checkOnePDF determines whether the PDF at fullPath is healthy.
// Auto-actions (empty delete, HTML delete, PS conversion) are performed silently.
// Returns a non-empty reason string when the file should be flagged to the user.
func (l *TBibTeXLibrary) checkOnePDF(key, fullPath string) string {
	if info, err := os.Stat(fullPath); err == nil && info.Size() == 0 {
		l.Warning(WarningEmptyPDFFile, key, fullPath)
		os.Remove(fullPath)
		return ""
	}

	if !isValidPDF(fullPath) {
		if isHTMLFile(fullPath) {
			l.Warning(WarningHTMLDisguisedAsPDF, key, fullPath)
			os.Remove(fullPath)
			return ""
		}
		if isPostScriptFile(fullPath) {
			if err := convertPSToPDF(fullPath); err != nil {
				l.Warning(WarningPSConversionFailed, key, err)
				return "PostScript file; ps2pdf conversion failed"
			}
			l.Progress(ProgressConvertedPSToPDF, key)
			// After conversion fall through to the content check below.
			if !isValidPDF(fullPath) {
				return "ps2pdf produced an invalid PDF"
			}
		} else {
			return "not a valid PDF file (wrong file type)"
		}
	}

	if hasContent, determined := pdfHasContent(fullPath); determined && !hasContent {
		return "no text content (possibly corrupted or scan-only)"
	}
	return ""
}

// CheckPDFHealth walks the library files folder and performs all PDF checks in
// one pass: orphaned files (no library entry), duplicate-content detection
// (via MD5), and PDF health checks (magic bytes, content).
//
// For each .pdf file:
//   - No matching library entry → warned as orphaned.
//   - Duplicate content (same MD5 as another file) → prompts to merge entries.
//   - HTML disguised as PDF → deleted automatically (no prompt).
//   - PostScript disguised as PDF → converted via ps2pdf, then re-checked.
//   - Valid PDF with no content (pdftotext + OCR both empty) → interactive prompt.
//   - File passes check → any stale confirmed-OK entry is removed.
//   - File fails check but is already confirmed-OK → silently skipped.
func (l *TBibTeXLibrary) CheckPDFHealth() {
	filesDir := l.FilesRoot + l.FilesFolder
	l.Progress(ProgressCheckingPDFHealth, filesDir)

	files, err := os.ReadDir(filesDir)
	if err != nil {
		return
	}

	var pdfFiles []string
	for _, e := range files {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".pdf") {
			pdfFiles = append(pdfFiles, e.Name())
		}
	}

	total := len(pdfFiles)
	count := 0
	md5Index := TStringSetMap{}

	for _, fileName := range pdfFiles {
		count++
		l.Progress(ProgressFileProgress, count, total, float64(count)*100/float64(total))
		key := strings.TrimSuffix(fileName, ".pdf")
		fullPath := filesDir + fileName

		if !l.EntryExists(key) {
			l.Warning(WarningFileNotAssociated, fileName)
			continue
		}

		md5Index.AddValueToStringSetMap(MD5ForFile(fullPath), key)

		reason := l.checkOnePDF(key, fullPath)
		if reason == "" {
			if _, ok := l.PDFConfirmedOk[key]; ok {
				delete(l.PDFConfirmedOk, key)
				l.pdfConfirmedOkModified = true
			}
			continue
		}
		if _, ok := l.PDFConfirmedOk[key]; ok {
			continue
		}
		if l.handleBrokenPDF(key, fullPath, reason) {
			return
		}
	}

	for _, keys := range md5Index {
		if keys.Size() > 1 {
			l.Warning(WarningDuplicateFileContent, keys.String())
			l.MaybeMergeEntrySet(keys)
		}
	}
}
