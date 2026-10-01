package handlers

import (
	"bytes"
	"encoding/json"
)

// nullable differentiates omitted PATCH fields from an explicit JSON null.
type nullable[T any] struct {
	Present bool
	Value   *T
}

func (n *nullable[T]) UnmarshalJSON(raw []byte) error {
	n.Present = true
	n.Value = nil
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	n.Value = &value
	return nil
}
