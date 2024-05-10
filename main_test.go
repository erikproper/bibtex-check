package main

import "fmt"
import "testing"

func TestStringMaps(t *testing.T) {
	Library = TBibTeXLibrary{}
	InitialiseMainLibrary()
	//	OpenMainBibFile()

	tt := TBibTeXTeX{}
	tt.library = &Library
	fmt.Println(NormaliseNamesString(&Library, "Ssebuggwawo, D. and Hoppenbrouwers, Stijn J. B. A. and Proper, Henderik A."))
	fmt.Println(NormaliseTitleString(&Library, "ConQuer-92"))
	fmt.Println(NormaliseTitleString(&Library, "{ConQuer-92}"))

	fmt.Println(NormaliseTitleString(&Library, "ConQuer-92 -- 24th Revised Report on -- Meta-Data the {Meta-Data} Conceptual Query Language {LISA-D}"))
	fmt.Println(NormaliseTitleString(&Library, "{ConQuer-92} -- {The} Revised Matulevi{\\v c} on the Conceptual Query Language {LISA-D}"))
	fmt.Println(NormaliseTitleString(&Library, "ConQuer-92 -- meta-Data Revised Meta-Data on the C{\\\"o}nce{\\v p}tual Query Language LISA-D"))
	fmt.Println(NormaliseTitleString(&Library, "{Enterprise Architecture at Work -- Modelling, Communication and Analysis}"))
	fmt.Println(NormaliseTitleString(&Library, "{Enterprise Architecture at Work: Modelling, Communication and Analysis}"))
	fmt.Println(NormaliseTitleString(&Library, "{EA {Anamnesis}: An Approach for Decision Making Analysis in Enterprise Architecture}"))
	fmt.Println(NormaliseTitleString(&Library, "EA {Anamnesis}: An Approach for Decision Making Analysis in Enterprise Architecture"))
	//	fmt.Println(NormaliseTitleString(&Library, "{8th {Mediter}RAnean Conference on Information Systems, {{{{{MCIS}}}}} 2014, Verona, Italy, September 3-5, 2014}"))
	//fmt.Println(NormaliseTitleString(&Library, "{EA} {Anamnesis}: {{Towards}} an Approach for Ent{\\\"e}rprise \\Architecture Rationalization"))
	//fmt.Println("----")
	//fmt.Println(NormaliseTitleString(&Library, "{EA} {Anamnesis}: Towards an Approach for Enterprise Architecture Rationalization"))

	// Use NeedsCaseProtection and NeedsTeXProtection
	// For "{", X, "}" patterns:
	// - If X involves unprotected TeX macros, then NeedsTeXProtection
	// - If X involves non-first uppercase characters, then NeedsCaseProtection
	// For " ", X, " " patterns:
	// - If X involves non-first uppercase characters, then NeedsCaseProtection

	//	fmt.Println(normalisePagesValue(&Library, "1:1--1:8, 3:2, 4-10"))
	//	fmt.Println(normalisePagesValue(&Library, "1:1--2:8"))
	//	fmt.Println(normalisePagesValue(&Library, "1:1---2:8"))

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
