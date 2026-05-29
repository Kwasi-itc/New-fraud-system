package httpapi

import (
	"sort"
	"sync"
	"time"
)

type serviceMetrics struct {
	mu             sync.RWMutex
	totalRequests  uint64
	totalLatencyMS float64
	byRoute        map[string]*routeMetric
}

type routeMetric struct {
	Method         string  `json:"method"`
	Route          string  `json:"route"`
	Count          uint64  `json:"count"`
	LastStatusCode int     `json:"last_status_code"`
	TotalLatencyMS float64 `json:"total_latency_ms"`
	AverageLatency float64 `json:"average_latency_ms"`
}

type metricsSnapshot struct {
	TotalRequests    uint64        `json:"total_requests"`
	AverageLatencyMS float64       `json:"average_latency_ms"`
	Routes           []routeMetric `json:"routes"`
}

func newServiceMetrics() *serviceMetrics {
	return &serviceMetrics{
		byRoute: make(map[string]*routeMetric),
	}
}

func (m *serviceMetrics) Record(method, route string, statusCode int, duration time.Duration) {
	latencyMS := float64(duration.Milliseconds())
	key := method + " " + route

	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalRequests++
	m.totalLatencyMS += latencyMS

	metric, ok := m.byRoute[key]
	if !ok {
		metric = &routeMetric{
			Method: method,
			Route:  route,
		}
		m.byRoute[key] = metric
	}
	metric.Count++
	metric.LastStatusCode = statusCode
	metric.TotalLatencyMS += latencyMS
	metric.AverageLatency = metric.TotalLatencyMS / float64(metric.Count)
}

func (m *serviceMetrics) Snapshot() metricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	routes := make([]routeMetric, 0, len(m.byRoute))
	for _, metric := range m.byRoute {
		routes = append(routes, *metric)
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Method == routes[j].Method {
			return routes[i].Route < routes[j].Route
		}
		return routes[i].Method < routes[j].Method
	})

	snapshot := metricsSnapshot{
		TotalRequests: m.totalRequests,
		Routes:        routes,
	}
	if m.totalRequests > 0 {
		snapshot.AverageLatencyMS = m.totalLatencyMS / float64(m.totalRequests)
	}
	return snapshot
}
