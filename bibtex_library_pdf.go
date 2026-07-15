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
 * Version of: 22.06.2026
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

// moveToTrash moves path into trashDir, creating it if necessary.
// If a file with the same name already exists in trashDir, a timestamp suffix is added.
func moveToTrash(path, trashDir string) bool {
	if err := os.MkdirAll(trashDir, 0755); err != nil {
		return false
	}
	base := filepath.Base(path)
	dest := filepath.Join(trashDir, base)
	if FileExists(dest) {
		ts := time.Now().Format("20060102-150405")
		ext := filepath.Ext(base)
		dest = filepath.Join(trashDir, strings.TrimSuffix(base, ext)+"-"+ts+ext)
	}
	return os.Rename(path, dest) == nil
}

// pdfFileInfo returns a human-readable summary of a PDF file's size and modification time.
func pdfFileInfo(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "?"
	}
	return fmt.Sprintf("%.1f KB, %s", float64(info.Size())/1024, info.ModTime().Format("2006-01-02 15:04"))
}

// openBothPDFs opens two PDF files in the system default viewer.
func openBothPDFs(path1, path2 string) {
	exec.Command("open", path1).Start() //nolint:errcheck
	exec.Command("open", path2).Start() //nolint:errcheck
}

// syncLocalPDFs synchronises global PDFs with the local .files/ folder for pdf_files="local".
// trashDir is the <bib>.trash/ folder; orphaned local PDFs are moved there rather than deleted.
//
//   - New global PDF → copy to local silently.
//   - Global newer than local (trusted) → overwrite local silently.
//   - Global newer than local (interactive) → show ages+sizes, ask user.
//   - Local newer than global → always ask; offer to open both in viewer first.
//   - Local PDF no longer in output pairs → move to trashDir.
func (l *TBibTeXLibrary) syncLocalPDFs(localFilesDir, trashDir string, pairs []TBibGetPair, trusted bool) {
	if err := os.MkdirAll(localFilesDir, 0755); err != nil {
		l.Warning("Cannot create local files dir %s: %s", localFilesDir, err)
		return
	}

	// Build expected set of local PDF names from pairs that have a global PDF.
	expected := map[string]bool{}
	for _, p := range pairs {
		if l.PDFFiles[p.canonicalKey] {
			expected[p.localKey+".pdf"] = true
		}
	}

	// Move orphaned local PDFs (present locally, no longer in output) to trash.
	if entries, err := os.ReadDir(localFilesDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".pdf" {
				continue
			}
			if !expected[entry.Name()] {
				src := localFilesDir + entry.Name()
				if !moveToTrash(src, trashDir) {
					l.Warning("Cannot move orphaned local PDF to trash: %s", entry.Name())
				}
			}
		}
	}

	// Sync each pair.
	options := TStringSetNew()
	options.Add("l", "g", "o", "s")

	for _, p := range pairs {
		if !l.PDFFiles[p.canonicalKey] {
			continue
		}
		globalPath := l.FilesRoot + l.FilesFolder + p.canonicalKey + ".pdf"
		localPath := localFilesDir + p.localKey + ".pdf"

		if !FileExists(localPath) {
			if err := copyFile(globalPath, localPath); err != nil {
				l.Warning("Cannot copy PDF %s → %s: %s", p.canonicalKey+".pdf", p.localKey+".pdf", err)
			}
			continue
		}
		if MD5ForFile(globalPath) == MD5ForFile(localPath) {
			continue
		}

		globalInfo, globalErr := os.Stat(globalPath)
		localInfo, localErr := os.Stat(localPath)
		if globalErr != nil || localErr != nil {
			continue
		}
		globalNewer := globalInfo.ModTime().After(localInfo.ModTime())

		if globalNewer {
			if trusted {
				if err := copyFile(globalPath, localPath); err != nil {
					l.Warning("Cannot overwrite local PDF %s: %s", p.localKey+".pdf", err)
				}
				continue
			}
			// Non-trusted, global newer: show info and ask.
			warning := "PDF differs for %s:\n  global: %s\n  local : %s"
			for {
				answer := l.WarningQuestion(QuestionLocalPDFConflict, options, warning,
					p.canonicalKey, pdfFileInfo(globalPath), pdfFileInfo(localPath))
				if answer == "o" {
					openBothPDFs(globalPath, localPath)
					continue
				}
				if answer == "g" {
					copyFile(globalPath, localPath) //nolint:errcheck
				} else if answer == "l" {
					copyFile(localPath, globalPath) //nolint:errcheck
				}
				break
			}
		} else {
			// Local newer: always ask with open-both offer.
			warning := "Local PDF is newer than global for %s:\n  global: %s\n  local : %s"
			for {
				answer := l.WarningQuestion(QuestionLocalPDFConflict, options, warning,
					p.canonicalKey, pdfFileInfo(globalPath), pdfFileInfo(localPath))
				if answer == "o" {
					openBothPDFs(globalPath, localPath)
					continue
				}
				if answer == "l" {
					copyFile(localPath, globalPath) //nolint:errcheck
				} else if answer == "g" {
					copyFile(globalPath, localPath) //nolint:errcheck
				}
				break
			}
		}
	}
}

// mergePDFFile transfers the PDF for sourceKey to targetKey as part of a merge.
// If source has a PDF and target does not, the file is renamed in place.
// If both have a PDF, the user is asked which to keep (with open-both offer);
// the discarded copy goes to library trash.
// No-op when source has no PDF.
func (l *TBibTeXLibrary) mergePDFFile(sourceKey, targetKey string) {
	if !l.PDFFiles[sourceKey] {
		return
	}
	srcPath := l.FilesRoot + l.FilesFolder + sourceKey + ".pdf"
	dstPath := l.FilesRoot + l.FilesFolder + targetKey + ".pdf"

	if l.PDFFiles[targetKey] || FileExists(dstPath) {
		// Both entries have a PDF — ask which to keep.
		options := TStringSetNew()
		options.Add("t", "s", "o", "k")
		warning := "Merge produced two PDFs:\n  target (%s): %s\n  source (%s): %s"
		for {
			answer := l.WarningQuestion(QuestionMergePDFConflict, options, warning,
				targetKey, pdfFileInfo(dstPath), sourceKey, pdfFileInfo(srcPath))
			switch answer {
			case "o":
				openBothPDFs(dstPath, srcPath)
				continue
			case "t":
				// Keep target — move source to trash.
				l.moveToLibraryTrash(srcPath) //nolint:errcheck
				delete(l.PDFFiles, sourceKey)
			case "s":
				// Keep source — replace target with source.
				l.moveToLibraryTrash(dstPath) //nolint:errcheck
				if err := os.Rename(srcPath, dstPath); err != nil {
					l.Warning("Could not rename PDF %s.pdf → %s.pdf: %s", sourceKey, targetKey, err)
				} else {
					delete(l.PDFFiles, sourceKey)
					l.PDFFiles[targetKey] = true
				}
			case "k":
				// Skip — leave both as-is for now.
			}
			break
		}
		return
	}

	if err := os.Rename(srcPath, dstPath); err != nil {
		l.Warning("Could not rename PDF %s.pdf → %s.pdf: %s", sourceKey, targetKey, err)
		return
	}
	delete(l.PDFFiles, sourceKey)
	l.PDFFiles[targetKey] = true
	l.Progress("Renamed PDF: %s.pdf → %s.pdf", sourceKey, targetKey)
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
	l.Progress("  Checking for orphaned PDFs")

	logPath := bibTeXFolder + bibTeXBaseName + tablesFolderSuffix + "/orphaned_pdfs.log"
	os.MkdirAll(bibTeXFolder+bibTeXBaseName+tablesFolderSuffix, 0o755) //nolint:errcheck
	var orphanLog *os.File
	if f, err2 := os.Create(logPath); err2 == nil {
		orphanLog = f
		fmt.Fprintf(orphanLog, "Orphaned PDF scan on %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
		defer orphanLog.Close()
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
					l.Progress("  Renamed PDF for merged entry: %s → %s.pdf", e.Name(), canonical)
					renamed++
				}
			}
			continue
		}

		// No entry, no alias — genuine orphan. Log to file rather than interrupting output.
		if orphanLog != nil {
			fmt.Fprintf(orphanLog, "Orphaned PDF (no library entry): %s — moving to %s\n", e.Name(), trashName)
		}
		if !l.moveToLibraryTrash(fullPath) {
			l.Warning("Could not move %s to %s", e.Name(), trashName)
		} else {
			moved++
		}
	}
	if moved > 0 {
		l.Progress("  Orphaned PDFs moved to trash: %d (see orphaned_pdfs.log)", moved)
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

// isParentChildPair reports whether key1 and key2 form a crossref parent-child pair.
// Returns (child, parent, true) when one entry's crossref field resolves to the other.
func (l *TBibTeXLibrary) isParentChildPair(key1, key2 string) (child, parent string, ok bool) {
	if cr := l.EntryFieldValueity(key1, "crossref"); cr != "" && l.MapEntryKey(cr) == key2 {
		return key1, key2, true
	}
	if cr := l.EntryFieldValueity(key2, "crossref"); cr != "" && l.MapEntryKey(cr) == key1 {
		return key2, key1, true
	}
	return "", "", false
}

// checkParentChildSharedPDF handles the case where a child entry's PDF has the same
// content (MD5) as its crossref parent's PDF. The child's PDF is moved to trash.
// If the child has a URL that is not on the ignore list, a fresh copy is downloaded:
//   - If it still matches the parent's MD5: the fresh download is discarded, the child's
//     URL is challenged against the parent's URL (Y/N/y/n to record the equivalence),
//     and the child's URL field is cleared.
//   - If it differs from the parent's MD5: it is installed as the child's own PDF.
func (l *TBibTeXLibrary) checkParentChildSharedPDF(childKey, parentKey string) {
	filesDir := l.FilesRoot + l.FilesFolder
	childPath := filesDir + childKey + ".pdf"
	parentPath := filesDir + parentKey + ".pdf"
	parentMD5 := MD5ForFile(parentPath)

	if !trashPDF(childPath) {
		l.Warning("Could not trash shared child PDF for %s.", childKey)
		return
	}
	l.Progress("Trashed shared child PDF for %s (same content as parent %s).", childKey, parentKey)

	childURL := l.EntryFieldValueity(childKey, "url")
	if childURL == "" || l.URLsIgnore.Contains(childURL) {
		return
	}

	tmpPath := filesDir + childKey + ".tmp"
	l.Progress("Re-downloading PDF for %s.", childKey)
	if err := downloadPDF(childURL, tmpPath); err != nil {
		l.Warning("Re-download failed for %s (%s): %s", childKey, childURL, err)
		os.Remove(tmpPath)
		return
	}

	if MD5ForFile(tmpPath) == parentMD5 {
		os.Remove(tmpPath)
		parentURL := l.EntryFieldValueity(parentKey, "url")
		if parentURL != "" && parentURL != childURL {
			l.ResolveFieldValue(childKey, parentKey, "url", parentURL, childURL)
		}
		l.SetEntryFieldValue(childKey, "url", "")
	} else {
		if err := os.Rename(tmpPath, childPath); err != nil {
			l.Warning("Could not install fresh PDF for %s: %s", childKey, err)
			os.Remove(tmpPath)
			return
		}
		l.Progress("Child %s now has a distinct PDF from parent %s.", childKey, parentKey)
	}
}

// CheckPDFHealth walks the library files folder and performs all PDF checks in one pass:
// orphaned files (no library entry), duplicate-content detection (via MD5), and per-file
// health checks (magic bytes, content).
//
// For each .pdf file:
//   - No matching library entry → warned as orphaned.
//   - Duplicate content (same MD5 as another file):
//       - Parent-child crossref pair → child PDF is trashed; if child has a URL a fresh
//         copy is downloaded and compared; if still identical the URL is challenged and
//         cleared; if different the fresh copy becomes the child's own PDF.
//       - Other duplicates → interactive w(aive)/m(erge)/s(kip) prompt.
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
	md5Index := TStringSetMap{}
	ticker := l.NewProgressTicker(fmt.Sprintf(ProgressCheckingPDFHealth, filesDir), total)

	for _, fileName := range pdfFiles {
		if ticker.Step() {
			break
		}
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
			ticker.Done()
			return
		}
	}
	ticker.Done()

	validAnswers := TStringSetNew()
	validAnswers.Add("w", "m", "s")

	for md5hash, keys := range md5Index {
		if keys.Size() <= 1 {
			continue
		}

		// Handle parent-child pairs first: child has the same PDF as its crossref parent.
		sortedKeys := keys.ElementsSorted()
		handledChildren := TStringSetNew()
		for i, k1 := range sortedKeys {
			for _, k2 := range sortedKeys[i+1:] {
				if child, parent, ok := l.isParentChildPair(k1, k2); ok && !handledChildren.Contains(child) {
					l.checkParentChildSharedPDF(child, parent)
					handledChildren.Add(child)
				}
			}
		}

		// Build remaining set — entries not consumed as a child above.
		remaining := TStringSetNew()
		for _, k := range sortedKeys {
			if !handledChildren.Contains(k) {
				remaining.Add(k)
			}
		}
		if remaining.Size() <= 1 {
			continue
		}

		// Skip when every remaining entry has already waived this exact PDF.
		allWaived := true
		for key := range remaining.Elements() {
			if l.GetMetadata(key, MetaPropWaivedDoublePdf) != md5hash {
				allWaived = false
				break
			}
		}
		if allWaived {
			continue
		}

		l.Warning(WarningDuplicateFileContent, remaining.String())
		switch l.WarningQuestion(QuestionDoublePdfWaive, validAnswers, "") {
		case "w":
			for key := range remaining.Elements() {
				l.SetMetadata(key, MetaPropWaivedDoublePdf, md5hash)
			}
		case "m":
			l.MaybeMergeEntrySet(remaining)
		}
	}
}
