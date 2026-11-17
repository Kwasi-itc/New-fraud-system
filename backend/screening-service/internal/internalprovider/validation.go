package internalprovider

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"golang.org/x/text/language"
)

func validFuzzyQuery(q screening.SearchQuery) bool {
	if utf8.RuneCountInString(q.Name) > 200 || utf8.RuneCountInString(q.Address) > 300 || utf8.RuneCountInString(q.RegistrationNumber) > 100 {
		return false
	}
	for _, value := range []string{q.Nationality, q.Country} {
		if value == "" {
			continue
		}
		region, err := language.ParseRegion(strings.ToUpper(value))
		if err != nil || len(value) != 2 || !region.IsCountry() {
			return false
		}
	}
	if q.BirthDate != "" {
		layout := "2006-01-02"
		if len(q.BirthDate) == 4 {
			layout = "2006"
		}
		date, err := time.Parse(layout, q.BirthDate)
		if err != nil || date.Year() < 1 || date.After(time.Now().UTC()) {
			return false
		}
	}
	return true
}
