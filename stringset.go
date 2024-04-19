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

func (s *TStringSet) String() string {
	head := ""
	tail := ""

	for element, isIn := range *s {
		if isIn {
			if head != "" {
				head += ", " + tail
				tail = element
			} else {
				head = tail
				tail = element
			}
		}
	}

	if head != "" {
		return head + " and " + tail
	} else {
		return tail
	}
}
