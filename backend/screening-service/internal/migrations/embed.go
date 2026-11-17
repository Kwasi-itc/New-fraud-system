package migrations

import "embed"

// Metadata is used by the disposable source verifier and the curated standalone
// catalog bootstrap. Shared installation retains its tracked owner migrator.
//
//go:embed metadata/*.up.sql
var Metadata embed.FS
