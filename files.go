package main

import (
	"fmt"
	"io"
	"os"
	"time"
)

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

func BackupFile(sourceFile string) bool {
	destinationFile := sourceFile + "." + timestamp()

	source, err := os.Open(sourceFile) //open the source file
	if err != nil {
		return false
	}
	defer source.Close()

	destination, err := os.Create(destinationFile) //create the destination file
	if err != nil {
		return false
	}
	defer destination.Close()

	_, err = io.Copy(destination, source) //copy the contents of source to destination file
	if err != nil {
		return false
	}

	return true
}
