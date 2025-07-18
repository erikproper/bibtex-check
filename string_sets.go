/*
 *
 * Module: string_sets
 *
 * This module provides basic operations to manage sets of strings.
 * In the future this module may become (part of) a sets & sequences package.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 22.04.2024
 *
 */

package main

import (
	"maps"
	"sort"
)

// Functions that create/add/remove/unite/etc sets, return a pointer to the given set to enable concatenation of operators.
// For instance s.Initialise().Add("Hello").Add("World").Delete("Hello")

// String sets are essentially defined as a mapping to an empty struct.
// However, we also want to select the way it is represented as a string.
// Therefore, we use a struct to represent this in a combined way.
type (
	TStringSetElements       map[string]struct{}
	TStringSetSortedElements sort.StringSlice
	TStringSet               struct {
		elements  TStringSetElements // The elements in the set
		verbalise bool               // Setting to determine the style used in converting sets to strings:
		//                           // - Verbalised:   "june", "juli", and "august"
		//                           // - Mathematical: { "june", "juli", "august" }
	}
)

// Create a new string set.
func TStringSetNew() TStringSet {
	fresh := TStringSet{}
	fresh.Initialise()

	return fresh
}

// Return the pointer to the provided string set.
// Useful when using maps of string sets.
func (s TStringSet) Set() *TStringSet {
	return &s
}

// (Re)initialise string sets.
func (s *TStringSet) Initialise() *TStringSet {
	s.elements = TStringSetElements{}
	s.verbalise = false

	return s
}

// Set the string representation of string sets to verbalisation mode.
func (s *TStringSet) Verbalised() *TStringSet {
	s.verbalise = true

	return s
}

// Set the string representation of string sets to mathematical mode.
func (s *TStringSet) Mathematical() *TStringSet {
	s.verbalise = false

	return s
}

// The size of a set.
// Note: As a set does not have an order, it would not make sense to speak of its "Length"
func (s *TStringSet) Size() int {
	return len(s.elements)
}

// Returns a map with the elements contained in the set.
func (s *TStringSet) Elements() TStringSetElements {
	return s.elements
}

// Returns a slice with the elements contained in the set sorted based on their string value
func (s *TStringSet) ElementsSorted() TStringSetSortedElements {
	elements := make([]string, 0, s.Size())

	for e := range s.elements {
		elements = append(elements, e)
	}
	sort.Strings(elements)

	return elements
}

// Add elements.
func (s *TStringSet) Add(elements ...string) *TStringSet {
	for _, element := range elements {
		s.elements[element] = struct{}{}
	}

	return s
}

// Remove elements.
func (s *TStringSet) Delete(elements ...string) *TStringSet {
	for _, element := range elements {
		delete(s.elements, element)
	}

	return s
}

// Combine with another set.
func (s *TStringSet) Unite(t TStringSet) *TStringSet {
	maps.Copy(s.elements, (&t).elements)

	return s
}

// Intersect with another set.
func (s *TStringSet) Intersect(t TStringSet) *TStringSet {
	for element := range s.elements {
		if _, isIn := (&t).elements[element]; !isIn {
			delete(s.elements, element)
		}
	}

	return s
}

// Subtract another set.
func (s *TStringSet) Subtract(t TStringSet) *TStringSet {
	for element := range s.elements {
		if _, isIn := (&t).elements[element]; isIn {
			delete(s.elements, element)
		}
	}

	return s
}

// Check if the set is equal to another set.
func (s *TStringSet) IsEq(t *TStringSet) bool {
	return maps.Equal(s.elements, t.elements)
}

// Check if the set is a subset, or equal, to another set
func (s *TStringSet) IsSubsetEq(t *TStringSet) bool {
	// Makes uses of the maps.Equal function, where we know:
	//    t UNION s = t ==> s SUBSETEQ t
	// which could also be written as:
	//    (u = t UNION s) AND u = t ==> s SUBSETEQ t

	u := TStringSetElements{}
	maps.Copy(u, t.elements)
	maps.Copy(u, s.elements)

	return maps.Equal(u, t.elements)
}

// Check if the set is a subset to another set
func (s *TStringSet) IsSubset(t *TStringSet) bool {

	return s.IsSubsetEq(t) && !s.IsEq(t)
}

// Check if the set is a superset, or equal, to another set
func (s *TStringSet) IsSupersetEq(t *TStringSet) bool {

	return t.IsSubsetEq(s)
}

// Check if the set is a superset to another set
func (s *TStringSet) IsSuperset(t *TStringSet) bool {

	return t.IsSubset(s)
}

// Check if the provided element(s) are in the set of strings
func (s *TStringSet) Contains(elements ...string) bool {
	for _, element := range elements {
		// As soon as we find one element which is not in the set of strings, we can safely stop and return false
		if _, isIn := s.elements[element]; !isIn {
			return false
		}
	}

	return true
}

//// RETHINK STring and Stringify
//// Could include a function to prepare the values, and then the ", and ", and" or "and" options
//// So, set a "format" function as parameter in the TStringSet
//// Same with overall wrapping with eg "{ ... }"
//// NOTE: Same for ordering it I guess ...

func (s TStringSet) Stringify() string {
	head := ""
	tail := ""

	for _, element := range s.ElementsSorted() {
		if head == "" {
			head = tail
		} else {
			head += ", " + tail
		}
		tail = element
	}

	if s.verbalise {
		if head == "" {
			return tail
		} else {
			return head + " and " + tail
		}
	} else {
		if head == "" {
			return tail
		} else {
			return head + ", " + tail
		}
	}
}

// Convert strings sets to a string.
// Depending on the settings regarding Verbalised/Mathematical, different styles of strings will be created:
// - Verbalised:   "june", juli", and "august"
// - Mathematical: { "june", juli", "august" }
func (s TStringSet) String() string {
	head := ""
	tail := ""

	for _, element := range s.ElementsSorted() {
		if head == "" {
			head = tail
		} else {
			head += ", " + tail
		}
		tail = "\"" + element + "\""
	}

	if s.verbalise {
		if head == "" {
			return tail
		} else {
			return head + " and " + tail
		}
	} else {
		if head == "" {
			return "{ " + tail + " }"
		} else {
			return "{ " + head + ", " + tail + " }"
		}
	}
}
