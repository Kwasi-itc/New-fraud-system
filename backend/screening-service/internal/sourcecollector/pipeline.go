package sourcecollector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconnectors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Pipeline struct{ DB *pgxpool.Pool }

// Register creates disabled, unreviewed GLOBAL collection metadata atomically.
// It neither grants tenants access nor claims source rights or activates a feed.
func (p Pipeline) Register(ctx context.Context, key sourceingest.Key, spec sourceconnectors.Spec) error {
	if key.Owner != uuid.Nil || !(sourcedata.DatasetRef{Scope: "global", ID: key.Dataset}).Valid() {
		return sourcedata.ErrInput
	}
	known, err := sourceconnectors.Lookup(spec.Key)
	if err != nil || known.Parser != spec.Parser {
		return sourceconnectors.ErrSource
	}
	tx, err := p.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_datasets(owner_scope,id,label,category) VALUES($1,$2,$3,$4)`, key.Owner, key.Dataset, spec.Label, spec.Category); err != nil {
		return err
	}
	metadata, _ := json.Marshal(map[string]any{"coverage": spec.Coverage, "reference": spec.Reference, "parser": spec.Parser, "rights_note": spec.Rights})
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_connections(owner_scope,dataset_id,connector,config) VALUES($1,$2,$3,$4)`, key.Owner, key.Dataset, spec.Key, metadata); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// StageSnapshot streams the complete validated parser into the existing bounded
// source-v2 staging path. A parser/truncation failure closes the pipe with an
// error, so no apparently complete JSON snapshot can be published accidentally.
func (p Pipeline) StageSnapshot(ctx context.Context, key sourceingest.Key, spec sourceconnectors.Spec, path string) (uuid.UUID, sourceconnectors.Parsed, error) {
	file, err := os.Open(path)
	if err != nil {
		return uuid.Nil, sourceconnectors.Parsed{}, sourceconnectors.ErrDownload
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > sourceconnectors.MaxArtifactBytes {
		return uuid.Nil, sourceconnectors.Parsed{}, sourceconnectors.ErrLimit
	}
	reader, writer := io.Pipe()
	type result struct {
		parsed sourceconnectors.Parsed
		err    error
	}
	done := make(chan result, 1)
	go func() {
		prefix, _ := json.Marshal(map[string]any{"source": spec.Key, "parser": spec.Parser})
		// Version precedes records in source-v2, permitting its larger explicit bound.
		_, e := io.WriteString(writer, `{"version":2,`+string(prefix[1:len(prefix)-1])+`,"records":[`)
		count := 0
		var parsed sourceconnectors.Parsed
		if e == nil {
			parsed, e = sourceconnectors.Parse(ctx, spec, file, func(record sourcedata.Record) error {
				if count > 0 {
					if _, e := io.WriteString(writer, ","); e != nil {
						return e
					}
				}
				count++
				return json.NewEncoder(writer).Encode(record)
			})
		}
		if e == nil {
			_, e = io.WriteString(writer, "]}")
		}
		writer.CloseWithError(e)
		done <- result{parsed, e}
	}()
	generation, stageErr := (sourceingest.Store{DB: p.DB, GlobalOnly: true}).Stage(ctx, key, reader)
	reader.CloseWithError(stageErr)
	parsed := <-done
	if ctx.Err() != nil {
		return generation, parsed.parsed, ctx.Err()
	}
	if stageErr != nil {
		return generation, parsed.parsed, stageErr
	}
	if parsed.err != nil {
		return generation, parsed.parsed, parsed.err
	}
	if err = p.AttachOriginal(ctx, key, generation, path); err != nil {
		return generation, parsed.parsed, err
	}
	return generation, parsed.parsed, nil
}

func (p Pipeline) AttachOriginal(ctx context.Context, key sourceingest.Key, generation uuid.UUID, path string) error {
	if key.Owner != uuid.Nil {
		return sourcedata.ErrPublish
	}
	file, err := os.Open(path)
	if err != nil {
		return sourceconnectors.ErrDownload
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, sourceconnectors.MaxArtifactBytes+1))
	if err != nil || len(raw) == 0 || int64(len(raw)) > sourceconnectors.MaxArtifactBytes {
		return sourceconnectors.ErrLimit
	}
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	tx, err := p.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM screening.source_versions WHERE owner_scope=$1 AND dataset_id=$2 AND id=$3 FOR NO KEY UPDATE`, key.Owner, key.Dataset, generation).Scan(&status); err != nil {
		return err
	}
	if status != "staged" {
		return sourcedata.ErrConflict
	}
	var previous string
	if err = tx.QueryRow(ctx, `SELECT sha256 FROM screening.source_version_artifacts WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$3`, key.Owner, key.Dataset, generation).Scan(&previous); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_artifacts(owner_scope,dataset_id,sha256,body,media_type) VALUES($1,$2,$3,$4,'application/xml') ON CONFLICT DO NOTHING`, key.Owner, key.Dataset, digest, raw); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE screening.source_version_artifacts SET sha256=$4 WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$3`, key.Owner, key.Dataset, generation, digest); err != nil {
		return err
	}
	if previous != digest {
		if _, err = tx.Exec(ctx, `DELETE FROM screening.source_artifacts a WHERE owner_scope=$1 AND dataset_id=$2 AND sha256=$3 AND NOT EXISTS(SELECT 1 FROM screening.source_version_artifacts v WHERE (v.owner_scope,v.dataset_id,v.sha256)=(a.owner_scope,a.dataset_id,a.sha256))`, key.Owner, key.Dataset, previous); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// PreserveArtifact retains a complete downloaded payload before parsing, including
// quarantined malformed snapshots. It is not publication or a rights approval.
func (p Pipeline) PreserveArtifact(ctx context.Context, key sourceingest.Key, path string) (string, error) {
	if key.Owner != uuid.Nil {
		return "", sourcedata.ErrPublish
	}
	file, e := os.Open(path)
	if e != nil {
		return "", sourceconnectors.ErrDownload
	}
	defer file.Close()
	raw, e := io.ReadAll(io.LimitReader(file, sourceconnectors.MaxArtifactBytes+1))
	if e != nil || len(raw) == 0 || int64(len(raw)) > sourceconnectors.MaxArtifactBytes {
		return "", sourceconnectors.ErrLimit
	}
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	_, e = p.DB.Exec(ctx, `INSERT INTO screening.source_artifacts(owner_scope,dataset_id,sha256,body,media_type) VALUES($1,$2,$3,$4,'application/xml') ON CONFLICT DO NOTHING`, key.Owner, key.Dataset, digest, raw)
	return digest, e
}
