// Package migrations preserves the data-model migration API and ledger lineage.
// Shared mechanics live in mysqlstore/migrations; an omitted owner remains data_model.
package migrations

import shared "github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"

type Step = shared.Step
type Plan = shared.Plan

func checksum(database string, s Step) [32]byte { return shared.Checksum(database, s) }
