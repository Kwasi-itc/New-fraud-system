package mysqlruntime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// LocalBlob persists real evidence bytes in screening-owned development storage.
// Short-lived random capabilities authorize one upload/download; no filesystem
// path or caller URL is interpreted as an object identity.
type LocalBlob struct {
	DB            *sql.DB
	table, origin string
}

func NewLocalBlob(db *sql.DB, database, origin string) (*LocalBlob, error) {
	schema, e := mysqlstore.Identifier(database)
	if e != nil {
		return nil, e
	}
	u, e := url.Parse(origin)
	if e != nil || u.Scheme != "http" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || db == nil {
		return nil, errors.New("explicit loopback development blob origin required")
	}
	return &LocalBlob{db, schema + ".screening_local_blobs", origin}, nil
}
func capability() (string, []byte, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", nil, e
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(token))
	return token, h[:], nil
}
func (b *LocalBlob) CreateUploadSession(ctx context.Context, t, s, name, mime string, size int64) (ports.BlobUploadSession, error) {
	if len(name) < 1 || len(name) > 255 || strings.ContainsAny(name, "/\\\x00") || size < 1 || size > 10<<20 {
		return ports.BlobUploadSession{}, errors.New("invalid local evidence metadata")
	}
	switch mime {
	case "application/pdf", "application/octet-stream", "text/plain", "image/png", "image/jpeg":
	default:
		return ports.BlobUploadSession{}, errors.New("unsupported local evidence MIME type")
	}
	tenant, e1 := uuid.Parse(t)
	screen, e2 := uuid.Parse(s)
	if e1 != nil || e2 != nil || tenant == uuid.Nil || screen == uuid.Nil {
		return ports.BlobUploadSession{}, errors.New("valid evidence tenant and screening required")
	}
	token, digest, e := capability()
	if e != nil {
		return ports.BlobUploadSession{}, e
	}
	key := uuid.New()
	_, e = b.DB.ExecContext(ctx, "INSERT INTO "+b.table+"(tenant_id,id,screening_id,file_name,content_type,file_size,upload_digest,upload_until,created_at) VALUES(?,?,?,?,?,?,?,TIMESTAMPADD(MINUTE,15,UTC_TIMESTAMP(6)),UTC_TIMESTAMP(6))", mysqlstore.UUID(tenant), mysqlstore.UUID(key), mysqlstore.UUID(screen), name, mime, size, digest)
	if e != nil {
		return ports.BlobUploadSession{}, errors.New("local evidence storage unavailable")
	}
	return ports.BlobUploadSession{StorageKey: "local-screening/" + key.String(), UploadURL: b.origin + "/local/evidence/" + token, Method: http.MethodPut}, nil
}
func (b *LocalBlob) GetDownloadURL(ctx context.Context, key string) (ports.BlobDownload, error) {
	raw, ok := strings.CutPrefix(key, "local-screening/")
	u, e := uuid.Parse(raw)
	if !ok || e != nil || u == uuid.Nil {
		return ports.BlobDownload{}, errors.New("unknown local evidence key")
	}
	token, digest, e := capability()
	if e != nil {
		return ports.BlobDownload{}, e
	}
	result, e := b.DB.ExecContext(ctx, "UPDATE "+b.table+" SET download_digest=?,download_until=TIMESTAMPADD(MINUTE,5,UTC_TIMESTAMP(6)) WHERE id=? AND content IS NOT NULL", digest, mysqlstore.UUID(u))
	if e != nil {
		return ports.BlobDownload{}, errors.New("local evidence storage unavailable")
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ports.BlobDownload{}, errors.New("evidence upload is not complete")
	}
	return ports.BlobDownload{DownloadURL: b.origin + "/local/evidence/" + token}, nil
}
func (b *LocalBlob) Mount(router *gin.Engine) {
	router.PUT("/local/evidence/:capability", func(c *gin.Context) {
		token := c.Param("capability")
		if len(token) != 43 {
			c.Status(404)
			return
		}
		digest := sha256.Sum256([]byte(token))
		var size int64
		var mime string
		if e := b.DB.QueryRowContext(c.Request.Context(), "SELECT file_size,content_type FROM "+b.table+" WHERE upload_digest=? AND upload_until>UTC_TIMESTAMP(6) AND content IS NULL", digest[:]).Scan(&size, &mime); e != nil {
			c.Status(404)
			return
		}
		if c.ContentType() != mime {
			c.JSON(415, gin.H{"error": "evidence content type mismatch"})
			return
		}
		content, e := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, size+1))
		if e != nil || int64(len(content)) != size {
			c.JSON(400, gin.H{"error": "evidence size mismatch"})
			return
		}
		hash := sha256.Sum256(content)
		result, e := b.DB.ExecContext(c.Request.Context(), "UPDATE "+b.table+" SET content=?,content_sha256=? WHERE upload_digest=? AND upload_until>UTC_TIMESTAMP(6) AND content IS NULL", content, hash[:], digest[:])
		if e != nil {
			c.Status(503)
			return
		}
		if n, _ := result.RowsAffected(); n != 1 {
			c.Status(409)
			return
		}
		c.Status(204)
	})
	router.GET("/local/evidence/:capability", func(c *gin.Context) {
		token := c.Param("capability")
		if len(token) != 43 {
			c.Status(404)
			return
		}
		digest := sha256.Sum256([]byte(token))
		var content []byte
		var mime string
		if e := b.DB.QueryRowContext(c.Request.Context(), "SELECT content,content_type FROM "+b.table+" WHERE download_digest=? AND download_until>UTC_TIMESTAMP(6) AND content IS NOT NULL", digest[:]).Scan(&content, &mime); e != nil {
			c.Status(404)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Data(200, mime, content)
	})
}
