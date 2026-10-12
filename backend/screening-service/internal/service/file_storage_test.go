package service

import (
	"context"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/blob"
)

func TestUnconfiguredFileUploadDoesNotCreateMetadata(t *testing.T) {
	files := &fakeFileRepo{}
	s := ScreeningService{fileRepo: files, blobStore: blob.NewHTTPClient("", time.Second)}
	file, session, err := s.CreateFileUpload(context.Background(), "tenant", "screening", "evidence.pdf", "application/pdf", "analyst", 100)
	if err == nil || file.ID != "" || session.StorageKey != "" || len(files.items) != 0 {
		t.Fatalf("unconfigured upload fabricated evidence: %+v %+v %v", file, session, err)
	}
}
