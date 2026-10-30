// Package mysqlrelease verifies decision-owned deployment artifacts. It does
// not collect target definitions, grant foreign-owner access or activate APIs.
package mysqlrelease

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
)

const MaxArtifactBytes = 16 << 20

// Identity must come from reviewed release/deployment configuration, not the
// artifact being checked. SHA-256 binds bytes; it is NOT a signature/approval.
type Identity struct {
	Database     string `json:"database"`
	Definer      string `json:"definer"`
	MySQLVersion string `json:"mysql_version"`
	Revision     string `json:"revision"`
	SourceSHA256 string `json:"source_sha256"`
	ImageDigest  string `json:"image_digest"`
}

// Artifact is serialized as compact encoding/json output followed by one LF.
// Definitions must be frozen on a separate pinned reference server, reviewed
// and promoted with the image. Never learn them from the deployment target.
type Artifact struct {
	Format int `json:"format"`
	Identity
	Steps []migrations.Step `json:"steps"`
}

type Release struct {
	identity Identity
	plan     migrations.Plan
	digest   string
}

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func (i Identity) validate() error {
	// 8.4.8 is the currently rehearsed target, not a compatibility assertion
	// for other MySQL releases or Aurora variants.
	if i.MySQLVersion != "8.4.8" || !revisionPattern.MatchString(i.Revision) || !sha256Pattern.MatchString(i.SourceSHA256) || len(i.ImageDigest) != 71 || i.ImageDigest[:7] != "sha256:" || !sha256Pattern.MatchString(i.ImageDigest[7:]) {
		return errors.New("exact rehearsed version and immutable source/image identities required")
	}
	return nil
}

func Decode(r io.Reader, expectedSHA256 string, expected Identity) (*Release, error) {
	if !sha256Pattern.MatchString(expectedSHA256) {
		return nil, errors.New("externally pinned artifact SHA-256 required")
	}
	if err := expected.validate(); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(r, MaxArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxArtifactBytes || !utf8.Valid(raw) {
		return nil, errors.New("invalid or oversized release artifact")
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != expectedSHA256 {
		return nil, errors.New("release artifact checksum mismatch")
	}
	var artifact Artifact
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&artifact); err != nil {
		return nil, fmt.Errorf("release artifact: %w", err)
	}
	canonical, err := json.Marshal(artifact)
	if err != nil {
		return nil, err
	}
	// Canonical bytes reject duplicate fields, trailing JSON, ambiguous casing
	// and alternate encodings; do not accept last-key-wins deployment metadata.
	if !bytes.Equal(raw, append(canonical, '\n')) {
		return nil, errors.New("release artifact must use canonical JSON followed by LF")
	}
	if artifact.Format != 1 || artifact.Identity != expected {
		return nil, errors.New("release identity differs from approved deployment binding")
	}
	plan, err := metadata.DispatchPlan(expected.Database, expected.Definer, artifact.Steps)
	if err != nil {
		return nil, err
	}
	return &Release{identity: expected, plan: plan, digest: expectedSHA256}, nil
}

func (r *Release) Digest() string     { return r.digest }
func (r *Release) Identity() Identity { return r.identity }

// Plan returns a copy so a caller cannot replace SQL after artifact validation.
func (r *Release) Plan() migrations.Plan {
	p := r.plan
	p.Steps = append([]migrations.Step(nil), p.Steps...)
	return p
}

// CheckTarget is a diagnostic preflight, NOT a reusable write/activation permit.
// Callers must open the inspection pool with verified TLS. Verify repeats this
// check on its actual pinned inspection session; production apply is excluded
// until the shared installer accepts a same-session pre-write target guard.
func (r *Release) CheckTarget(ctx context.Context, db *sql.DB, expectedServerUUID string) error {
	if db == nil {
		return errors.New("inspection pool required")
	}
	return r.checkTarget(ctx, db, expectedServerUUID)
}

type targetQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (r *Release) checkTarget(ctx context.Context, db targetQuery, expectedServerUUID string) error {
	serverID, err := uuid.Parse(expectedServerUUID)
	if r == nil || db == nil || err != nil || serverID == uuid.Nil || serverID.String() != expectedServerUUID {
		return errors.New("release, installer and exact writer UUID required")
	}
	var database, version, actualServer string
	var readOnly, superReadOnly bool
	if err := db.QueryRowContext(ctx, "SELECT DATABASE(),VERSION(),@@server_uuid,@@global.read_only,@@global.super_read_only").Scan(&database, &version, &actualServer, &readOnly, &superReadOnly); err != nil {
		return err
	}
	if database != r.identity.Database || version != r.identity.MySQLVersion || actualServer != expectedServerUUID || readOnly || superReadOnly {
		return errors.New("target database/version/writer differs or target is read-only")
	}
	var locked string
	if err := db.QueryRowContext(ctx, "SELECT account_locked FROM mysql.user WHERE User=? AND Host='localhost'", r.identity.Definer).Scan(&locked); err != nil {
		return fmt.Errorf("locked definer inspection: %w", err)
	}
	if locked != "Y" {
		return errors.New("dedicated definer must be locked")
	}
	return nil
}

// Verify performs no DDL, ledger repair or permission changes. A successful
// result establishes only this owner's schema, never shared-stack activation.
func (r *Release) Verify(ctx context.Context, db *sql.DB, serverUUID string) error {
	if err := r.CheckTarget(ctx, db, serverUUID); err != nil {
		return err
	}
	runner, err := migrations.New(db, "decision_engine", r.Plan())
	if err != nil {
		return err
	}
	return runner.Verify(ctx, func(conn *sql.Conn) error { return r.checkTarget(ctx, conn, serverUUID) })
}
