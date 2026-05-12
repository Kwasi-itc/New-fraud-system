package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/app"
	"github.com/Kwasi-itc/marble-datamodel-service/internal/reconcile"
	storepostgres "github.com/Kwasi-itc/marble-datamodel-service/internal/store/postgres"
)

func main() {
	cfg, err := app.LoadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := storepostgres.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer db.Close()

	report, err := reconcile.NewService(db).Run(context.Background())
	if err != nil {
		log.Fatalf("run reconciliation: %v", err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		log.Fatalf("encode report: %v", err)
	}
}
