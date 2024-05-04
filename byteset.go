/*
 *
 * Module: byteset
 *
 * This module provides basic operations to manage sets of bytes / characters based on uint64 vectors.
 * The assumption is that using such vectors is faster than e.g. using maps from byte to empty structs.
 * In the future this module may become (part of) a sets & sequences package.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 22.04.2024
 *
 */

package main

import "fmt"

type (
	TByteSetElements map[byte]struct{}

	TByteSet struct {
		words       [4]uint64 // Representation of the (encoding) of the elements.
		treatAsChar bool      // Set to true if the elements should be treated as characters when converting the set to a string.
		verbalise   bool      // Setting to determine the style used in converting sets to strings:
		//                    // - Verbalised:   'a', 'b', and 'c'
		//                    // - Mathematical: { 'a', 'b', 'c' }
	}
)

// String representation of (special!) characters; see init function below.
var ByteToString [256]string

// Functions that create/add/remove/unite/etc sets, return a pointer to the given set to enable concatenation of operators.
// For instance s.Initialise().Add(1).Add(2).Delete(1)

// Create a new byte set.
func TByteSetNew() TByteSet {
	fresh := TByteSet{}
	fresh.Initialise()

	return fresh
}

// Return the pointer to the provided byte set.
// Useful when using maps of byte sets.
func (s TByteSet) Set() *TByteSet {
	return &s
}

// (Re)initialise byte sets.
func (s *TByteSet) Initialise() *TByteSet {
	s.words[0] = uint64(0)
	s.words[1] = uint64(0)
	s.words[2] = uint64(0)
	s.words[3] = uint64(0)
	s.verbalise = false
	s.treatAsChar = false

	return s
}

// Set the string representation of byte sets to verbalisation mode.
func (s *TByteSet) Verbalised() *TByteSet {
	s.verbalise = true

	return s
}

// Set the string representation of byte sets to mathematical mode.
func (s *TByteSet) Mathematical() *TByteSet {
	s.verbalise = false

	return s
}

// Set the string representation of byte sets to treat elements as characters.
func (s *TByteSet) TreatAsCharacters() *TByteSet {
	s.treatAsChar = true

	return s
}

// Set the string representation of byte sets to treat elements as bytes.
func (s *TByteSet) TreatAsBytes() *TByteSet {
	s.treatAsChar = false

	return s
}

// The size of a set.
// Note: As a set does not have an order, it would not make sense to speak of its "Length"
func (s *TByteSet) Size() int {
	return len(s.Elements())
}

// Returns a map with the elements contained in the set.
func (s *TByteSet) Elements() TByteSetElements {
	elements := TByteSetElements{}
	for w := 0; w < 4; w++ {
		for b := 0; b < 64; b++ {
			if (s.words[w] & (1 << b)) > 0 {
				elements[byte(w*64+b)] = struct{}{}
			}
		}
	}

	return elements
}

// Returns a string set with strings representing the elements contained in the set.
func (s *TByteSet) Strings() TStringSet {
	t := TStringSetNew()
	for e := range s.Elements() {
		t.Add(ByteToString[e])
	}

	return t
}

// Internal function to encode a byte into the combination of a bit and the right uint64 word
func (s *TByteSet) split(b byte) (byte, uint64) {
	return b / 64, 1 << byte(b%64)
}

// Internal function to add elements from a byte slice to the byte set
func (s *TByteSet) add(elements []byte) *TByteSet {
	for _, element := range elements {
		word, bit := s.split(element)

		// This is where we actually add the element by OR-ing it.
		s.words[word] |= bit
	}

	return s
}

// Add elements to a byte set.
func (s *TByteSet) Add(elements ...byte) *TByteSet {
	return s.add(elements)
}

// Add characters of a string as elements to a byte set.
func (s *TByteSet) AddString(elements string) *TByteSet {
	return s.add([]byte(elements)).TreatAsCharacters()
}

// Internal function to delete elements from a byte slice to the byte set
func (s *TByteSet) delete(elements []byte) *TByteSet {
	for _, element := range elements {
		word, bit := s.split(element)

		// This is where we actually delete the element by AND-NOT-ing it.
		s.words[word] &^= bit
	}

	return s
}

// Delete elements from a byte set.
func (s *TByteSet) Delete(elements ...byte) *TByteSet {
	return s.delete(elements)
}

// Delete characters from a string as elements from a byte set.
func (s *TByteSet) DeleteString(elements string) *TByteSet {
	return s.delete([]byte(elements))
}

// Combine with another set.
func (s *TByteSet) Unite(t TByteSet) *TByteSet {
	// Unite the sets by OR-ing things bit-wise
	s.words[0] |= t.words[0]
	s.words[1] |= t.words[1]
	s.words[2] |= t.words[2]
	s.words[3] |= t.words[3]

	return s
}

// Intersect with another set.
func (s *TByteSet) Intersect(t TByteSet) *TByteSet {
	// Intersect the sets by AND-ing things bit-wise
	s.words[0] &= t.words[0]
	s.words[1] &= t.words[1]
	s.words[2] &= t.words[2]
	s.words[3] &= t.words[3]

	return s
}

// Subtract another set.
func (s *TByteSet) Subtract(t TByteSet) *TByteSet {
	// Intersect the sets by AND-NOT-ing things bit-wise
	s.words[0] &^= t.words[0]
	s.words[1] &^= t.words[1]
	s.words[2] &^= t.words[2]
	s.words[3] &^= t.words[3]

	return s
}

// Check if the set is equal to another set.
func (s TByteSet) IsEq(t TByteSet) bool {
	// If all words are equal, then the sets are equal
	return s.words[0] == t.words[0] && s.words[1] == t.words[1] &&
		s.words[2] == t.words[2] && s.words[3] == t.words[3]
}

// Check if the set is a subset, or equal, to another set
func (s TByteSet) IsSubsetEq(t TByteSet) bool {
	// If all AND-NOT nots of t result in "blending out" the bits of set s, then s is a subset of (or equal to) t
	return s.words[0]&^t.words[0] == 0 && (s.words[1]&^t.words[1] == 0) &&
		(s.words[2]&^t.words[2] == 0) && (s.words[3]&^t.words[3] == 0)
}

// Check if the set is a subset to another set
func (s TByteSet) IsSubset(t TByteSet) bool {
	return s.IsSubsetEq(t) && !s.IsEq(t)
}

// Check if the set is a superset, or equal, to another set
func (s TByteSet) IsSupersetEq(t TByteSet) bool {
	return t.IsSubsetEq(s)
}

// Check if the set is a superset to another set
func (s TByteSet) IsSuperset(t TByteSet) bool {
	return s.IsSupersetEq(t) && !s.IsEq(t)
}

// Check if the provided element(s) are in the set of strings
func (s TByteSet) Contains(elements ...byte) bool {
	var elementSet TByteSet

	elementSet.add(elements)

	return elementSet.IsSubsetEq(s)
}

// Convert byte sets to a string.
// Depending on the settings regarding Verbalised/Mathematical, different styles of strings will be created:
// - Verbalised:   'a', 'b', and 'c'
// - Mathematical: { 'a', 'b', 'c' }
// This cannot be a parameter, since String() enables as to write fmt.Println(s) for any TByteset
func (s TByteSet) String() string {
	head := ""
	tail := ""

	for rawElement := range s.Elements() {
		element := ""
		if s.treatAsChar {
			element = "'" + ByteToString[rawElement] + "'"
		} else {
			element = fmt.Sprintf("%d", rawElement)
		}

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
			return "{ " + tail + " }"
		} else {
			return "{ " + head + ", " + tail + " }"
		}
	}
}

func init() {
	for i := 0; i < 256; i++ {
		ByteToString[i] = fmt.Sprint(i)
	}

	ByteToString['\\'] = "\\\\"
	ByteToString['\a'] = "\\a"
	ByteToString['\b'] = "\\b"
	ByteToString['\f'] = "\\f"
	ByteToString['\n'] = "\\n"
	ByteToString['\r'] = "\\r"
	ByteToString['\t'] = "\\t"
	ByteToString['\v'] = "\\v"
}
