package migrationaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var referencePattern = regexp.MustCompile(`(?i)\b(?:core|screening|case_manager|identity|public)\.[a-z_][a-z_0-9]*`)
var sqlPattern = regexp.MustCompile(`(?i)\b(?:select\s|insert\s+into|update\s+(?:core|screening)\.|delete\s+from|from\s+(?:core|screening|case_manager|identity|public)\.|join\s+|for\s+(?:update|share)|returning\b|order\s+by|limit\s+|where\s+)`)

func columnQueryIDs(queries []QueryContract, table, column string) []string {
	result := []string{}
	pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(column) + `\b`)
	for _, q := range queries {
		bound := false
		for _, ref := range q.References {
			if ref == table {
				bound = true
				break
			}
		}
		if !bound {
			continue
		}
		for _, fragment := range q.SQLFragments {
			if pattern.MatchString(fragment) {
				result = append(result, q.ID)
				break
			}
		}
	}
	return result
}

// Sources hashes fixed owner/dependency paths. Function contracts retain SQL
// fragments and unresolved dynamic shapes rather than claiming a SQL parser or
// complete call graph. Generated MySQL source and tests are tracked separately.
func Sources(root string) ([]SourceFile, []QueryContract, error) {
	patterns := []string{"internal/migrations/metadata/*.sql", "internal/store/postgres/*.go", "internal/ports/*.go", "internal/tenantdata/*.go", "internal/clients/datamodel/*.go", "internal/clients/ingestion/*.go", "internal/riverjobs/*.go", "internal/runtime/ast_eval/*.go", "internal/service/*.go", "internal/app/*.go", "internal/httpapi/investigator*.go", "cmd/migrate/*.go", "cmd/worker/*.go", "../investigator/*.go"}
	paths := map[string]bool{}
	for _, pattern := range patterns {
		files, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return nil, nil, err
		}
		for _, path := range files {
			if !strings.HasSuffix(path, "_test.go") {
				paths[path] = true
			}
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	files := make([]SourceFile, 0, len(ordered))
	queries := []QueryContract{}
	for _, path := range ordered {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		text := strings.ReplaceAll(string(b), "\r\n", "\n")
		hash := sha256.Sum256([]byte(text))
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil, nil, err
		}
		relative = filepath.ToSlash(relative)
		files = append(files, SourceFile{relative, hex.EncodeToString(hash[:])})
		if !strings.HasSuffix(path, ".go") {
			continue
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, text, 0)
		if err != nil {
			return nil, nil, err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			q := QueryContract{File: relative, Line: set.Position(fn.Pos()).Line, Contract: sourceContract(relative), Limitations: "Source-fragment/manual-contract inventory, not SQL parsing, data profiling, transitive call-graph analysis or measured index usage", Access: []string{}, References: []string{}, SQLFragments: []string{}}
			name := fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) > 0 {
				switch r := fn.Recv.List[0].Type.(type) {
				case *ast.Ident:
					name = r.Name + "." + name
				case *ast.StarExpr:
					if ident, ok := r.X.(*ast.Ident); ok {
						name = ident.Name + "." + name
					}
				}
			}
			q.ID = relative + "::" + name
			refs, access, sql := map[string]bool{}, map[string]bool{}, map[string]bool{}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
					value, err := strconv.Unquote(literal.Value)
					if err != nil {
						return true
					}
					for _, ref := range referencePattern.FindAllString(value, -1) {
						refs[strings.ToLower(ref)] = true
					}
					if sqlPattern.MatchString(value) {
						sql[value] = true
						upper := strings.ToUpper(value)
						for _, op := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
							if regexp.MustCompile(`\b` + op + `\b`).MatchString(upper) {
								access[op] = true
							}
						}
					}
				}
				if call, ok := node.(*ast.CallExpr); ok {
					if selector, ok := call.Fun.(*ast.SelectorExpr); ok && strings.HasPrefix(relative, "internal/riverjobs/") && (selector.Sel.Name == "InsertTx" || selector.Sel.Name == "Insert") {
						refs["public.river_job"] = true
						access["REQUIRED_ENQUEUE"] = true
					}
				}
				return true
			})
			if relative == "../investigator/review.go" && fn.Name.Name == "Run" {
				refs["core.investigator_reviews"] = true
				q.Limitations += "; dynamic owner.investigator_reviews resolves to core for decision review and screening for the other supported shared-library branch"
			}
			if strings.HasPrefix(relative, "internal/clients/datamodel/") && ast.IsExported(fn.Name.Name) {
				refs["data_model.HTTP_contract"] = true
				access["HTTP_CONTRACT"] = true
			}
			if strings.HasPrefix(relative, "internal/clients/ingestion/") && ast.IsExported(fn.Name.Name) {
				refs["ingestion.HTTP_contract"] = true
				access["HTTP_CONTRACT"] = true
			}
			if len(sql) == 0 && len(refs) == 0 && len(access) == 0 {
				continue
			}
			for value := range refs {
				q.References = append(q.References, value)
			}
			for value := range access {
				q.Access = append(q.Access, value)
			}
			for value := range sql {
				q.SQLFragments = append(q.SQLFragments, value)
			}
			sort.Strings(q.References)
			sort.Strings(q.Access)
			sort.Strings(q.SQLFragments)
			queries = append(queries, q)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	sort.Slice(queries, func(i, j int) bool { return queries[i].ID < queries[j].ID })
	return files, queries, nil
}

func sourceContract(path string) string {
	switch filepath.Base(path) {
	case "workflow_authoring.go":
		return "Current trusted identity authorization precedes resource locks; owner currently locks scenario parent then rule/inbox. Preserve optimistic timestamps, soft delete and authoring audit; review narrower lock scope, never copy broad serialization blindly. No network under locks."
	case "workflow_snapshot.go":
		return "One snapshot, 501-parent sentinel/500 cap, explicit parent and child order; replace ordered correlated JSON aggregates with bounded batched child assembly, no N+1."
	case "decision_repository.go":
		return "Tenant/scenario/score/outcome/exact record/prefix filters, typed dynamic SQL shape, count/list and stable created_at+UUID cursor; preserve wildcard/case/null rules and parameter bounds. Candidate indexes need actual predicate/order plans."
	case "tenant_data_reader.go":
		return "Registry/model-authorized tenant identifiers and fields, typed filter/group/operator/null/numeric/time contract, projections and bounded record/aggregate reads; no guessed MySQL schema, generic placeholder rewrite or broad in-memory fallback."
	case "async_decision_execution_repository.go", "scheduled_execution_repository.go":
		return "Domain status/attempt/idempotency/result/callback state; preserve locked claims, replay/conflict and conditional recovery. MySQL owner writes plus required delivery share caller transaction; retained River args need explicit retry/redrive identity review."
	case "jobs.go":
		return "Seven typed required jobs: validate owner tenant/resource on exact mutation transaction, then transport INSERT; missing producer fails. No queue bypass and no additional application database."
	case "queue_readiness.go":
		return "Pinned public River lineage, object/projection/index/role checks on actual runtime pool; read-only, bounded, no repair. MySQL needs its own verified queue capability contract."
	case "platform_repository.go":
		return "Exact tenant-scoped tags, lists, risk and IP text. Latest risk has explicit created_at/id order and driver-neutral confirmed absence; arbitrary failures cannot become no-risk. Only tags/risk/IP have staged MySQL implementations."
	case "outbox_event_repository.go":
		return "Optional integration publication state; disabling that integration does not disable workflow, screening, scoring or callback work. Required owner/job writes remain atomic when enabled."
	case "review.go", "identity.go", "investigator.go":
		return "Shared review/identity owner SQL on caller transaction: trusted actor, current permissions, stable operation/hash replay, immutable review and case history/outbox. Preserve owner boundaries, lock ordering and immediate constraint order; no arbitrary cross-owner writes."
	case "screening_repository.go":
		return "Decision-owned screening result/projection state and relationship validation; monotonic source reconciliation depends on screening-owned SQL/schema/lock contract."
	case "reader_factory.go":
		return "Reader authority must be explicit. Current source can fall back to HTTP on absent direct DB; target wiring must reject invalid/missing modes instead of silently changing authority."
	default:
		return "Preserve tenant/parent predicates, domain ordering and atomic mutation boundaries; replace RETURNING inside caller transaction, bound lists/parameters/projections, and preserve nullable/time/JSON/scalar scanner behavior. SQL fragments and catalog relations are review evidence, not an executable target adapter."
	}
}
