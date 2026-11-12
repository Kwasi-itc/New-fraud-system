// Local operator maintenance only; never mounted as an HTTP mutation.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/mysqlauth"
	driver "github.com/go-sql-driver/mysql"
)

func run() error {
	from := flag.String("from", "", "exact previous local issuer URL")
	to := flag.String("to", "", "exact replacement local issuer URL")
	flag.Parse()
	if os.Getenv("AUTH_LOCAL_MYSQL") != "1" || os.Getenv("LOCAL_MYSQL_NO_AUTH") != "1" {
		return errors.New("explicit local operator runtime required")
	}
	password, err := os.ReadFile("/run/secrets/mysql_root_password")
	if err != nil {
		return errors.New("local root credential unavailable")
	}
	cfg := driver.NewConfig()
	cfg.Net, cfg.Addr, cfg.User, cfg.Passwd, cfg.DBName = "unix", "/target-socket/mysqld.sock", "root", string(password), "fraud"
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 30 * time.Second
	cfg.WriteTimeout = 30 * time.Second
	connector, err := driver.NewConnector(cfg)
	if err != nil {
		return err
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := mysqlauth.New(db, "fraud")
	if err != nil {
		return err
	}
	result, err := store.MoveLocalIssuer(ctx, *from, *to)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "local issuer move failed:", err)
		os.Exit(1)
	}
}
