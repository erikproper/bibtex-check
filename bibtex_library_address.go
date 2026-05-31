/*
 *
 * Module: bibtex_library_address
 *
 * This module normalises the address field of BibTeX entries.
 * It applies state-name and country-name alias mappings, and appends a
 * canonical country when only a state is present.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 25.05.2026
 *
 */

package main

import (
	"os"
	"path/filepath"
	"strings"
)

// Default CSV content written on first run when a file is absent.
// Format: canonical;alias  (canonical name first, then the alias that maps to it).
// Bare 2-letter abbreviations are included only where they do NOT conflict with
// an ISO 3166-1 alpha-2 country code present in filter_country_names.csv.
// Braced forms ({MA}, {CA}, …) are safe to include for ALL abbreviations because
// NormaliseAddressNames runs before NormaliseLiteralString, so braces are still
// intact at lookup time, and no country alias uses the braced form.
const defaultStateNames = `Alabama;Alabama
Alabama;{AL}
Alaska;Alaska
Alaska;AK
Alaska;{AK}
Arizona;Arizona
Arizona;{AZ}
Arkansas;Arkansas
Arkansas;{AR}
California;California
California;{CA}
California;Calif.
Colorado;Colorado
Colorado;{CO}
Connecticut;Connecticut
Connecticut;CT
Connecticut;{CT}
Delaware;Delaware
Delaware;{DE}
Florida;Florida
Florida;FL
Florida;{FL}
Georgia;Georgia
Georgia;{GA}
Hawaii;Hawaii
Hawaii;HI
Hawaii;{HI}
Idaho;Idaho
Idaho;{ID}
Illinois;Illinois
Illinois;{IL}
Indiana;Indiana
Indiana;{IN}
Iowa;Iowa
Iowa;IA
Iowa;{IA}
Kansas;Kansas
Kansas;KS
Kansas;{KS}
Kentucky;Kentucky
Kentucky;{KY}
Louisiana;Louisiana
Louisiana;{LA}
Maine;Maine
Maine;{ME}
Maryland;Maryland
Maryland;{MD}
Massachusetts;Massachusetts
Massachusetts;{MA}
Massachusetts;Mass.
Michigan;Michigan
Michigan;MI
Michigan;{MI}
Minnesota;Minnesota
Minnesota;{MN}
Mississippi;Mississippi
Mississippi;{MS}
Missouri;Missouri
Missouri;MO
Missouri;{MO}
Montana;Montana
Montana;{MT}
Nebraska;Nebraska
Nebraska;{NE}
Nevada;Nevada
Nevada;NV
Nevada;{NV}
New Hampshire;New Hampshire
New Hampshire;NH
New Hampshire;{NH}
New Jersey;New Jersey
New Jersey;NJ
New Jersey;{NJ}
New Mexico;New Mexico
New Mexico;NM
New Mexico;{NM}
New York;New York
New York;NY
New York;{NY}
North Carolina;North Carolina
North Carolina;NC
North Carolina;{NC}
North Dakota;North Dakota
North Dakota;ND
North Dakota;{ND}
Ohio;Ohio
Ohio;OH
Ohio;{OH}
Oklahoma;Oklahoma
Oklahoma;OK
Oklahoma;{OK}
Oregon;Oregon
Oregon;OR
Oregon;{OR}
Pennsylvania;Pennsylvania
Pennsylvania;{PA}
Rhode Island;Rhode Island
Rhode Island;RI
Rhode Island;{RI}
South Carolina;South Carolina
South Carolina;{SC}
South Dakota;South Dakota
South Dakota;{SD}
Tennessee;Tennessee
Tennessee;{TN}
Texas;Texas
Texas;TX
Texas;{TX}
Utah;Utah
Utah;UT
Utah;{UT}
Vermont;Vermont
Vermont;VT
Vermont;{VT}
Virginia;Virginia
Virginia;{VA}
Washington;Washington
Washington;WA
Washington;{WA}
West Virginia;West Virginia
West Virginia;WV
West Virginia;{WV}
Wisconsin;Wisconsin
Wisconsin;WI
Wisconsin;{WI}
Wyoming;Wyoming
Wyoming;WY
Wyoming;{WY}
Ontario;Ontario
Ontario;ON
Ontario;{ON}
Quebec;Quebec
Quebec;QC
Quebec;{QC}
British Columbia;British Columbia
British Columbia;BC
British Columbia;{BC}
Alberta;Alberta
Alberta;AB
Alberta;{AB}
Manitoba;Manitoba
Manitoba;MB
Manitoba;{MB}
Saskatchewan;Saskatchewan
Saskatchewan;{SK}
Nova Scotia;Nova Scotia
Nova Scotia;NS
Nova Scotia;{NS}
New Brunswick;New Brunswick
New Brunswick;NB
New Brunswick;{NB}
Newfoundland and Labrador;Newfoundland and Labrador
Newfoundland and Labrador;{NL}
Prince Edward Island;Prince Edward Island
Prince Edward Island;PEI
Prince Edward Island;{PEI}
Northwest Territories;Northwest Territories
Northwest Territories;NT
Northwest Territories;{NT}
Yukon;Yukon
Yukon;YT
Yukon;{YT}
Nunavut;Nunavut
Nunavut;NU
Nunavut;{NU}
New South Wales;New South Wales
New South Wales;NSW
New South Wales;{NSW}
New South Wales;{New South Wales}
Victoria;Victoria
Victoria;VIC
Victoria;{VIC}
Victoria;{Victoria}
Queensland;Queensland
Queensland;QLD
Queensland;{QLD}
Queensland;{Queensland}
South Australia;South Australia
South Australia;SA
South Australia;{SA}
South Australia;{South Australia}
Western Australia;Western Australia
Western Australia;{Western Australia}
Tasmania;Tasmania
Tasmania;TAS
Tasmania;{TAS}
Tasmania;{Tasmania}
Australian Capital Territory;Australian Capital Territory
Australian Capital Territory;ACT
Australian Capital Territory;{ACT}
Australian Capital Territory;{Australian Capital Territory}
Northern Territory;Northern Territory
Northern Territory;{Northern Territory}
England;England
Scotland;Scotland
Wales;Wales
Northern Ireland;Northern Ireland
`

const defaultStateCountries = `Alabama;USA
Alaska;USA
Arizona;USA
Arkansas;USA
California;USA
Colorado;USA
Connecticut;USA
Delaware;USA
Florida;USA
Georgia;USA
Hawaii;USA
Idaho;USA
Illinois;USA
Indiana;USA
Iowa;USA
Kansas;USA
Kentucky;USA
Louisiana;USA
Maine;USA
Maryland;USA
Massachusetts;USA
Michigan;USA
Minnesota;USA
Mississippi;USA
Missouri;USA
Montana;USA
Nebraska;USA
Nevada;USA
New Hampshire;USA
New Jersey;USA
New Mexico;USA
New York;USA
North Carolina;USA
North Dakota;USA
Ohio;USA
Oklahoma;USA
Oregon;USA
Pennsylvania;USA
Rhode Island;USA
South Carolina;USA
South Dakota;USA
Tennessee;USA
Texas;USA
Utah;USA
Vermont;USA
Virginia;USA
Washington;USA
West Virginia;USA
Wisconsin;USA
Wyoming;USA
Ontario;Canada
Quebec;Canada
British Columbia;Canada
Alberta;Canada
Manitoba;Canada
Saskatchewan;Canada
Nova Scotia;Canada
New Brunswick;Canada
Newfoundland and Labrador;Canada
Prince Edward Island;Canada
Northwest Territories;Canada
Yukon;Canada
Nunavut;Canada
New South Wales;Australia
Victoria;Australia
Queensland;Australia
South Australia;Australia
Western Australia;Australia
Tasmania;Australia
Australian Capital Territory;Australia
Northern Territory;Australia
England;UK
Scotland;UK
Wales;UK
Northern Ireland;UK
`

// Format: canonical;alias  (canonical name first, then the alias that maps to it).
const defaultCountryNames = `USA;USA
USA;{USA}
USA;US
USA;U.S.A.
USA;U.S.
USA;United States
USA;United States of America
Canada;Canada
Canada;{Canada}
Canada;CA
UK;UK
UK;{UK}
UK;U.K.
UK;United Kingdom
UK;Great Britain
UK;GB
Australia;Australia
Australia;{Australia}
Australia;AU
Germany;Germany
Germany;{Germany}
Germany;Deutschland
Germany;DE
France;France
France;{France}
France;FR
Italy;Italy
Italy;{Italy}
Italy;IT
The Netherlands;The Netherlands
The Netherlands;Netherlands
The Netherlands;{The Netherlands}
The Netherlands;{The} Netherlands
The Netherlands;{The} {Netherlands}
The Netherlands;Holland
The Netherlands;Nederland
The Netherlands;NL
Switzerland;Switzerland
Switzerland;{Switzerland}
Switzerland;Schweiz
Switzerland;CH
Austria;Austria
Austria;{Austria}
Austria;AT
Belgium;Belgium
Belgium;{Belgium}
Belgium;BE
Sweden;Sweden
Sweden;{Sweden}
Sweden;SE
Norway;Norway
Norway;{Norway}
Norway;NO
Denmark;Denmark
Denmark;{Denmark}
Denmark;DK
Finland;Finland
Finland;{Finland}
Finland;FI
Spain;Spain
Spain;{Spain}
Spain;ES
Portugal;Portugal
Portugal;{Portugal}
Portugal;PT
Poland;Poland
Poland;{Poland}
Poland;PL
Czech Republic;Czech Republic
Czech Republic;Czechia
Czech Republic;CZ
Hungary;Hungary
Hungary;{Hungary}
Hungary;HU
Romania;Romania
Romania;{Romania}
Romania;RO
Greece;Greece
Greece;{Greece}
Greece;GR
Turkey;Turkey
Turkey;{Turkey}
Turkey;TR
Japan;Japan
Japan;{Japan}
Japan;JP
China;China
China;{China}
China;CN
South Korea;South Korea
South Korea;Korea
South Korea;KR
India;India
India;{India}
India;IN
Brazil;Brazil
Brazil;{Brazil}
Brazil;BR
Mexico;Mexico
Mexico;{Mexico}
Mexico;MX
Argentina;Argentina
Argentina;{Argentina}
Argentina;AR
New Zealand;New Zealand
New Zealand;{New Zealand}
New Zealand;NZ
South Africa;South Africa
South Africa;{South Africa}
South Africa;ZA
Singapore;Singapore
Singapore;{Singapore}
Singapore;SG
Luxembourg;Luxembourg
Luxembourg;{Luxembourg}
Luxembourg;LU
Ireland;Ireland
Ireland;{Ireland}
Ireland;IE
Israel;Israel
Israel;{Israel}
Israel;IL
Russia;Russia
Russia;{Russia}
Russia;RU
Liechtenstein;Liechtenstein
Liechtenstein;LI
Albania;Albania
Albania;AL
Azerbaijan;Azerbaijan
Azerbaijan;AZ
Colombia;Colombia
Colombia;CO
Gabon;Gabon
Gabon;GA
Indonesia;Indonesia
Indonesia;ID
Cayman Islands;Cayman Islands
Cayman Islands;KY
Laos;Laos
Laos;LA
Montenegro;Montenegro
Montenegro;ME
Moldova;Moldova
Moldova;MD
Morocco;Morocco
Morocco;MA
Mongolia;Mongolia
Mongolia;MN
Montserrat;Montserrat
Montserrat;MS
Malta;Malta
Malta;MT
Niger;Niger
Niger;NE
Panama;Panama
Panama;PA
Seychelles;Seychelles
Seychelles;SC
Sudan;Sudan
Sudan;SD
Slovakia;Slovakia
Slovakia;SK
Tunisia;Tunisia
Tunisia;TN
Vatican City;Vatican City
Vatican City;Holy See
Vatican City;VA
`

// defaultBooktitleCountryNames contains English-only country aliases safe for
// use in title/booktitle fields.  Cross-language translations (e.g. Deutschland,
// Allemagne) are deliberately excluded: if a title uses a non-English country
// name the title is likely in that language and should not be altered.
// ISO two-letter codes are also excluded as they are ambiguous in title context.
const defaultBooktitleCountryNames = `USA;USA
USA;{USA}
USA;U.S.A.
USA;U.S.
USA;United States
USA;United States of America
Canada;Canada
Canada;{Canada}
UK;UK
UK;{UK}
UK;U.K.
UK;United Kingdom
UK;Great Britain
Australia;Australia
Australia;{Australia}
Germany;Germany
Germany;{Germany}
France;France
France;{France}
Italy;Italy
Italy;{Italy}
The Netherlands;The Netherlands
The Netherlands;{The Netherlands}
The Netherlands;Netherlands
The Netherlands;Holland
Switzerland;Switzerland
Switzerland;{Switzerland}
Austria;Austria
Austria;{Austria}
Belgium;Belgium
Belgium;{Belgium}
Sweden;Sweden
Sweden;{Sweden}
Norway;Norway
Norway;{Norway}
Denmark;Denmark
Denmark;{Denmark}
Finland;Finland
Finland;{Finland}
Spain;Spain
Spain;{Spain}
Portugal;Portugal
Portugal;{Portugal}
Poland;Poland
Poland;{Poland}
Czech Republic;Czech Republic
Czech Republic;Czechia
Hungary;Hungary
Hungary;{Hungary}
Romania;Romania
Romania;{Romania}
Greece;Greece
Greece;{Greece}
Turkey;Turkey
Turkey;{Turkey}
Japan;Japan
Japan;{Japan}
China;China
China;{China}
South Korea;South Korea
South Korea;Korea
India;India
India;{India}
Brazil;Brazil
Brazil;{Brazil}
Mexico;Mexico
Mexico;{Mexico}
Argentina;Argentina
Argentina;{Argentina}
New Zealand;New Zealand
New Zealand;{New Zealand}
South Africa;South Africa
South Africa;{South Africa}
Singapore;Singapore
Singapore;{Singapore}
Luxembourg;Luxembourg
Luxembourg;{Luxembourg}
Ireland;Ireland
Ireland;{Ireland}
Israel;Israel
Israel;{Israel}
Russia;Russia
Russia;{Russia}
Liechtenstein;Liechtenstein
Albania;Albania
Azerbaijan;Azerbaijan
Colombia;Colombia
Indonesia;Indonesia
Malta;Malta
Panama;Panama
Slovakia;Slovakia
`

// maybeWriteDefaultCsv writes content to path when the file is absent.
func maybeWriteDefaultCsv(path, content string) {
	if _, err := os.Stat(path); err == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		dbInteraction.Warning("Could not create directory for %s: %s", path, err)
		return
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		dbInteraction.Warning("Could not write default CSV %s: %s", path, err)
	}
}

// NormaliseAddressValue is the fieldNormalisers entry point for the address field.
// State/country alias resolution runs before literal-string normalisation so that
// braced abbreviations like {MA} or {USA} are matched while braces are still present.
func NormaliseAddressValue(l *TBibTeXLibrary, s string) string {
	return NormaliseLiteralString(l, l.NormaliseAddressNames(s))
}

// NormaliseBooktitleValue is the fieldNormalisers entry point for the booktitle field.
// Country alias resolution runs before title-string normalisation so that braced forms
// like {USA} or {Germany} are matched while braces are still present.
func NormaliseBooktitleValue(l *TBibTeXLibrary, s string) string {
	return NormaliseTitleString(l, l.NormaliseBooktitleLocationNames(s))
}

// NormaliseAddressNames resolves state and country aliases within a
// comma-separated address string and, when a canonical state is present
// without a country, appends the canonical country for that state.
func (l *TBibTeXLibrary) NormaliseAddressNames(address string) string {
	parts := strings.Split(address, ",")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}

	// Apply state and country alias normalisation to each address component.
	for i, part := range parts {
		if canonical, ok := l.StateAliasToCanonical[part]; ok {
			parts[i] = canonical
		}
		if canonical, ok := l.CountryAliasToCanonical[part]; ok {
			parts[i] = canonical
		}
	}

	// If no canonical country is present but a canonical state is, add its country.
	hasCountry := false
	for _, part := range parts {
		if _, ok := l.CountryAliasToCanonical[part]; ok {
			hasCountry = true
			break
		}
	}
	if !hasCountry {
		for _, part := range parts {
			if country, ok := l.StateToCountry[part]; ok {
				parts = append(parts, country)
				break
			}
		}
	}

	return strings.Join(parts, ", ")
}

// NormaliseBooktitleLocationNames replaces state and country name aliases in
// the comma-separated parts of a conference title or booktitle with their
// canonical forms.  Unlike NormaliseAddressNames it does not append a country
// when only a state is found, and it preserves the surrounding whitespace of
// each part so the rest of the title string is unaffected.
func (l *TBibTeXLibrary) NormaliseBooktitleLocationNames(title string) string {
	parts := strings.Split(title, ",")
	changed := false
	for i, part := range parts {
		trimmed := strings.TrimSpace(part)
		var canonical string
		if c, ok := l.BooktitleCountryAliasToCanonical[trimmed]; ok {
			canonical = c
		}
		if canonical == "" || canonical == trimmed {
			continue
		}
		leadLen := len(part) - len(strings.TrimLeft(part, " \t"))
		trailStart := len(strings.TrimRight(part, " \t"))
		parts[i] = part[:leadLen] + canonical + part[trailStart:]
		changed = true
	}
	if !changed {
		return title
	}
	return strings.Join(parts, ",")
}


// CheckAddressMappings validates the loaded address tables:
//   - warns when a canonical state name also appears as a country alias
//     (the country normalisation will win, which may not be intended)
//   - warns when a canonical state has no country mapping
func (l *TBibTeXLibrary) CheckAddressMappings() {
	// Collect canonical state names.
	canonicalStates := map[string]bool{}
	for _, canonical := range l.StateAliasToCanonical {
		canonicalStates[canonical] = true
	}

	for canonical := range canonicalStates {
		// A canonical state that is also a country alias will always resolve
		// to the country during address normalisation.
		if _, isCountry := l.CountryAliasToCanonical[canonical]; isCountry {
			l.Warning(WarningStateOverlapsCountry, canonical)
		}
		// Every canonical state should have a country mapping.
		if _, hasCountry := l.StateToCountry[canonical]; !hasCountry {
			l.Warning(WarningStateHasNoCountry, canonical)
		}
	}
}
