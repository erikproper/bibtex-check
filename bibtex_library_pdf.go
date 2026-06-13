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
	"fmt"
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

// LoadPDFFiles scans FilesRoot+FilesFolder and populates PDFFiles with the key stem of
// every <key>.pdf file found. Called once at library open time; must be refreshed whenever
// files are added or removed during a run (e.g. after ScanOrphanPDFs moves a file).
func (l *TBibTeXLibrary) LoadPDFFiles() {
	l.PDFFiles = map[string]bool{}
	entries, err := os.ReadDir(l.FilesRoot + l.FilesFolder)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".pdf") {
			continue
		}
		l.PDFFiles[strings.TrimSuffix(e.Name(), ".pdf")] = true
	}
}

// libraryTrashFolder returns the path to the library-local trash folder
// (<FilesRoot><BaseName>.trash/), creating it if necessary.
func (l *TBibTeXLibrary) libraryTrashFolder() string {
	dir := l.FilesRoot + l.BaseName + ".trash/"
	_ = os.MkdirAll(dir, 0755)
	return dir
}

// moveToLibraryTrash moves a file into the library's own .trash/ folder.
// If a file with the same name already exists, a timestamp suffix is added.
// Returns false if the move fails.
func (l *TBibTeXLibrary) moveToLibraryTrash(path string) bool {
	trashDir := l.libraryTrashFolder()
	base := filepath.Base(path)
	dest := trashDir + base
	if FileExists(dest) {
		ts := time.Now().Format("20060102-150405")
		ext := filepath.Ext(base)
		dest = trashDir + strings.TrimSuffix(base, ext) + "-" + ts + ext
	}
	return os.Rename(path, dest) == nil
}

// ScanOrphanPDFs does a quick scan of the library files folder.
// For each <key>.pdf:
//   - If key is a live canonical entry → keep (nothing to do).
//   - If key is an alias for a canonical entry → rename to <canonical>.pdf.
//     If <canonical>.pdf already exists, the old file is moved to trash instead.
//   - Otherwise → no library entry at all → moved to library trash.
// After the scan, PDFFiles is refreshed from disk.
// Intended for the normal check run and sync writes (lightweight — no PDF parsing).
func (l *TBibTeXLibrary) ScanOrphanPDFs() {
	filesDir := l.FilesRoot + l.FilesFolder
	entries, err := os.ReadDir(filesDir)
	if err != nil {
		return
	}
	trashName := l.BaseName + ".trash"
	moved := 0
	renamed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".pdf") {
			continue
		}
		key := strings.TrimSuffix(e.Name(), ".pdf")
		fullPath := filesDir + e.Name()

		if l.EntryExists(key) {
			continue // canonical entry — all good
		}

		// Check whether the key is an alias pointing to a live canonical entry.
		if canonical := l.MapEntryKey(key); canonical != "" && canonical != key && l.EntryExists(canonical) {
			canonicalPath := filesDir + canonical + ".pdf"
			if FileExists(canonicalPath) {
				// Canonical PDF already present — the old-key file is truly redundant.
				l.Warning("Merged entry had duplicate PDF: %s → trashing (canonical already has %s.pdf)", e.Name(), canonical)
				if !l.moveToLibraryTrash(fullPath) {
					l.Warning("Could not move %s to %s", e.Name(), trashName)
				} else {
					moved++
				}
			} else {
				if err := os.Rename(fullPath, canonicalPath); err != nil {
					l.Warning("Could not rename %s → %s.pdf: %s", e.Name(), canonical, err)
				} else {
					l.Progress("Renamed PDF for merged entry: %s → %s.pdf", e.Name(), canonical)
					renamed++
				}
			}
			continue
		}

		// No entry, no alias — genuine orphan.
		l.Warning("Orphaned PDF (no library entry): %s — moving to %s", e.Name(), trashName)
		if !l.moveToLibraryTrash(fullPath) {
			l.Warning("Could not move %s to %s", e.Name(), trashName)
		} else {
			moved++
		}
	}
	if moved > 0 {
		l.Progress("Orphaned PDFs moved to trash: %d", moved)
	}
	if renamed > 0 || moved > 0 {
		l.LoadPDFFiles() // refresh after renames/moves
	}
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
			l.SetMetadata(key, MetaPropPdfConfirmedOk, time.Now().Format("2006-01-02"))
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
	spinner := l.NewSpinner(fmt.Sprintf(ProgressCheckingPDFHealth, filesDir))

	for _, fileName := range pdfFiles {
		count++
		spinner.Update(count, total)
		key := strings.TrimSuffix(fileName, ".pdf")
		fullPath := filesDir + fileName

		if !l.EntryExists(key) {
			l.Warning(WarningFileNotAssociated, fileName)
			continue
		}

		md5Index.AddValueToStringSetMap(MD5ForFile(fullPath), key)

		reason := l.checkOnePDF(key, fullPath)
		if reason == "" {
			if l.HasMetadata(key, MetaPropPdfConfirmedOk) {
				l.DeleteMetadata(key, MetaPropPdfConfirmedOk)
			}
			continue
		}
		if l.HasMetadata(key, MetaPropPdfConfirmedOk) {
			continue
		}
		if l.handleBrokenPDF(key, fullPath, reason) {
			spinner.Stop()
			return
		}
	}
	spinner.Stop()

	validAnswers := TStringSetNew()
	validAnswers.Add("w", "m", "s")

	for md5hash, keys := range md5Index {
		if keys.Size() <= 1 {
			continue
		}

		// Skip when every entry in the group has already waived this exact PDF.
		allWaived := true
		for key := range keys.Elements() {
			if l.GetMetadata(key, MetaPropWaivedDoublePdf) != md5hash {
				allWaived = false
				break
			}
		}
		if allWaived {
			continue
		}

		l.Warning(WarningDuplicateFileContent, keys.String())
		switch l.WarningQuestion(QuestionDoublePdfWaive, validAnswers, "") {
		case "w":
			for key := range keys.Elements() {
				l.SetMetadata(key, MetaPropWaivedDoublePdf, md5hash)
			}
		case "m":
			l.MaybeMergeEntrySet(keys)
		}
	}
}
