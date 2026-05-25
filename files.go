/*
 *
 * Module: files
 *
 * This module provides basic operations to manage files.
 * It, in particular, provides functionality to backup existing files.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 23.04.2024
 *
 */

package main

import (
	"bufio"
	"crypto/md5"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/udhos/equalfile"
)

func MD5ForFile(file string) string {
	source, err := os.Open(file)

	if err != nil {
		return "1"
	}

	defer source.Close()

	hash := md5.New()
	_, err = io.Copy(hash, source)

	if err != nil {
		return "2"
	}

	return fmt.Sprintf("%x", hash.Sum(nil))
}

// Consistency of names .. FileXXX or XXXFile

func EqualFiles(file1, file2 string) bool {
	cmp := equalfile.New(nil, equalfile.Options{}) // compare using single mode
	equal, _ := cmp.CompareFile(file1, file2)

	return equal
}

// Get a unique timestamp to be used as part of the filename of backups.
func timestamp() string {
	now := time.Now()
	result := fmt.Sprintf(
		"%04d-%02d-%02d-%02d-%02d-%02d.%05d",
		now.Year(),
		int(now.Month()),
		now.Day(),
		now.Hour(),
		now.Minute(),
		now.Second(),
		now.Nanosecond()/10000)

	return result
}

var libraryBackupCreated bool

// ensureLibraryBackup creates a timestamped backup directory containing the bib
// file and all CSV tables, at most once per run. It is called before the first
// write so the snapshot reflects the last known-good state.
func ensureLibraryBackup() {
	if libraryBackupCreated || bibTeXBaseName == "" || backupFolder == "" {
		return
	}
	libraryBackupCreated = true

	ts := time.Now().Format("2006-01-02-15-04-05")
	backupDir := filepath.Join(backupFolder, bibTeXBaseName+"-"+ts)

	if err := os.MkdirAll(backupDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: could not create library backup directory %s: %s\n", backupDir, err)
		return
	}

	bibSrc := bibTeXFolder + bibTeXBaseName + BibFileExtension
	_ = copyFile(bibSrc, filepath.Join(backupDir, bibTeXBaseName+BibFileExtension))

	tablesDir := bibTeXFolder + bibTeXBaseName + ".tables"
	backupTablesDir := filepath.Join(backupDir, bibTeXBaseName+".tables")
	if err := os.MkdirAll(backupTablesDir, 0755); err == nil {
		if des, err := os.ReadDir(tablesDir); err == nil {
			for _, de := range des {
				if !de.IsDir() && strings.HasSuffix(de.Name(), ".csv") {
					_ = copyFile(filepath.Join(tablesDir, de.Name()), filepath.Join(backupTablesDir, de.Name()))
				}
			}
		}
	}

	Reporting.Progress(ProgressCreatingLibraryBackup, backupDir)
}

// BackupFile copies sourceFile into backupFolder, appending a timestamp suffix.
func BackupFile(sourceFile string) bool {
	source, err := os.Open(sourceFile)
	if err != nil {
		return false
	}
	defer source.Close()

	if err := os.MkdirAll(backupFolder, 0755); err != nil {
		return false
	}
	destPath := filepath.Join(backupFolder, filepath.Base(sourceFile)+"."+timestamp())
	destination, err := os.Create(destPath)
	if err != nil {
		return false
	}
	defer destination.Close()

	if _, err = io.Copy(destination, source); err != nil {
		return false
	}
	return true
}

func fileModTime(file string) int64 {
	fileInfo, err := os.Stat(file)

	if os.IsNotExist(err) {
		return 0
	} else {
		return fileInfo.ModTime().UnixMicro()
	}
}

// Check if the given file exists or not.
func FileExists(fileName string) bool {
	if fileName == "" {
		return false
	}

	info, err := os.Stat(fileName)
	if os.IsNotExist(err) {
		return false
	}

	return !info.IsDir()
}

func FileRename(oldName, newName string) {
	os.Rename(oldName, newName)
}

func FileDelete(file string) {
	os.Remove(file)
}

func processFile(fullFilePath string, process func(string)) bool {
	file, err := os.Open(fullFilePath)
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		process(string(scanner.Text()))
	}

	return scanner.Err() == nil
}

// processCSVFile reads a CSV file using encoding/csv (handles quoted fields containing
// the delimiter) and calls process for each record. The delimiter is taken from csvDelimiter.
func processCSVFile(fullFilePath string, process func([]string)) bool {
	file, err := os.Open(fullFilePath)
	if err != nil {
		return false
	}
	defer file.Close()

	r := csv.NewReader(file)
	r.Comma = rune(csvDelimiter[0])
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		process(record)
	}
	return true
}

// csvLine formats fields as a single CSV line using encoding/csv, quoting any field
// that contains the delimiter. The trailing newline is stripped so callers can
// append "\n" themselves (consistent with the rest of the write infrastructure).
func csvLine(fields ...string) string {
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	w.Comma = rune(csvDelimiter[0])
	_ = w.Write(fields)
	w.Flush()
	return strings.TrimRight(buf.String(), "\n")
}
