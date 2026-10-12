package blob

import (
	"context"
	"testing"
	"time"
)

func TestUnconfiguredStorageDoesNotFabricateEvidenceLocations(t *testing.T) {
	c := NewHTTPClient("", time.Second)
	session, err := c.CreateUploadSession(context.Background(), "tenant", "screening", "evidence.pdf", "application/pdf", 100)
	if err == nil || session.StorageKey != "" || session.UploadURL != "" {
		t.Fatalf("unconfigured upload: %+v %v", session, err)
	}
	download, err := c.GetDownloadURL(context.Background(), "private/storage-key")
	if err == nil || download.DownloadURL != "" {
		t.Fatalf("unconfigured download: %+v %v", download, err)
	}
}
