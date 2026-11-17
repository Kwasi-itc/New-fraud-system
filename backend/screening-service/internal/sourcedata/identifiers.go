package sourcedata

import (
	"golang.org/x/text/language"
	"strings"
	"unicode/utf8"
)

type Identifier struct {
	Type         string `json:"type"`
	Value        string `json:"value"`
	Country      string `json:"country,omitempty"`
	Issuer       string `json:"issuer,omitempty"`
	OriginalType string `json:"original_type,omitempty"`
}

func (id Identifier) Validate() error {
	if id.Type != "passport" && id.Type != "national_id" && id.Type != "registration" && id.Type != "tax_id" && id.Type != "lei" && id.Type != "imo" && id.Type != "other" {
		return ErrInput
	}
	if strings.TrimSpace(id.Value) == "" || len(id.Value) > 1000 || !utf8.ValidString(id.Value) || strings.ContainsRune(id.Value, 0) || len(id.Country) > 2 || len(id.Issuer) > 200 || len(id.OriginalType) > 200 {
		return ErrInput
	}
	if id.Country != "" {
		region, e := language.ParseRegion(id.Country)
		if len(id.Country) != 2 || e != nil || !region.IsCountry() {
			return ErrInput
		}
	}
	return nil
}

// Preserve punctuation/internal spacing and leading zeros. IDs are never fuzzy.
func NormalizeIdentifier(value string) string { return strings.TrimSpace(value) }

// Queries preserve the source namespace and original identifier spelling. We
// require a jurisdiction or issuer for locally issued identifiers; LEI/IMO are
// globally assigned. Unsupported "other" source types remain evidence only.
func ValidateQueryIdentifiers(ids []Identifier) error {
	if len(ids) > 10 {
		return ErrInput
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id.Validate() != nil || id.Type == "other" || id.OriginalType != "" || len(id.Value) > 200 || !utf8.ValidString(id.Issuer) || strings.ContainsRune(id.Issuer, 0) || (id.Type != "lei" && id.Type != "imo" && id.Country == "" && strings.TrimSpace(id.Issuer) == "") {
			return ErrInput
		}
		key := id.Type + "\x00" + NormalizeIdentifier(id.Value) + "\x00" + strings.ToLower(id.Country) + "\x00" + strings.TrimSpace(id.Issuer)
		if seen[key] {
			return ErrInput
		}
		seen[key] = true
	}
	return nil
}

func IdentifierNamespace(a, b Identifier) bool {
	return a.Type == b.Type && strings.EqualFold(a.Country, b.Country) && strings.TrimSpace(a.Issuer) == strings.TrimSpace(b.Issuer)
}
