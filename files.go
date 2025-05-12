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
	"crypto/md5"
	"fmt"
	"github.com/udhos/equalfile"
	"io"
	"os"
	"time"
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

// Create a backup of an existing file.
func BackupFile(sourceFile string) bool {
	// Open the source file
	source, err := os.Open(sourceFile)
	if err != nil {
		return false
	}
	defer source.Close()

	// Create the backup file
	destination, err := os.Create(sourceFile + "." + timestamp())
	if err != nil {
		return false
	}
	defer destination.Close()

	// Copy the contents of the source file to the backup file
	if _, err = io.Copy(destination, source); err != nil {
		return false
	}

	return true
}

func ModTime(file string) int64 {
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
