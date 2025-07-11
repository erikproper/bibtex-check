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
	TStringMap                map[string]string
	TStringStringMap          map[string]TStringMap
	TStringStringStringMap    map[string]TStringStringMap
	TStringSetMap             map[string]TStringSet
	TStringStringSetMap       map[string]TStringSetMap
	TStringStringStringSetMap map[string]TStringStringSetMap
)

/*
 *
 * Checking functions
 *
 */

// Safely check if a string is "mapped" in the provided string map
func (m *TStringMap) IsMappedString(i string) bool {
	if (*m) == nil {
		return false
	} else {
		return len((*m)[i]) > 0
	}
}

// Safely check if a string is "mapped" in the provided string map
func (m *TStringStringMap) IsMappedString(i string) bool {
	if (*m) == nil {
		return false
	} else {
		return len((*m)[i]) > 0
	}
}

// Safely check if a string is "mapped" in the provided string map
func (m *TStringStringStringMap) IsMappedString(i string) bool {
	if (*m) == nil {
		return false
	} else {
		return len((*m)[i]) > 0
	}
}

// Safely check if a string pair is "mapped" in the provided string map
func (m *TStringStringMap) IsMappedStringPair(i, j string) bool {
	if (*m) == nil {
		return false
	} else if (*m)[i] == nil {
		return false
	} else {
		return len((*m)[i][j]) > 0
	}
}

// Safely check if a string pair is "mapped" in the provided string map
func (m *TStringStringStringMap) IsMappedStringPair(i, j string) bool {
	if (*m) == nil {
		return false
	} else if (*m)[i] == nil {
		return false
	} else {
		return len((*m)[i][j]) > 0
	}
}

// Safely check if a string tripple is "mapped" in the provided string map
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
 * Mappings
 *
 */
func (m *TStringMap) GetDomainFromStringMap() TStringSet {
	domain := TStringSetNew()

	for element := range *m {
		domain.Add(element)
	}

	return domain
}

/*
 *
 * Get value(s) functions
 *
 */

// The functions below use the term "Valueity", since the can return an actual value, or an empty value.
// Safely get the mapped value from the given string, from the given map.
// Returns the empty string if no value is mapped.
func (m *TStringMap) GetValueityFromStringMap(i string) string {
	if (*m) == nil {
		return ""
	} else {
		return (*m)[i]
	}
}

// Safely get the mapped value from the given string pair, from the given map.
// Returns the empty string if no value is mapped.
func (m *TStringStringMap) GetValueityFromStringPairMap(i, j string) string {
	if (*m) == nil {
		return ""
	} else if (*m)[i] == nil {
		return ""
	} else {
		return (*m)[i][j]
	}
}

// Safely get the mapped value from the given string triple, from the given map.
// Returns the empty string if no value is mapped.
func (m *TStringStringStringMap) GetValueityFromStringTripleMap(i, j, k string) string {
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
 * Set value(s) functions
 *
 */

// Safely set the mapped value for a string map
func (m *TStringMap) SetValueForStringMap(i, v string) {
	if (*m) == nil {
		(*m) = TStringMap{}
	}

	(*m)[i] = v
}

// Safely set the mapped value for a string pair map
func (m *TStringStringMap) SetValueForStringPairMap(i, j, v string) {
	if (*m) == nil {
		(*m) = TStringStringMap{}
	}

	if (*m)[i] == nil {
		(*m)[i] = TStringMap{}
	}

	(*m)[i][j] = v
}

// Safely set the mapped value for a string triple map
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

// Safely add an element to a string set map
func (m *TStringSetMap) AddValueToStringSetMap(i, v string) {
	if (*m) == nil {
		(*m) = TStringSetMap{}
	}

	_, hasSet := (*m)[i]
	if !hasSet {
		(*m)[i] = TStringSetNew()
	}

	(*m)[i].Set().Add(v)
}

func (m *TStringSetMap) GetValueSetFromStringSetMap(i string) *TStringSet {
	if (*m) == nil {
		var s *TStringSet
		s.Initialise()

		return s
	} else {
		return (*m)[i].Set()
	}
}

// Safely remove an element from a string set map
func (m *TStringSetMap) DeleteValueFromStringSetMap(i, v string) {
	if (*m) == nil {
		return
	}

	_, hasSet := (*m)[i]
	if !hasSet {
		return
	}

	(*m)[i].Set().Delete(v)
}

// /// Also the other ones ... makes AddValueToStringSetMap obsolete
func (m *TStringSetMap) StringSet(i string) *TStringSet {
	if (*m) == nil {
		(*m) = TStringSetMap{}
	}

	_, hasSet := (*m)[i]
	if !hasSet {
		(*m)[i] = TStringSetNew()
	}

	return (*m)[i].Set()
}

// Safely add an element to a string set map
func (m *TStringStringSetMap) AddValueToStringPairSetMap(i, j, v string) {
	if (*m) == nil {
		(*m) = TStringStringSetMap{}
	}

	_, hasMap := (*m)[i]
	if !hasMap {
		(*m)[i] = TStringSetMap{}
	}

	_, hasSet := (*m)[i][j]
	if !hasSet {
		(*m)[i][j] = TStringSetNew()
	}

	(*m)[i][j].Set().Add(v)
}

// Safely add an element to a string set map
func (m *TStringStringStringSetMap) AddValueToStringTrippleSetMap(i, j, k, v string) {
	if (*m) == nil {
		(*m) = TStringStringStringSetMap{}
	}

	_, hasMap := (*m)[i]
	if !hasMap {
		(*m)[i] = TStringStringSetMap{}
	}

	_, hasMap = (*m)[i][j]
	if !hasMap {
		(*m)[i][j] = TStringSetMap{}
	}

	_, hasSet := (*m)[i][j][k]
	if !hasSet {
		(*m)[i][j][k] = TStringSetNew()
	}

	(*m)[i][j][k].Set().Add(v)
}
