/*
 *
 * Module: string_maps
 *
 * This module is concerned with different operators on string related maps
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 03.05.2024
 *
 */

package main

type (
	TStringMap             map[string]string
	TStringStringMap       map[string]TStringMap
	TStringStringStringMap map[string]TStringStringMap
	TStringSetMap          map[string]TStringSet
)

/*
 *
 * Processing functions
 *
 */

// For each string pair apply the provided function.
func (m *TStringMap) ForEachStringMapping(f func(string, string)) {
	for i, v := range *m {
		f(i, v)
	}
}

// For each string tripple apply the provided function.
func (m *TStringStringMap) ForEachStringPairMapping(f func(string, string, string)) {
	for i, v := range *m {
		v.ForEachStringMapping(func(a, b string) { f(i, a, b) })
	}
}

// For each string quadruple apply the provided function.
func (m *TStringStringStringMap) ForEachStringTrippleMapping(f func(string, string, string, string)) {
	for i, v := range *m {
		v.ForEachStringPairMapping(func(a, b, c string) { f(i, a, b, c) })
	}
}

// For each pair of string and set of strings, apply the provided function.
func (m *TStringSetMap) ForEachStringSetMapping(f func(string, TStringSet)) {
	for i, v := range *m {
		f(i, v)
	}
}

/*
 *
 * Checking functions
 *
 */

// Check if a string is "mapped" in the provided string map
func (m *TStringMap) IsMappedString(i string) bool {
	if (*m) == nil {
		return false
	} else {
		return len((*m)[i]) > 0
	}
}

// Check if a string is "mapped" in the provided string map
func (m *TStringStringMap) IsMappedString(i string) bool {
	if (*m) == nil {
		return false
	} else {
		return len((*m)[i]) > 0
	}
}

// Check if a string is "mapped" in the provided string map
func (m *TStringStringStringMap) IsMappedString(i string) bool {
	if (*m) == nil {
		return false
	} else {
		return len((*m)[i]) > 0
	}
}

// Check if a string pair is "mapped" in the provided string map
func (m *TStringStringMap) IsMappedStringPair(i, j string) bool {
	if (*m) == nil {
		return false
	} else if (*m)[i] == nil {
		return false
	} else {
		return len((*m)[i][j]) > 0
	}
}

// Check if a string pair is "mapped" in the provided string map
func (m *TStringStringStringMap) IsMappedStringPair(i, j string) bool {
	if (*m) == nil {
		return false
	} else if (*m)[i] == nil {
		return false
	} else {
		return len((*m)[i][j]) > 0
	}
}

// Check if a string tripple is "mapped" in the provided string map
func (m *TStringStringStringMap) IsMappedStringTripple(i, j, k string) bool {
	if (*m) == nil {
		return false
	} else if (*m)[i] == nil {
		return false
	} else if (*m)[i][j] == nil {
		return false
	} else {
		return len((*m)[i][j][k]) > 0
	}
}

/*
 *
 * Get functions
 *
 */

// Get the mapped value from the given string, from the given map.
// Returns the empty string if no value is mapped.
func (m *TStringMap) GetValueFromStringMap(i string) string {
	if (*m) == nil {
		return ""
	} else {
		return (*m)[i]
	}
}

// Get the mapped value from the given string pair, from the given map.
// Returns the empty string if no value is mapped.
func (m *TStringStringMap) GetValueFromStringPairMap(i, j string) string {
	if (*m) == nil {
		return ""
	} else if (*m)[i] == nil {
		return ""
	} else {
		return (*m)[i][j]
	}
}

// Get the mapped value from the given string triple, from the given map.
// Returns the empty string if no value is mapped.
func (m *TStringStringStringMap) GetValueFromStringTrippleMap(i, j, k string) string {
	if (*m) == nil {
		return ""
	} else if (*m)[i] == nil {
		return ""
	} else if (*m)[i][j] == nil {
		return ""
	} else {
		return (*m)[i][j][k]
	}
}

/*
 *
 * Set functions
 *
 */

// Set the mapped value for a string map
func (m *TStringMap) SetValueForStringMap(i, v string) {
	if (*m) == nil {
		(*m) = TStringMap{}
	}

	(*m)[i] = v
}

// Set the mapped value for a string pair map
func (m *TStringStringMap) SetValueForStringPairMap(i, j, v string) {
	if (*m) == nil {
		(*m) = TStringStringMap{}
	}

	if (*m)[i] == nil {
		(*m)[i] = TStringMap{}
	}

	(*m)[i][j] = v
}

// Set the mapped value for a string triple map
func (m *TStringStringStringMap) SetValueForStringTripleMap(i, j, k, v string) {
	if (*m) == nil {
		(*m) = TStringStringStringMap{}
	}

	if (*m)[i] == nil {
		(*m)[i] = TStringStringMap{}
	}

	if (*m)[i][j] == nil {
		(*m)[i][j] = TStringMap{}
	}

	(*m)[i][j][k] = v
}
