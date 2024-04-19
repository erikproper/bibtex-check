package main

type TStringSet map[string]bool

func (s *TStringSet) Contains(elements ...string) bool {
	for _, element := range elements {
		_, isIn := (*s)[element]

		if !isIn {
			return false
		}
	}

	return true
}

func (s *TStringSet) Add(elements ...string) *TStringSet {
	for _, element := range elements {
		(*s)[element] = true
	}

	return s
}
