package main

import "fmt"
import "testing"

func TestStringMaps(t *testing.T) {

//	fmt.Println(normalisePagesValue(&Library, "1:1--1:8, 3:2, 4-10"))
//	fmt.Println(normalisePagesValue(&Library, "1:1--2:8"))
//	fmt.Println(normalisePagesValue(&Library, "1:1---2:8"))

Library.UpdateChallengeWinner("hello", "daar", "creasht", "dit")
fmt.Println(Library.ChallengeWinners)
	//	strings.TrimSpace
	// Play
	// TITLES
	// Macro calls always protected.
	// { => nest
	// \ => in macro name to next space
	// \{, \&, => no protection needed
	// \', \^, etc ==> no space to next char needed
	// \x Y ==> keep space
	// " -- " ==> Sub title mode
	// ": " ==> Sub title mode
	// [nonspace]+[A-Z]+[nonspace]* => protect
	//
	//		Titles("{Hello {{World}}   HOW {aRe} Things}")
	//		Titles("{ Hello {{World}} HOW   a{R}e Things}")
	//		Titles("{Hello {{World}} HOW a{R}e Things")
	//		Titles("Hello { { Wo   rld}} HOW a{R}e Things")
	// Braces can prevent kerning between letters, so it is in general preferable to enclose entire words and not just single letters in braces to protect them.
}
