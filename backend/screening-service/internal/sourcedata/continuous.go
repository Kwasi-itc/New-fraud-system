package sourcedata

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
)

// Version 2 lives in the existing field_map_json owner contract. Version 1
// legacy maps retain their original provider semantics; they are never upgraded
// implicitly. Internal continuous configuration uses the existing authenticated
// operator API, not a customer-supplied tenant as provider authority.
type ContinuousMapping struct {
	Version   int               `json:"version"`
	Fields    map[string]string `json:"fields"`
	Selection Selection         `json:"provider_config"`
}

func DecodeContinuous(raw json.RawMessage) (ContinuousMapping, error) {
	var m ContinuousMapping
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if len(raw) > 8192 || d.Decode(&m) != nil || d.Decode(new(any)) != io.EOF || m.Version != 2 || len(m.Fields) < 1 || len(m.Fields) > 10 {
		return m, ErrInput
	}
	selection, _ := json.Marshal(m.Selection)
	if _, e := DecodeSelection(selection); e != nil {
		return m, e
	}
	field := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)
	for key, kind := range m.Fields {
		if !field.MatchString(key) || (kind != "Person" && kind != "Organization" && kind != "Vessel" && kind != "Aircraft") {
			return m, ErrInput
		}
	}
	return m, nil
}
