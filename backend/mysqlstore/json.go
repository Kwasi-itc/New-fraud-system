package mysqlstore

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// JSON is a non-null JSON document. Validation does not decode numbers or
// normalize keys. MySQL may normalize the document; this is not a byte-exact
// payload archive or a promise that every JSON number fits MySQL's range.
type JSON []byte

func (j JSON) Value() (driver.Value, error) {
	if !utf8.Valid(j) || !json.Valid(j) {
		return nil, errors.New("invalid UTF-8 JSON document")
	}
	// Bind text, not a binary-character-set parameter. Conversion owns its bytes.
	return string(j), nil
}

func (j *JSON) Scan(value any) error {
	if j == nil {
		return errors.New("nil JSON destination")
	}
	*j = nil
	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return errors.New("JSON requires non-null document text")
	}
	if !utf8.Valid(data) || !json.Valid(data) {
		return errors.New("invalid UTF-8 JSON document")
	}
	*j = append(JSON(nil), data...)
	return nil
}

// NullJSON distinguishes SQL NULL (Valid=false) from the document null.
type NullJSON struct {
	JSON  JSON
	Valid bool
}

func (n NullJSON) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.JSON.Value()
}

func (n *NullJSON) Scan(value any) error {
	if n == nil {
		return errors.New("nil nullable JSON destination")
	}
	*n = NullJSON{}
	if value == nil {
		return nil
	}
	if err := n.JSON.Scan(value); err != nil {
		return err
	}
	n.Valid = true
	return nil
}
