package main

import "fmt"

type TReporting struct{}

func (r *TReporting) Error(errorMessage string, context ...any) bool {
	fmt.Printf("ERROR: "+errorMessage+".\n", context...)

	return true
}

func (r *TReporting) Warning(warning string, context ...any) bool {
	fmt.Printf("WARNING: "+warning+".\n", context...)

	return true
}
