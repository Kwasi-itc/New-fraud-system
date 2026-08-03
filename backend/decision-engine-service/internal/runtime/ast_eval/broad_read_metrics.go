package ast_eval

import "sync"

type BroadReadHelperMetrics struct {
	RejectedCount          uint64            `json:"rejected_count"`
	RejectedByFunctionName map[string]uint64 `json:"rejected_by_function_name"`
}

var broadReadHelperMetricsMu sync.Mutex
var broadReadHelperRejectedByFunctionName = map[string]uint64{}

func recordBroadReadHelperRejection(functionName string) {
	broadReadHelperMetricsMu.Lock()
	defer broadReadHelperMetricsMu.Unlock()
	broadReadHelperRejectedByFunctionName[functionName]++
}

func BroadReadHelperMetricsSnapshot() BroadReadHelperMetrics {
	broadReadHelperMetricsMu.Lock()
	defer broadReadHelperMetricsMu.Unlock()

	snapshot := BroadReadHelperMetrics{
		RejectedByFunctionName: make(map[string]uint64, len(broadReadHelperRejectedByFunctionName)),
	}
	for key, value := range broadReadHelperRejectedByFunctionName {
		snapshot.RejectedCount += value
		snapshot.RejectedByFunctionName[key] = value
	}
	return snapshot
}
