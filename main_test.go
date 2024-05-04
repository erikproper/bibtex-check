package main

import "fmt"
import "testing"

var Tester1 TStringMap
var Tester2 TStringStringMap
var Tester3 TStringStringStringMap

func TestStringMaps(t *testing.T) {
	Tester1.StringMapSetValue("hello", "world")
	fmt.Println(Tester1)
	fmt.Println(Tester1.StringMapGetValue("hello"))
	fmt.Println(Tester1.StringMapGetValue("not"))

	Tester2.StringStringMapSetValue("hello", "world", "erik")
	fmt.Println(Tester2)
	fmt.Println(Tester2.StringStringMapGetValue("hello", "world"))
	fmt.Println(Tester2.StringStringMapGetValue("not", "world"))
	fmt.Println(Tester2.StringStringMapGetValue("hello", "not"))

	Tester3.StringStringStringMapSetValue("hello", "world", "erik", "proper")
	fmt.Println(Tester3)
	fmt.Println(Tester3.StringStringStringMapGetValue("hello", "world", "erik"))
	fmt.Println(Tester3.StringStringStringMapGetValue("not", "world", "erik"))
	fmt.Println(Tester3.StringStringStringMapGetValue("hello", "not", "erik"))
	fmt.Println(Tester3.StringStringStringMapGetValue("hello", "world", "not"))

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
