package screening

import "strings"

type IdentifierMapping struct {
	Type         string `json:"type"`
	ValueField   string `json:"value_field"`
	CountryField string `json:"country_field,omitempty"`
	Issuer       string `json:"issuer,omitempty"`
}

func (m IdentifierMapping) Valid() bool {
	switch m.Type {
	case "passport", "national_id", "registration", "tax_id", "lei", "imo":
	default:
		return false
	}
	return fieldName.MatchString(m.ValueField) && (m.CountryField == "" || fieldName.MatchString(m.CountryField)) && len(m.Issuer) <= 200 && !strings.ContainsRune(m.Issuer, 0) && (m.Type == "lei" || m.Type == "imo" || m.CountryField != "" || strings.TrimSpace(m.Issuer) != "")
}
