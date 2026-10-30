package screening

import (
	"context"
	"errors"
)

var ErrRecognitionFailed = errors.New("Name recognition failed; no screening was submitted.")

type RecognizedName struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type NameRecognizer func(context.Context, string) ([]RecognizedName, error)
