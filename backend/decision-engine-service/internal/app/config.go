package app

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                                string
	DatabaseURL                         string
	DataModelServiceURL                 string
	IngestionServiceURL                 string
	ServiceAuthMode                     string
	ServiceAuthToken                    string
	ServiceAllowedOrigins               []string
	WorkflowActionURL                   string
	ScreeningServiceURL                 string
	ScreeningProviderURL                string
	ScoringProviderURL                  string
	OutboxPublisherURL                  string
	LogLevel                            string
	GinMode                             string
	HTTPClientTimeout                   time.Duration
	AggregatePushdownMode               string
	AggregatePushdownAggregates         []string
	LiveDecisionConcurrencyLimit        int
	LiveAsyncFallbackEnabled            bool
	RuleEvaluationConcurrency           int
	ScenarioEvaluationConcurrency       int
	WorkerMode                          string
	WorkerTasks                         []string
	WorkerTaskPriorities                map[string]int
	ScheduledExecutionMaxAttempts       int
	ScheduledExecutionRetryBackoff      time.Duration
	ScheduledExecutionQueueName         string
	ScheduledExecutionQueueWorkers      int
	AsyncExecutionMaxAttempts           int
	AsyncExecutionRetryBackoff          time.Duration
	AsyncExecutionDefaultWaitWindow     time.Duration
	AsyncExecutionMaxWaitWindow         time.Duration
	AsyncExecutionCallbackTimeout       time.Duration
	AsyncExecutionCallbackMaxAttempts   int
	AsyncExecutionQueueName             string
	AsyncExecutionQueueWorkers          int
	AsyncExecutionCallbackQueueName     string
	AsyncExecutionCallbackQueueWorkers  int
	AsyncExecutionCallbackSigningSecret string
	WorkflowDispatchQueueName           string
	WorkflowDispatchQueueWorkers        int
	ScreeningDispatchQueueName          string
	ScreeningDispatchQueueWorkers       int
	ScoringDispatchQueueName            string
	ScoringDispatchQueueWorkers         int
	OutboxQueueName                     string
	OutboxQueueWorkers                  int
	WorkerPollInterval                  time.Duration
	WorkerBatchLimit                    int
}

const maxRuleEvaluationConcurrency = 64
const maxScenarioEvaluationConcurrency = 32

var supportedWorkerTasks = map[string]struct{}{
	"scheduled":          {},
	"async":              {},
	"workflow_dispatch":  {},
	"screening_dispatch": {},
	"scoring_dispatch":   {},
	"outbox":             {},
}

var defaultWorkerTaskPriorities = map[string]int{
	"async":              10,
	"scheduled":          20,
	"workflow_dispatch":  30,
	"screening_dispatch": 40,
	"scoring_dispatch":   50,
	"outbox":             60,
}

func LoadConfig() (Config, error) {
	loadDotEnvIfPresent()

	httpClientTimeout, err := getEnvDuration("HTTP_CLIENT_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	workerPollInterval, err := getEnvDuration("WORKER_POLL_INTERVAL", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	scheduledExecutionRetryBackoff, err := getEnvDuration("SCHEDULED_EXECUTION_RETRY_BACKOFF", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	asyncExecutionRetryBackoff, err := getEnvDuration("ASYNC_EXECUTION_RETRY_BACKOFF", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	asyncExecutionDefaultWaitWindow, err := getEnvDuration("ASYNC_EXECUTION_DEFAULT_WAIT_WINDOW", 300*time.Millisecond)
	if err != nil {
		return Config{}, err
	}
	asyncExecutionMaxWaitWindow, err := getEnvDuration("ASYNC_EXECUTION_MAX_WAIT_WINDOW", time.Second)
	if err != nil {
		return Config{}, err
	}
	asyncExecutionCallbackTimeout, err := getEnvDuration("ASYNC_EXECUTION_CALLBACK_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	asyncExecutionCallbackMaxAttempts, err := getEnvInt("ASYNC_EXECUTION_CALLBACK_MAX_ATTEMPTS", 5)
	if err != nil {
		return Config{}, err
	}
	workerBatchLimit, err := getEnvInt("WORKER_BATCH_LIMIT", 100)
	if err != nil {
		return Config{}, err
	}
	scheduledExecutionMaxAttempts, err := getEnvInt("SCHEDULED_EXECUTION_MAX_ATTEMPTS", 3)
	if err != nil {
		return Config{}, err
	}
	asyncExecutionMaxAttempts, err := getEnvInt("ASYNC_EXECUTION_MAX_ATTEMPTS", 3)
	if err != nil {
		return Config{}, err
	}
	scheduledExecutionQueueWorkers, err := getEnvInt("SCHEDULED_EXECUTION_QUEUE_WORKERS", 4)
	if err != nil {
		return Config{}, err
	}
	asyncExecutionQueueWorkers, err := getEnvInt("ASYNC_EXECUTION_QUEUE_WORKERS", 4)
	if err != nil {
		return Config{}, err
	}
	asyncExecutionCallbackQueueWorkers, err := getEnvInt("ASYNC_EXECUTION_CALLBACK_QUEUE_WORKERS", 2)
	if err != nil {
		return Config{}, err
	}
	workflowDispatchQueueWorkers, err := getEnvInt("WORKFLOW_DISPATCH_QUEUE_WORKERS", 4)
	if err != nil {
		return Config{}, err
	}
	screeningDispatchQueueWorkers, err := getEnvInt("SCREENING_DISPATCH_QUEUE_WORKERS", 4)
	if err != nil {
		return Config{}, err
	}
	scoringDispatchQueueWorkers, err := getEnvInt("SCORING_DISPATCH_QUEUE_WORKERS", 4)
	if err != nil {
		return Config{}, err
	}
	outboxQueueWorkers, err := getEnvInt("OUTBOX_QUEUE_WORKERS", 4)
	if err != nil {
		return Config{}, err
	}
	liveDecisionConcurrencyLimit, err := getEnvInt("LIVE_DECISION_CONCURRENCY_LIMIT", 0)
	if err != nil {
		return Config{}, err
	}
	liveAsyncFallbackEnabled, err := getEnvBool("LIVE_ASYNC_FALLBACK_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	workerTasks, err := parseWorkerTasksEnv("WORKER_TASKS")
	if err != nil {
		return Config{}, err
	}
	workerTaskPriorities, err := parseWorkerTaskPrioritiesEnv("WORKER_TASK_PRIORITIES", workerTasks)
	if err != nil {
		return Config{}, err
	}
	ruleEvaluationConcurrency, err := getEnvInt("RULE_EVALUATION_CONCURRENCY", 0)
	if err != nil {
		return Config{}, err
	}
	scenarioEvaluationConcurrency, err := getEnvInt("SCENARIO_EVALUATION_CONCURRENCY", 0)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Port:                                getEnv("PORT", "8082"),
		DatabaseURL:                         os.Getenv("DATABASE_URL"),
		DataModelServiceURL:                 strings.TrimRight(os.Getenv("DATA_MODEL_SERVICE_URL"), "/"),
		IngestionServiceURL:                 strings.TrimRight(os.Getenv("INGESTION_SERVICE_URL"), "/"),
		ServiceAuthMode:                     getEnv("SERVICE_AUTH_MODE", "disabled"),
		ServiceAuthToken:                    os.Getenv("SERVICE_AUTH_TOKEN"),
		ServiceAllowedOrigins:               parseCSVEnv("SERVICE_ALLOWED_ORIGINS", []string{"http://localhost:3000"}),
		WorkflowActionURL:                   strings.TrimRight(os.Getenv("WORKFLOW_ACTION_URL"), "/"),
		ScreeningServiceURL:                 strings.TrimRight(os.Getenv("SCREENING_SERVICE_URL"), "/"),
		ScreeningProviderURL:                strings.TrimRight(os.Getenv("SCREENING_PROVIDER_URL"), "/"),
		ScoringProviderURL:                  strings.TrimRight(os.Getenv("SCORING_PROVIDER_URL"), "/"),
		OutboxPublisherURL:                  strings.TrimRight(os.Getenv("OUTBOX_PUBLISHER_URL"), "/"),
		LogLevel:                            getEnv("LOG_LEVEL", "debug"),
		GinMode:                             getEnv("GIN_MODE", "debug"),
		HTTPClientTimeout:                   httpClientTimeout,
		AggregatePushdownMode:               strings.ToLower(getEnv("AGGREGATE_PUSHDOWN_MODE", "enabled")),
		AggregatePushdownAggregates:         parseCSVEnv("AGGREGATE_PUSHDOWN_AGGREGATES", []string{"count"}),
		LiveDecisionConcurrencyLimit:        liveDecisionConcurrencyLimit,
		LiveAsyncFallbackEnabled:            liveAsyncFallbackEnabled,
		RuleEvaluationConcurrency:           ruleEvaluationConcurrency,
		ScenarioEvaluationConcurrency:       scenarioEvaluationConcurrency,
		WorkerMode:                          strings.ToLower(getEnv("WORKER_MODE", "batch")),
		WorkerTasks:                         workerTasks,
		WorkerTaskPriorities:                workerTaskPriorities,
		ScheduledExecutionMaxAttempts:       scheduledExecutionMaxAttempts,
		ScheduledExecutionRetryBackoff:      scheduledExecutionRetryBackoff,
		ScheduledExecutionQueueName:         getEnv("SCHEDULED_EXECUTION_QUEUE_NAME", "scheduled_executions"),
		ScheduledExecutionQueueWorkers:      scheduledExecutionQueueWorkers,
		AsyncExecutionMaxAttempts:           asyncExecutionMaxAttempts,
		AsyncExecutionRetryBackoff:          asyncExecutionRetryBackoff,
		AsyncExecutionDefaultWaitWindow:     asyncExecutionDefaultWaitWindow,
		AsyncExecutionMaxWaitWindow:         asyncExecutionMaxWaitWindow,
		AsyncExecutionCallbackTimeout:       asyncExecutionCallbackTimeout,
		AsyncExecutionCallbackMaxAttempts:   asyncExecutionCallbackMaxAttempts,
		AsyncExecutionQueueName:             getEnv("ASYNC_EXECUTION_QUEUE_NAME", "async_decision_executions"),
		AsyncExecutionQueueWorkers:          asyncExecutionQueueWorkers,
		AsyncExecutionCallbackQueueName:     getEnv("ASYNC_EXECUTION_CALLBACK_QUEUE_NAME", "async_decision_execution_callbacks"),
		AsyncExecutionCallbackQueueWorkers:  asyncExecutionCallbackQueueWorkers,
		AsyncExecutionCallbackSigningSecret: os.Getenv("ASYNC_EXECUTION_CALLBACK_SIGNING_SECRET"),
		WorkflowDispatchQueueName:           getEnv("WORKFLOW_DISPATCH_QUEUE_NAME", "workflow_executions"),
		WorkflowDispatchQueueWorkers:        workflowDispatchQueueWorkers,
		ScreeningDispatchQueueName:          getEnv("SCREENING_DISPATCH_QUEUE_NAME", "screening_executions"),
		ScreeningDispatchQueueWorkers:       screeningDispatchQueueWorkers,
		ScoringDispatchQueueName:            getEnv("SCORING_DISPATCH_QUEUE_NAME", "scoring_requests"),
		ScoringDispatchQueueWorkers:         scoringDispatchQueueWorkers,
		OutboxQueueName:                     getEnv("OUTBOX_QUEUE_NAME", "outbox_events"),
		OutboxQueueWorkers:                  outboxQueueWorkers,
		WorkerPollInterval:                  workerPollInterval,
		WorkerBatchLimit:                    workerBatchLimit,
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.DataModelServiceURL == "" {
		return Config{}, fmt.Errorf("DATA_MODEL_SERVICE_URL is required")
	}
	if cfg.IngestionServiceURL == "" {
		return Config{}, fmt.Errorf("INGESTION_SERVICE_URL is required")
	}
	if cfg.ServiceAuthMode == "token" && cfg.ServiceAuthToken == "" {
		return Config{}, fmt.Errorf("SERVICE_AUTH_TOKEN is required when SERVICE_AUTH_MODE=token")
	}
	if cfg.WorkerMode != "batch" && cfg.WorkerMode != "poll" {
		return Config{}, fmt.Errorf("WORKER_MODE must be either batch or poll")
	}
	if len(cfg.WorkerTasks) == 0 {
		return Config{}, fmt.Errorf("WORKER_TASKS must include at least one supported task")
	}
	if cfg.AggregatePushdownMode != "enabled" && cfg.AggregatePushdownMode != "disabled" && cfg.AggregatePushdownMode != "strict" {
		return Config{}, fmt.Errorf("AGGREGATE_PUSHDOWN_MODE must be one of enabled, disabled, or strict")
	}
	if cfg.WorkerPollInterval <= 0 {
		return Config{}, fmt.Errorf("WORKER_POLL_INTERVAL must be greater than zero")
	}
	if cfg.WorkerBatchLimit <= 0 {
		return Config{}, fmt.Errorf("WORKER_BATCH_LIMIT must be greater than zero")
	}
	if cfg.LiveDecisionConcurrencyLimit < 0 {
		return Config{}, fmt.Errorf("LIVE_DECISION_CONCURRENCY_LIMIT must be greater than or equal to zero")
	}
	if cfg.ScheduledExecutionMaxAttempts <= 0 {
		return Config{}, fmt.Errorf("SCHEDULED_EXECUTION_MAX_ATTEMPTS must be greater than zero")
	}
	if cfg.AsyncExecutionMaxAttempts <= 0 {
		return Config{}, fmt.Errorf("ASYNC_EXECUTION_MAX_ATTEMPTS must be greater than zero")
	}
	if cfg.ScheduledExecutionRetryBackoff <= 0 {
		return Config{}, fmt.Errorf("SCHEDULED_EXECUTION_RETRY_BACKOFF must be greater than zero")
	}
	if cfg.AsyncExecutionRetryBackoff <= 0 {
		return Config{}, fmt.Errorf("ASYNC_EXECUTION_RETRY_BACKOFF must be greater than zero")
	}
	if cfg.AsyncExecutionDefaultWaitWindow < 0 {
		return Config{}, fmt.Errorf("ASYNC_EXECUTION_DEFAULT_WAIT_WINDOW must be greater than or equal to zero")
	}
	if cfg.AsyncExecutionMaxWaitWindow <= 0 {
		return Config{}, fmt.Errorf("ASYNC_EXECUTION_MAX_WAIT_WINDOW must be greater than zero")
	}
	if cfg.AsyncExecutionCallbackTimeout <= 0 {
		return Config{}, fmt.Errorf("ASYNC_EXECUTION_CALLBACK_TIMEOUT must be greater than zero")
	}
	if cfg.AsyncExecutionCallbackMaxAttempts <= 0 {
		return Config{}, fmt.Errorf("ASYNC_EXECUTION_CALLBACK_MAX_ATTEMPTS must be greater than zero")
	}
	if cfg.ScheduledExecutionQueueWorkers <= 0 {
		return Config{}, fmt.Errorf("SCHEDULED_EXECUTION_QUEUE_WORKERS must be greater than zero")
	}
	if cfg.AsyncExecutionQueueWorkers <= 0 {
		return Config{}, fmt.Errorf("ASYNC_EXECUTION_QUEUE_WORKERS must be greater than zero")
	}
	if cfg.AsyncExecutionCallbackQueueWorkers <= 0 {
		return Config{}, fmt.Errorf("ASYNC_EXECUTION_CALLBACK_QUEUE_WORKERS must be greater than zero")
	}
	if strings.TrimSpace(cfg.ScheduledExecutionQueueName) == "" {
		return Config{}, fmt.Errorf("SCHEDULED_EXECUTION_QUEUE_NAME must not be empty")
	}
	if strings.TrimSpace(cfg.AsyncExecutionQueueName) == "" {
		return Config{}, fmt.Errorf("ASYNC_EXECUTION_QUEUE_NAME must not be empty")
	}
	if strings.TrimSpace(cfg.AsyncExecutionCallbackQueueName) == "" {
		return Config{}, fmt.Errorf("ASYNC_EXECUTION_CALLBACK_QUEUE_NAME must not be empty")
	}
	if cfg.WorkflowDispatchQueueWorkers <= 0 {
		return Config{}, fmt.Errorf("WORKFLOW_DISPATCH_QUEUE_WORKERS must be greater than zero")
	}
	if cfg.ScreeningDispatchQueueWorkers <= 0 {
		return Config{}, fmt.Errorf("SCREENING_DISPATCH_QUEUE_WORKERS must be greater than zero")
	}
	if cfg.ScoringDispatchQueueWorkers <= 0 {
		return Config{}, fmt.Errorf("SCORING_DISPATCH_QUEUE_WORKERS must be greater than zero")
	}
	if cfg.OutboxQueueWorkers <= 0 {
		return Config{}, fmt.Errorf("OUTBOX_QUEUE_WORKERS must be greater than zero")
	}
	if strings.TrimSpace(cfg.WorkflowDispatchQueueName) == "" {
		return Config{}, fmt.Errorf("WORKFLOW_DISPATCH_QUEUE_NAME must not be empty")
	}
	if strings.TrimSpace(cfg.ScreeningDispatchQueueName) == "" {
		return Config{}, fmt.Errorf("SCREENING_DISPATCH_QUEUE_NAME must not be empty")
	}
	if strings.TrimSpace(cfg.ScoringDispatchQueueName) == "" {
		return Config{}, fmt.Errorf("SCORING_DISPATCH_QUEUE_NAME must not be empty")
	}
	if strings.TrimSpace(cfg.OutboxQueueName) == "" {
		return Config{}, fmt.Errorf("OUTBOX_QUEUE_NAME must not be empty")
	}
	if cfg.RuleEvaluationConcurrency < 0 {
		return Config{}, fmt.Errorf("RULE_EVALUATION_CONCURRENCY must be greater than or equal to zero")
	}
	if cfg.RuleEvaluationConcurrency > maxRuleEvaluationConcurrency {
		return Config{}, fmt.Errorf("RULE_EVALUATION_CONCURRENCY must be less than or equal to %d", maxRuleEvaluationConcurrency)
	}
	if cfg.ScenarioEvaluationConcurrency < 0 {
		return Config{}, fmt.Errorf("SCENARIO_EVALUATION_CONCURRENCY must be greater than or equal to zero")
	}
	if cfg.ScenarioEvaluationConcurrency > maxScenarioEvaluationConcurrency {
		return Config{}, fmt.Errorf("SCENARIO_EVALUATION_CONCURRENCY must be less than or equal to %d", maxScenarioEvaluationConcurrency)
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getEnvDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", key, err)
	}
	return parsed, nil
}

func getEnvInt(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid integer: %w", key, err)
	}
	return parsed, nil
}

func getEnvBool(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback, nil
	}
	switch value {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a valid boolean", key)
	}
}

func parseCSVEnv(key string, fallback []string) []string {
	value := os.Getenv(key)
	if strings.TrimSpace(value) == "" {
		return append([]string(nil), fallback...)
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), fallback...)
	}
	return out
}

func parseWorkerTasksEnv(key string) ([]string, error) {
	return parseSupportedCSVEnv(key, []string{
		"scheduled",
		"async",
		"workflow_dispatch",
		"screening_dispatch",
		"scoring_dispatch",
		"outbox",
	}, supportedWorkerTasks)
}

func parseWorkerTaskPrioritiesEnv(key string, enabledTasks []string) (map[string]int, error) {
	priorities := make(map[string]int, len(enabledTasks))
	for _, task := range enabledTasks {
		priorities[task] = defaultWorkerTaskPriorities[task]
	}

	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return priorities, nil
	}

	assignments := strings.Split(value, ",")
	for _, assignment := range assignments {
		assignment = strings.TrimSpace(assignment)
		if assignment == "" {
			continue
		}
		task, priorityValue, ok := strings.Cut(assignment, ":")
		if !ok {
			return nil, fmt.Errorf("%s must use task:priority entries", key)
		}
		task = strings.ToLower(strings.TrimSpace(task))
		if _, ok := supportedWorkerTasks[task]; !ok {
			return nil, fmt.Errorf("%s contains unsupported task %q", key, task)
		}
		priorityValue = strings.TrimSpace(priorityValue)
		priority, err := strconv.Atoi(priorityValue)
		if err != nil {
			return nil, fmt.Errorf("%s contains invalid priority for %q: %w", key, task, err)
		}
		if _, enabled := priorities[task]; enabled {
			priorities[task] = priority
		}
	}
	return priorities, nil
}

func SortedWorkerTasks(tasks []string, priorities map[string]int) []string {
	out := append([]string(nil), tasks...)
	sort.SliceStable(out, func(i, j int) bool {
		leftPriority := priorities[out[i]]
		rightPriority := priorities[out[j]]
		if leftPriority == rightPriority {
			return out[i] < out[j]
		}
		return leftPriority < rightPriority
	})
	return out
}

func parseSupportedCSVEnv(key string, fallback []string, supported map[string]struct{}) ([]string, error) {
	value := os.Getenv(key)
	if strings.TrimSpace(value) == "" {
		return append([]string(nil), fallback...), nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		if _, ok := supported[part]; !ok {
			return nil, fmt.Errorf("%s contains unsupported task %q", key, part)
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func loadDotEnvIfPresent() {
	content, err := os.ReadFile(".env")
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		if key == "" || os.Getenv(key) != "" {
			continue
		}

		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		_ = os.Setenv(key, value)
	}
}
