package mysqlstore

import (
	"database/sql/driver"
	"errors"
	"strings"
	"unicode/utf8"
)

// Text preserves Unicode text without trimming, normalization, case folding or
// truncation. Embedded NUL remains invalid, as in the PostgreSQL text domain.
// Owners must enforce column/payload limits and exact-key comparison separately.
type Text string

func (s Text) Value() (driver.Value, error) {
	if !utf8.ValidString(string(s)) || strings.ContainsRune(string(s), 0) {
		return nil, errors.New("text requires valid UTF-8 without NUL")
	}
	return string(s), nil
}

func (s *Text) Scan(value any) error {
	if s == nil {
		return errors.New("nil text destination")
	}
	*s = ""
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case []byte:
		text = string(v)
	default:
		return errors.New("text requires non-null textual storage")
	}
	if _, err := (Text(text)).Value(); err != nil {
		return err
	}
	*s = Text(text)
	return nil
}

type NullText struct {
	Text  Text
	Valid bool
}

func (s NullText) Value() (driver.Value, error) {
	if !s.Valid {
		return nil, nil
	}
	return s.Text.Value()
}
func (s *NullText) Scan(value any) error {
	if s == nil {
		return errors.New("nil nullable text destination")
	}
	*s = NullText{}
	if value == nil {
		return nil
	}
	if err := s.Text.Scan(value); err != nil {
		return err
	}
	s.Valid = true
	return nil
}
