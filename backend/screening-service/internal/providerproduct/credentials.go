package providerproduct

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrCredential = errors.New("invalid_provider_credential")
var ErrOperation = errors.New("provider_operation_forbidden")
var ErrQuota = errors.New("provider_rate_limit")

type Access struct {
	Scope             provideraccess.Scope
	Operations        []string
	RequestsPerMinute int
}
type Credentials struct{ DB *pgxpool.Pool }

func newCredential() (uuid.UUID, string, []byte, error) {
	id := uuid.New()
	secret := make([]byte, 32)
	if _, e := rand.Read(secret); e != nil {
		return uuid.Nil, "", nil, e
	}
	token := "spk_" + id.String() + "." + base64.RawURLEncoding.EncodeToString(secret)
	digest := sha256.Sum256([]byte(token))
	return id, token, digest[:], nil
}

func (c Credentials) Authenticate(ctx context.Context, token string) (Access, error) {
	var out Access
	if len(token) != 84 || !strings.HasPrefix(token, "spk_") {
		return out, ErrCredential
	}
	key, secret, ok := strings.Cut(strings.TrimPrefix(token, "spk_"), ".")
	id, e := uuid.Parse(key)
	if !ok || e != nil || id == uuid.Nil {
		return out, ErrCredential
	}
	raw, e := base64.RawURLEncoding.DecodeString(secret)
	if e != nil || len(raw) != 32 {
		return out, ErrCredential
	}
	var stored []byte
	var client, namespace, revision uuid.UUID
	e = c.DB.QueryRow(ctx, `SELECT k.digest,c.id,c.namespace_id,c.revision,c.operations,c.requests_per_minute FROM provider_api.credentials k JOIN provider_api.clients c ON c.id=k.client_id WHERE k.id=$1 AND NOT k.revoked AND k.expires_at>now() AND c.enabled AND c.expires_at>now()`, id).Scan(&stored, &client, &namespace, &revision, &out.Operations, &out.RequestsPerMinute)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return Access{}, ErrCredential
		}
		return Access{}, e
	}
	sum := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(stored, sum[:]) != 1 {
		return Access{}, ErrCredential
	}
	rows, e := c.DB.Query(ctx, `SELECT CASE WHEN owner_scope='00000000-0000-0000-0000-000000000000' THEN 'global:' ELSE 'tenant:' END||dataset_id FROM provider_api.client_datasets WHERE client_id=$1 AND namespace_id=$2 ORDER BY owner_scope,dataset_id LIMIT 501`, client, namespace)
	if e != nil {
		return Access{}, e
	}
	defer rows.Close()
	refs := []string{}
	for rows.Next() {
		var ref string
		if e = rows.Scan(&ref); e != nil {
			return Access{}, e
		}
		refs = append(refs, ref)
	}
	if e = rows.Err(); e != nil {
		return Access{}, e
	}
	if len(refs) > 500 {
		return Access{}, ErrOperation
	}
	out.Scope, e = provideraccess.MachineClient(client, id, namespace, revision, refs)
	return out, e
}

// Consume uses a DB-atomic minute budget, shared across API replicas. Failed
// authenticated requests consume budget too; this is not a billing contract.
func (c Credentials) Consume(ctx context.Context, a Access, operation string) error {
	allowed := false
	for _, v := range a.Operations {
		allowed = allowed || v == operation
	}
	if !allowed {
		return ErrOperation
	}
	var count int
	e := c.DB.QueryRow(ctx, `INSERT INTO provider_api.usage_minutes(client_id,minute,requests) VALUES($1,date_trunc('minute',now()),1) ON CONFLICT(client_id,minute) DO UPDATE SET requests=provider_api.usage_minutes.requests+1 WHERE provider_api.usage_minutes.requests<$2 RETURNING requests`, a.Scope.Client(), a.RequestsPerMinute).Scan(&count)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrQuota
	}
	return e
}

type ClientInput struct {
	Name              string
	Namespace         uuid.UUID
	Operations        []string
	RequestsPerMinute int
	ExpiresAt         time.Time
}

func (c Credentials) Create(ctx context.Context, in ClientInput) (uuid.UUID, error) {
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 || in.Namespace == uuid.Nil || len(in.Operations) == 0 || in.RequestsPerMinute < 1 || in.RequestsPerMinute > 60000 || !in.ExpiresAt.After(time.Now()) || in.ExpiresAt.After(time.Now().Add(365*24*time.Hour)) {
		return uuid.Nil, sourcedata.ErrInput
	}
	seen := map[string]bool{}
	for _, op := range in.Operations {
		if seen[op] || (op != "search" && op != "enrich" && op != "catalog" && op != "freshness" && op != "delta") {
			return uuid.Nil, sourcedata.ErrInput
		}
		seen[op] = true
	}
	tx, e := c.DB.Begin(ctx)
	if e != nil {
		return uuid.Nil, e
	}
	defer tx.Rollback(ctx)
	var topology string
	if e = tx.QueryRow(ctx, `SELECT topology FROM provider_api.installation WHERE singleton`).Scan(&topology); e != nil {
		return uuid.Nil, e
	}
	if topology == "shared" {
		var active bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.tenants WHERE id=$1 AND status='active')`, in.Namespace).Scan(&active); e != nil || !active {
			return uuid.Nil, ErrOperation
		}
	}
	id := uuid.New()
	if _, e = tx.Exec(ctx, `INSERT INTO provider_api.clients(id,namespace_id,name,expires_at,operations,requests_per_minute) VALUES($1,$2,$3,$4,$5,$6)`, id, in.Namespace, in.Name, in.ExpiresAt, in.Operations, in.RequestsPerMinute); e != nil {
		return uuid.Nil, e
	}
	if e = audit(ctx, tx, id, "client-created", map[string]any{"operations": in.Operations, "requests_per_minute": in.RequestsPerMinute}); e != nil {
		return uuid.Nil, e
	}
	return id, tx.Commit(ctx)
}

// Issue persists a digest only. The callback writes the one-time credential to
// an explicitly chosen protected file; it must not log or print the token.
func (c Credentials) Issue(ctx context.Context, client uuid.UUID, ttl time.Duration, save func(string) error) (uuid.UUID, error) {
	if client == uuid.Nil || ttl < time.Minute || ttl > 365*24*time.Hour || save == nil {
		return uuid.Nil, sourcedata.ErrInput
	}
	id, token, digest, e := newCredential()
	if e != nil {
		return uuid.Nil, e
	}
	tx, e := c.DB.Begin(ctx)
	if e != nil {
		return uuid.Nil, e
	}
	defer tx.Rollback(ctx)
	var expires time.Time
	if e = tx.QueryRow(ctx, `SELECT least(expires_at,now()+make_interval(secs=>$2)) FROM provider_api.clients WHERE id=$1 AND enabled AND expires_at>now() FOR NO KEY UPDATE`, client, int(ttl.Seconds())).Scan(&expires); e != nil {
		return uuid.Nil, ErrCredential
	}
	if _, e = tx.Exec(ctx, `INSERT INTO provider_api.credentials(id,client_id,digest,expires_at) VALUES($1,$2,$3,$4)`, id, client, digest, expires); e != nil {
		return uuid.Nil, e
	}
	if e = audit(ctx, tx, client, "credential-issued", map[string]any{"credential_id": id, "expires_at": expires}); e != nil {
		return uuid.Nil, e
	}
	if e = save(token); e != nil {
		return uuid.Nil, e
	}
	return id, tx.Commit(ctx)
}

func (c Credentials) Grant(ctx context.Context, client uuid.UUID, ref sourcedata.DatasetRef, enabled bool) error {
	if client == uuid.Nil || !ref.Valid() {
		return sourcedata.ErrInput
	}
	tx, e := c.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var namespace uuid.UUID
	var topology string
	if e = tx.QueryRow(ctx, `SELECT c.namespace_id,i.topology FROM provider_api.clients c CROSS JOIN provider_api.installation i WHERE c.id=$1 AND i.singleton FOR NO KEY UPDATE OF c`, client).Scan(&namespace, &topology); e != nil {
		return ErrCredential
	}
	owner := uuid.Nil
	if ref.Scope == "tenant" {
		owner = namespace
	}
	if enabled && topology == "shared" {
		var allowed bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM screening.source_grants WHERE tenant_id=$1 AND owner_scope=$2 AND dataset_id=$3)`, namespace, owner, ref.ID).Scan(&allowed); e != nil || !allowed {
			return ErrOperation
		}
	}
	if enabled {
		_, e = tx.Exec(ctx, `INSERT INTO provider_api.client_datasets(client_id,namespace_id,owner_scope,dataset_id) VALUES($1,$2,$3,$4) ON CONFLICT(client_id,owner_scope,dataset_id) DO UPDATE SET revision=gen_random_uuid()`, client, namespace, owner, ref.ID)
	} else {
		_, e = tx.Exec(ctx, `DELETE FROM provider_api.client_datasets WHERE client_id=$1 AND owner_scope=$2 AND dataset_id=$3`, client, owner, ref.ID)
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE provider_api.clients SET revision=gen_random_uuid() WHERE id=$1`, client); e != nil {
		return e
	}
	if e = audit(ctx, tx, client, "dataset-access-changed", map[string]any{"dataset": ref, "enabled": enabled}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (c Credentials) Revoke(ctx context.Context, client, credential uuid.UUID) error {
	if client == uuid.Nil {
		return sourcedata.ErrInput
	}
	tx, e := c.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var found uuid.UUID
	if e = tx.QueryRow(ctx, `SELECT id FROM provider_api.clients WHERE id=$1 FOR NO KEY UPDATE`, client).Scan(&found); e != nil {
		return ErrCredential
	}
	if credential == uuid.Nil {
		_, e = tx.Exec(ctx, `UPDATE provider_api.clients SET enabled=false,revision=gen_random_uuid() WHERE id=$1`, client)
	} else {
		var key uuid.UUID
		e = tx.QueryRow(ctx, `UPDATE provider_api.credentials SET revoked=true WHERE id=$1 AND client_id=$2 RETURNING id`, credential, client).Scan(&key)
		if e == nil {
			_, e = tx.Exec(ctx, `UPDATE provider_api.clients SET revision=gen_random_uuid() WHERE id=$1`, client)
		}
	}
	if e != nil {
		return ErrCredential
	}
	if e = audit(ctx, tx, client, "access-revoked", map[string]any{"credential_id": credential}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func audit(ctx context.Context, tx pgx.Tx, client uuid.UUID, action string, detail any) error {
	raw, e := json.Marshal(detail)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO provider_api.admin_audit(client_id,actor,action,detail) VALUES($1,current_user,$2,$3)`, client, action, raw)
	return e
}
