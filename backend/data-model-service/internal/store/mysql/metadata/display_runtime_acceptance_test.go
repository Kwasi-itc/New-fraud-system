package metadata_test

import "testing"

// Exercise the same owner/publication path now mounted by the local PUT route.
func TestDisplayRuntimePublicationAcceptance(t *testing.T) {
	testPhysicalCreationPublication(t, true, false,
		"service_allocated_display_result_owner_commit",
		"service_allocated_display_result_owner_concurrent",
		"service_allocated_display_result_owner_lost_commit",
		"service_allocated_display_result_owner_revision_failure",
		"service_allocated_display_result_owner_restricted")
}
