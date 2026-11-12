package mysqlruntime

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
	"io"
	"os"
)

type Config struct{ Address, Database, AccountHost, Issuer, Audience, KeyID, Origin, CaseURL, IngestionURL string }

func Load(path string) (Config, error) {
	var c Config
	b, e := os.ReadFile(path)
	if e != nil {
		return c, e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return c, e
	}
	if d.Decode(new(any)) != io.EOF {
		return c, errors.New("one runtime configuration required")
	}
	validOrigin := c.Origin == "http://localhost:8085"
	if ec2test.Enabled() {
		_, err := ec2test.Origin(c.Origin)
		validOrigin = err == nil
	}
	if c.Address != "mysql:3306" || c.Database != "fraud" || c.AccountHost == "" || c.Issuer == "" || c.Audience == "" || c.KeyID == "" || !validOrigin || c.CaseURL != "http://case-manager-service:8086" || c.IngestionURL != "http://ingestion-service:8081" {
		return c, errors.New("prepared shared MySQL development configuration required")
	}
	return c, nil
}
func Secret(name string) (string, error) {
	switch name {
	case "screening_api_password", "screening_worker_password", "screening_service_token", "screening_operator_token", "screening_intake_token", "case_service_token", "ingestion_runtime_token":
	default:
		return "", errors.New("unknown screening secret")
	}
	b, e := os.ReadFile("/run/secrets/" + name)
	if e != nil {
		return "", e
	}
	decoded, e := hex.DecodeString(string(b))
	if e != nil || len(decoded) != 32 {
		return "", errors.New("invalid prepared screening secret")
	}
	return string(b), nil
}
