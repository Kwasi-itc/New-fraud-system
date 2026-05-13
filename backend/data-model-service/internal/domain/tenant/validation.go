package tenant

import (
	"fmt"
	"strings"
)

func ValidateCreate(input CreateInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("tenant name is required")
	}

	return nil
}
