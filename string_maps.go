//
// Module: string_maps
//
// This module is concerned XXXXXX
//
// Creator: Henderik A. Proper (erikproper@gmail.com)
//
// Version of: 03.05.2024
//

package main

type (
	TStringMap             map[string]string
	TStringStringMap       map[string]TStringMap
	TStringStringStringMap map[string]TStringStringMap
	TStringSetMap          map[string]TStringSet
)

func (m *TStringMap) ForEachStringPair(f func(string, string)) {
	for i, v := range *m {
		f(i, v)
	}
}

func (m *TStringStringMap) ForEachStringTripple(f func(string, string, string)) {
	for i, v := range *m {
		v.ForEachStringPair(func(a, b string) { f(i, a, b) })
	}
}

func (m *TStringStringStringMap) ForEachStringQuadrupple(f func(string, string, string, string)) {
	for i, v := range *m {
		v.ForEachStringTripple(func(a, b, c string) { f(i, a, b, c) })
	}
}

func (m *TStringSetMap) ForEachStringSetPair(f func(string, TStringSet)) {
	for i, v := range *m {
		f(i, v)
	}
}

func (m *TStringMap) StringMapped(i string) bool {
	if (*m) == nil {
		return false
	} else {
		_, exists := (*m)[i]
		return exists
	}
}

func (m *TStringStringMap) StringStringMapped(i, j string) bool {
	if (*m) == nil {
		return false
	} else if (*m)[i] == nil {
		return false
	} else {
		_, exists := (*m)[i][j]
		return exists
	}
}

func (m *TStringStringStringMap) StringStringStringMapped(i, j, k string) bool {
	if (*m) == nil {
		return false
	} else if (*m)[i] == nil {
		return false
	} else if (*m)[i][j] == nil {
		return false
	} else {
		_, exists := (*m)[i][j][k]
		return exists
	}
}

func (m *TStringMap) StringMapGetValue(i string) string {
	if (*m) == nil {
		return ""
	} else {
		return (*m)[i]
	}
}

func (m *TStringStringMap) StringStringMapGetValue(i, j string) string {
	if (*m) == nil {
		return ""
	} else if (*m)[i] == nil {
		return ""
	} else {
		return (*m)[i][j]
	}
}

func (m *TStringStringStringMap) StringStringStringMapGetValue(i, j, k string) string {
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

//

func (m *TStringMap) StringMapSetValue(i, v string) {
	if (*m) == nil {
		(*m) = TStringMap{}
	}

	(*m)[i] = v
}

func (m *TStringStringMap) StringStringMapSetValue(i, j, v string) {
	if (*m) == nil {
		(*m) = TStringStringMap{}
	}

	if (*m)[i] == nil {
		(*m)[i] = TStringMap{}
	}

	(*m)[i][j] = v
}

func (m *TStringStringStringMap) StringStringStringMapSetValue(i, j, k, v string) {
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
