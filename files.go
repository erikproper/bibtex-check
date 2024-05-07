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
	"fmt"
	"io"
	"os"
	"time"
)

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

// Check if the given file exists or not.
func FileExists(fileName string) bool {
	_, err := os.Stat(fileName)
	return err == nil
}
