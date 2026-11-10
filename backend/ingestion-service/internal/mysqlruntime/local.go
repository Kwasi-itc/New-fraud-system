package mysqlruntime

import (
	"encoding/hex"
	"errors"
	"os"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
)

const LocalAPIUser = "local_ingestion_api"
const LocalWorkerUser = "local_ingestion_worker"

// LocalSecret reads generated Docker secrets without exposing them in arguments
// or Compose environment output. Setup never replaces an existing secret.
func LocalSecret(name string) (string, error) {
	switch name {
	case "ingestion_api_password", "ingestion_worker_password", "ingestion_runtime_token":
	default:
		return "", errors.New("unknown ingestion local secret")
	}
	raw, err := os.ReadFile("/run/secrets/" + name)
	if err != nil {
		return "", err
	}
	decoded, err := hex.DecodeString(string(raw))
	if err != nil || len(decoded) != 32 {
		return "", errors.New("invalid generated ingestion local secret")
	}
	return string(raw), nil
}

func LoadLocal(worker bool) (Config, error) {
	var c Config
	if _, err := localdev.Origin(os.Getenv("LOCAL_MYSQL_NO_AUTH"), os.Getenv("APP_ORIGIN")); err != nil {
		return c, err
	}
	c, err := readConfig("/runtime/config.json")
	if err != nil {
		return c, err
	}
	if c.Authority != "local-mysql-development" || c.Database != localdev.Database {
		return c, errors.New("local MySQL configuration required")
	}
	c.User = LocalAPIUser
	c.LocalAllTenants = true
	if worker {
		c.User = LocalWorkerUser
	}
	c.Token, err = LocalSecret("ingestion_runtime_token")
	if err != nil {
		return c, err
	}
	return c, c.Validate()
}
