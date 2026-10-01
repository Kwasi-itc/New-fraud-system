package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

func (s CaseService) StartUpload(ctx context.Context, file casepkg.File) (casepkg.Upload, error) {
	actor := access.Actor(ctx)
	if actor == nil {
		return casepkg.Upload{}, casepkg.ErrForbidden
	}
	file.FileName = strings.TrimSpace(file.FileName)
	if file.FileName == "" || len(file.FileName) > 200 || strings.ContainsAny(file.FileName, "/\\\x00\r\n") || file.FileSize < 1 || file.FileSize > casepkg.MaxEvidenceBytes {
		return casepkg.Upload{}, casepkg.Invalid("file name and size (1 byte to 10 MiB) required")
	}
	switch file.ContentType {
	case "application/pdf", "image/png", "image/jpeg", "text/plain", "text/csv":
	default:
		return casepkg.Upload{}, casepkg.Invalid("supported evidence types: PDF, PNG, JPEG, plain text, CSV")
	}
	return transact(ctx, s, func(tx CaseService) (casepkg.Upload, error) {
		item, err := tx.caseForMutation(ctx, file.TenantID, file.CaseID, actor)
		if err != nil {
			return casepkg.Upload{}, err
		}
		if item.Status == casepkg.StatusClosed {
			return casepkg.Upload{}, casepkg.ErrConflict
		}
		file.ID = tx.ids.New()
		file.UploadedBy = *actor
		file.CreatedAt = tx.clock.Now()
		file.StorageKey = ""
		file.SourceFileID = nil
		upload := casepkg.Upload{File: file, ExpiresAt: file.CreatedAt.Add(24 * time.Hour)}
		return upload, tx.uploads.Create(ctx, upload)
	})
}

func (s CaseService) UploadContent(ctx context.Context, tenant, id, upload uuid.UUID, data []byte) error {
	if len(data) < 1 || len(data) > casepkg.MaxEvidenceBytes {
		return casepkg.Invalid("evidence exceeds size limit or is empty")
	}
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	kind, _, err := mime.ParseMediaType(http.DetectContentType(data))
	if err != nil {
		return casepkg.Invalid("invalid content type")
	}
	_, err = transact(ctx, s, func(tx CaseService) (struct{}, error) {
		if _, err := tx.caseForMutation(ctx, tenant, id, access.Actor(ctx)); err != nil {
			return struct{}{}, err
		}
		u, err := tx.uploads.Lock(ctx, tenant, id, upload)
		if err != nil {
			return struct{}{}, err
		}
		actor := access.Actor(ctx)
		if actor == nil || *actor != u.UploadedBy {
			return struct{}{}, casepkg.ErrForbidden
		}
		if u.Finalized || !u.ExpiresAt.After(tx.clock.Now()) {
			return struct{}{}, casepkg.ErrConflict
		}
		if int64(len(data)) != u.FileSize || (kind != u.ContentType && !(kind == "text/plain" && u.ContentType == "text/csv")) {
			return struct{}{}, casepkg.Invalid("uploaded bytes do not match declared size/type")
		}
		if u.SHA256 != "" && u.SHA256 != hash {
			return struct{}{}, casepkg.ErrConflict
		}
		u.Data = data
		u.SHA256 = hash
		return struct{}{}, tx.uploads.Save(ctx, u)
	})
	return err
}

func (s CaseService) FinalizeUpload(ctx context.Context, tenant, id, upload uuid.UUID) (casepkg.File, error) {
	return transact(ctx, s, func(tx CaseService) (casepkg.File, error) {
		actor := access.Actor(ctx)
		item, err := tx.caseForMutation(ctx, tenant, id, actor)
		if err != nil {
			return casepkg.File{}, err
		}
		u, err := tx.uploads.Lock(ctx, tenant, id, upload)
		if err != nil {
			return casepkg.File{}, err
		}
		if actor == nil || *actor != u.UploadedBy {
			return casepkg.File{}, casepkg.ErrForbidden
		}
		if u.Finalized {
			return u.File, nil
		}
		if item.Status == casepkg.StatusClosed || !u.ExpiresAt.After(tx.clock.Now()) || u.SHA256 == "" || int64(len(u.Data)) != u.FileSize {
			return casepkg.File{}, casepkg.ErrConflict
		}
		file, err := tx.files.Create(ctx, u.File)
		if err != nil {
			return file, err
		}
		u.Finalized = true
		if err := tx.uploads.Save(ctx, u); err != nil {
			return file, err
		}
		if err := tx.contributors.Add(ctx, tenant, id, *actor, tx.clock.Now()); err != nil {
			return file, err
		}
		_, err = tx.events.Create(ctx, newEvent(tx.ids.New(), tenant, id, actor, "evidence_attached", u.SHA256, u.ID.String(), "case_file", u.FileName, "", tx.clock.Now()))
		return file, err
	})
}

func (s CaseService) DownloadEvidence(ctx context.Context, tenant, id, file uuid.UUID) (casepkg.Upload, error) {
	return transact(ctx, s, func(tx CaseService) (casepkg.Upload, error) {
		if _, err := tx.workspaceCase(ctx, tenant, id); err != nil {
			return casepkg.Upload{}, err
		}
		u, err := tx.uploads.Lock(ctx, tenant, id, file)
		if err != nil {
			return u, err
		}
		if !u.Finalized {
			return casepkg.Upload{}, casepkg.ErrNotFound
		}
		return u, nil
	})
}
