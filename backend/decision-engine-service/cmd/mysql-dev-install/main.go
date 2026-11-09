package main

import (
	"context"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"os"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	steps, err := metadata.DispatchStatements(localdev.Database, "local_decision_guard")
	if err == nil {
		var grants []string
		grants, err = metadata.DispatchDefinerGrants(localdev.Database, "local_decision_guard")
		if err == nil {
			installation := localdev.Installation{Owner: "decision_engine", Steps: steps, Accounts: []string{"local_decision_guard"}, Grants: grants}
			if os.Getenv("LOCAL_DECISION_API") == "1" {
				installation.Verify = configureLocal
			}
			err = localdev.Install(ctx, installation)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("decisions local MySQL migrations applied")
}
