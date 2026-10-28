package contract

import (
	"errors"

	"github.com/google/uuid"
)

var ErrPublicationVersionConflict = errors.New("publication contract version precondition failed")

type PublicationConditionMode string

const (
	PublicationAnyVersion   PublicationConditionMode = "any"
	PublicationMustBeAbsent PublicationConditionMode = "absent"
	PublicationExactVersion PublicationConditionMode = "exact"
)

// PublicationCondition guards the ingestion contract, not every metadata edit.
// Presentation changes may retain the token. The zero value is invalid.
type PublicationCondition struct {
	Mode    PublicationConditionMode `json:"mode"`
	Version uuid.UUID                `json:"version"`
}

func (c PublicationCondition) Validate() error {
	switch c.Mode {
	case PublicationAnyVersion, PublicationMustBeAbsent:
		if c.Version == uuid.Nil {
			return nil
		}
	case PublicationExactVersion:
		if c.Version != uuid.Nil {
			return nil
		}
	}
	return errors.New("explicit valid publication version condition required")
}

func (c PublicationCondition) Check(previous *Publication) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Mode == PublicationAnyVersion || (c.Mode == PublicationMustBeAbsent && previous == nil) || (c.Mode == PublicationExactVersion && previous != nil && c.Version == previous.Version) {
		return nil
	}
	return ErrPublicationVersionConflict
}
