package app

import "strings"

// OutboxPublishingEnabled is the shared API/worker policy. Disabling publication
// does not disable decisions, workflow actions, screening, scoring or callbacks.
func (c Config) OutboxPublishingEnabled() bool {
	return strings.TrimSpace(c.OutboxPublisherURL) != ""
}
