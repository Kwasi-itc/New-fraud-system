package mysql

import (
	"context"
	"encoding/json"
	c "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

func (r referenceRepo) PublishedEvidence(ctx context.Context, t, s, file, event uuid.UUID) (c.File, *uuid.UUID, error) {
	out := c.File{TenantID: t, SourceFileID: &file}
	var decision *uuid.UUID
	var raw []byte
	// Match the immutable outbox publication and actual owner file; a service
	// credential alone cannot invent a file or move it across screenings/tenants.
	e := r.db.QueryRowContext(ctx, "SELECT f.file_name,f.content_type,f.file_size,f.storage_key,f.uploaded_by,s.decision_id,e.payload FROM "+r.schema+".screening_files f JOIN "+r.schema+".screening_screenings s ON s.tenant_id=f.tenant_id AND s.id=f.screening_id JOIN "+r.schema+".screening_case_event_outbox e ON e.tenant_id=f.tenant_id AND e.id=f.id AND e.kind='evidence-uploaded' WHERE f.tenant_id=? AND f.screening_id=? AND f.id=? AND e.id=?", id(t), id(s), id(file), id(event)).Scan(&out.FileName, &out.ContentType, &out.FileSize, &out.StorageKey, &out.UploadedBy, nullableID{&decision}, &raw)
	if e != nil {
		return out, nil, screeningContractError(e)
	}
	var publication struct {
		EventID     uuid.UUID `json:"event_id"`
		TenantID    uuid.UUID `json:"tenant_id"`
		ScreeningID uuid.UUID `json:"screening_id"`
		FileID      uuid.UUID `json:"file_id"`
		UploadedBy  string    `json:"uploaded_by"`
	}
	if json.Unmarshal(raw, &publication) != nil || publication.EventID != event || publication.TenantID != t || publication.ScreeningID != s || publication.FileID != file || publication.UploadedBy != out.UploadedBy {
		return out, nil, c.Invalid("invalid source evidence publication")
	}
	return out, decision, nil
}

func (r referenceRepo) ImportedEvidence(ctx context.Context, t, caseID, source uuid.UUID) (*c.File, error) {
	rows, e := r.db.QueryContext(ctx, "SELECT "+fileCols+" FROM "+r.table("case_files")+" WHERE tenant_id=? AND case_id=? AND source_file_id=? LIMIT 2", id(t), id(caseID), id(source))
	files, e := collect(rows, e, scanFile)
	if e != nil {
		return nil, e
	}
	if len(files) > 1 {
		return nil, c.ErrConflict
	}
	if len(files) == 0 {
		return nil, nil
	}
	return &files[0], nil
}
