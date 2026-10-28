package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
)

func decodePlan(data []byte) (migrations.Plan, error) {
	var plan migrations.Plan
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return plan, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return plan, errors.New("one JSON plan required")
	}
	return plan, plan.Validate()
}
