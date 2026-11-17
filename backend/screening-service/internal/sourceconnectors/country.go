package sourceconnectors

import (
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
	"golang.org/x/text/unicode/norm"
	"strings"
	"unicode"
)

// Versioned source-country-v1 mapping. Unrecognised source text is retained as
// text, never invented as an ISO code or treated as a strong identity conflict.
func countryKey(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, norm.NFD.String(value))
}

var countryNames = func() map[string]string {
	out := map[string]string{}
	for _, region := range language.Supported.Regions() {
		if region.IsCountry() {
			code := strings.ToLower(region.String())
			out[countryKey(display.English.Regions().Name(region))] = code
		}
	}
	for name, code := range map[string]string{"United States of America": "us", "United Kingdom of Great Britain and Northern Ireland": "gb", "Russian Federation": "ru", "Iran (Islamic Republic of)": "ir", "Iran Islamic Republic of": "ir", "Syrian Arab Republic": "sy", "Democratic People's Republic of Korea": "kp", "Korea Democratic People's Republic of": "kp", "Republic of Korea": "kr", "Korea Republic of": "kr", "Cote d'Ivoire": "ci", "Viet Nam": "vn", "Türkiye": "tr", "Turkey": "tr", "United Republic of Tanzania": "tz", "Moldova Republic of": "md", "Venezuela Bolivarian Republic of": "ve", "Bolivia Plurinational State of": "bo", "Lao People's Democratic Republic": "la", "State of Palestine": "ps", "The Gambia": "gm"} {
		out[countryKey(name)] = code
	}
	return out
}()

func CountryCode(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 2 {
		if region, err := language.ParseRegion(value); err == nil && region.IsCountry() {
			return strings.ToLower(region.String())
		}
	}
	return countryNames[countryKey(value)]
}
