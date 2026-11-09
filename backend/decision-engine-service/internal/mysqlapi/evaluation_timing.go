package mysqlapi

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"sort"
	"strconv"
	"strings"
)

// Only an explicitly enabled request context emits timing diagnostics. A raw
// request header alone does not enable them in the production composition.
func evaluationServerTiming(ctx context.Context) string {
	t := ports.EvaluationTraceFromContext(ctx)
	if t == nil {
		return ""
	}
	stages := t.Snapshot()
	names := make([]string, 0, len(stages))
	for name := range stages {
		if len(name) == 0 || len(name) > 64 || strings.IndexFunc(name, func(r rune) bool { return r != '_' && (r < 'a' || r > 'z') && (r < '0' || r > '9') }) >= 0 {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+";dur="+strconv.FormatFloat(float64(stages[name].Microseconds())/1000, 'f', 3, 64))
	}
	return strings.Join(parts, ", ")
}
