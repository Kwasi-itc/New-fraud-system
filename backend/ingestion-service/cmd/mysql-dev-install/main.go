package main

import (
	"context"
	"flag"
	"fmt"
	store "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"os"
	"time"
)

func main() {
	configureVelocity := flag.Bool("velocity", false, "rebuild the installed tenant's exact count/integer-sum projection from /runtime/velocity-definition.json")
	flag.Parse()
	if *configureVelocity {
		if err := os.Setenv("LOCAL_INGESTION_CONFIGURE_VELOCITY", "1"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	steps, err := store.InstallationStatements(localdev.Database)
	if err == nil {
		installation := localdev.Installation{Owner: "ingestion", Steps: steps, Upgrade: extendLocalOwnerPlan(steps)}
		if os.Getenv("LOCAL_INGESTION_API") == "1" {
			installation.Verify = configureLocalRuntime
		}
		err = localdev.Install(ctx, installation)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("ingestion local MySQL migrations applied")
}
