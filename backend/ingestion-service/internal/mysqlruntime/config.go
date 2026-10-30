package mysqlruntime

import (
	"encoding/json"
	"errors"
	store "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work"
	"github.com/google/uuid"
	"io"
	"net"
	"os"
	"regexp"
	"time"
)

// This configuration deliberately permits only disposable Compose databases.
// A production enablement requires independent deployment review and read-path
// adoption. No PostgreSQL connection or River client is constructed here.
type Config struct {
	Authority, Address, Database, User                                      string
	Token                                                                   string `json:"-"`
	TenantID                                                                uuid.UUID
	ObjectType                                                              string
	UploadQueue, DeferredQueue                                              string
	Pool, Concurrency, WriteConcurrency, Attempts, BodyBytes, ContractBytes int
	Limits                                                                  store.RecordOwnerLimits
	Lease, Timeout, Poll, Shutdown                                          time.Duration
	IngestionPlan, DataModelPlan                                            migrations.Plan
}

func Load(path string) (Config, error) {
	var c Config
	f, e := os.Open(path)
	if e != nil {
		return c, e
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 32<<20))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return c, e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, errors.New("single bounded configuration required")
	}
	c.Token = os.Getenv("INGESTION_RUNTIME_TOKEN")
	return c, c.Validate()
}
func (c Config) Validate() error {
	host, _, e := net.SplitHostPort(c.Address)
	if c.Authority != "isolated-mysql" || e != nil || (host != "mysql" && host != "127.0.0.1") || !regexp.MustCompile(`^ingestion_full_test_[a-f0-9]{32}$`).MatchString(c.Database) || c.User == "root" || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`).MatchString(c.User) || len(c.Token) < 16 || c.TenantID == uuid.Nil || c.ObjectType == "" {
		return errors.New("explicit isolated MySQL scope and credentials required")
	}
	if c.Pool < 4 || c.Pool > 64 || c.Concurrency < 1 || c.Concurrency > 16 || 4*c.Concurrency > c.Pool || c.WriteConcurrency < 1 || c.WriteConcurrency > 128 || c.Attempts < 1 || c.Attempts > 100 || c.BodyBytes < 1 || c.BodyBytes > 1<<20 || c.ContractBytes < 1 || c.ContractBytes > 16<<20 {
		return errors.New("bounded shared pool/request/worker policy required")
	}
	if !work.ValidQueueName(c.UploadQueue) || !work.ValidQueueName(c.DeferredQueue) || c.UploadQueue == c.DeferredQueue || c.Lease < 3*time.Second || c.Lease > 30*time.Minute || c.Timeout < time.Second || c.Timeout > c.Lease || c.Poll < 10*time.Millisecond || c.Poll > time.Minute || c.Shutdown < c.Timeout || c.Shutdown > time.Minute {
		return errors.New("complete queue timing policy required")
	}
	if c.Limits.CommandBytes < 1 || c.Limits.CommandBytes > 16<<20 || c.Limits.WriteBytes < 1 || c.Limits.WriteBytes > 16<<20 || c.Limits.EffectRowBytes < 1 || c.Limits.EffectRowBytes > 16<<20 || c.Limits.EffectBatchBytes < 1 || c.Limits.EffectBatchBytes > 16<<20 || c.Limits.Receipt.KeyBytes < 1 || c.Limits.Receipt.KeyBytes > 1<<20 || c.Limits.Receipt.ResultBytes < 1 || c.Limits.Receipt.ResultBytes > 16<<20 || c.Limits.Receipt.CollisionRows < 1 || c.Limits.Receipt.CollisionRows > 65535 {
		return errors.New("explicit receipt and effect limits required")
	}
	if c.IngestionPlan.Database != c.Database || c.DataModelPlan.Database != c.Database || c.DataModelPlan.Owner != "data_model" {
		return errors.New("one shared database and both installation artifacts required")
	}
	if _, e := store.InstallationPlan(c.Database, c.IngestionPlan.Steps); e != nil {
		return e
	}
	if e := c.DataModelPlan.Validate(); e != nil {
		return e
	}
	if len(c.DataModelPlan.Steps) == 0 || c.DataModelPlan.Steps[len(c.DataModelPlan.Steps)-1].ID != "data_model_v006_contract_lookup" {
		return errors.New("data-model v006 lineage required")
	}
	return nil
}
