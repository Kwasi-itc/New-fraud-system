package redisfacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

const (
	markerTTL              = 7 * 24 * time.Hour
	factWriteLease         = 60 * time.Second
	factWriteChunkSize     = 256
	factLatencySampleLimit = 1024
)

type Writer struct {
	client      *redis.Client
	db          *pgxpool.Pool
	maintenance sync.RWMutex
	logger      *slog.Logger
	metrics     aggregateFactWriteMetricState
	latencies   aggregateFactWriteLatencyState
}

func (w *Writer) SetLogger(logger *slog.Logger) {
	w.logger = logger
}

type AggregateFactWriteMetrics struct {
	PrepareRequests          uint64                        `json:"prepare_requests"`
	PrepareFailures          uint64                        `json:"prepare_failures"`
	PreparedWrites           uint64                        `json:"prepared_writes"`
	NoActiveFactWrites       uint64                        `json:"no_active_fact_writes"`
	AlreadyAppliedWrites     uint64                        `json:"already_applied_writes"`
	RecordsEvaluated         uint64                        `json:"records_evaluated"`
	DefinitionsEvaluated     uint64                        `json:"definitions_evaluated"`
	CommitRequests           uint64                        `json:"commit_requests"`
	CommitFailures           uint64                        `json:"commit_failures"`
	CommittedWrites          uint64                        `json:"committed_writes"`
	GroupedDeltasCommitted   uint64                        `json:"grouped_deltas_committed"`
	PrepareChunks            uint64                        `json:"prepare_chunks"`
	CommitChunks             uint64                        `json:"commit_chunks"`
	FinalizeChunks           uint64                        `json:"finalize_chunks"`
	RecoveredWrites          uint64                        `json:"recovered_writes"`
	CommitLatencyTotalMicros uint64                        `json:"commit_latency_total_micros"`
	AbortedWrites            uint64                        `json:"aborted_writes"`
	PrepareLatency           AggregateFactOperationLatency `json:"prepare_latency"`
	CommitLatency            AggregateFactOperationLatency `json:"commit_latency"`
	ValkeyReads              AggregateFactOperationLatency `json:"valkey_reads"`
	ValkeyWrites             AggregateFactOperationLatency `json:"valkey_writes"`
	BucketApplyWrites        AggregateFactOperationLatency `json:"bucket_apply_writes"`
}

type AggregateFactOperationLatency struct {
	Requests      uint64 `json:"requests"`
	Failures      uint64 `json:"failures"`
	TotalMicros   uint64 `json:"total_micros"`
	AverageMicros uint64 `json:"average_micros"`
	P50Micros     uint64 `json:"p50_micros"`
	P95Micros     uint64 `json:"p95_micros"`
	P99Micros     uint64 `json:"p99_micros"`
	MaxMicros     uint64 `json:"max_micros"`
}

type aggregateFactWriteMetricState struct {
	prepareRequests          atomic.Uint64
	prepareFailures          atomic.Uint64
	preparedWrites           atomic.Uint64
	noActiveFactWrites       atomic.Uint64
	alreadyAppliedWrites     atomic.Uint64
	recordsEvaluated         atomic.Uint64
	definitionsEvaluated     atomic.Uint64
	commitRequests           atomic.Uint64
	commitFailures           atomic.Uint64
	committedWrites          atomic.Uint64
	groupedDeltasCommitted   atomic.Uint64
	prepareChunks            atomic.Uint64
	commitChunks             atomic.Uint64
	finalizeChunks           atomic.Uint64
	recoveredWrites          atomic.Uint64
	commitLatencyTotalMicros atomic.Uint64
	abortedWrites            atomic.Uint64
}

type aggregateFactWriteLatencyState struct {
	prepare      aggregateFactOperationLatencyState
	commit       aggregateFactOperationLatencyState
	valkeyReads  aggregateFactOperationLatencyState
	valkeyWrites aggregateFactOperationLatencyState
	bucketApply  aggregateFactOperationLatencyState
}

type aggregateFactOperationLatencyState struct {
	requests    atomic.Uint64
	failures    atomic.Uint64
	totalMicros atomic.Uint64
	maxMicros   atomic.Uint64
	mu          sync.Mutex
	samples     []uint64
}

func (s *aggregateFactOperationLatencyState) record(duration time.Duration, err error) {
	micros := uint64(max(duration.Microseconds(), 0))
	s.requests.Add(1)
	if err != nil {
		s.failures.Add(1)
	}
	s.totalMicros.Add(micros)
	for current := s.maxMicros.Load(); micros > current && !s.maxMicros.CompareAndSwap(current, micros); current = s.maxMicros.Load() {
	}
	s.mu.Lock()
	if len(s.samples) >= factLatencySampleLimit {
		copy(s.samples, s.samples[1:])
		s.samples[len(s.samples)-1] = micros
	} else {
		s.samples = append(s.samples, micros)
	}
	s.mu.Unlock()
}

func (s *aggregateFactOperationLatencyState) snapshot() AggregateFactOperationLatency {
	requests := s.requests.Load()
	total := s.totalMicros.Load()
	s.mu.Lock()
	samples := append([]uint64(nil), s.samples...)
	s.mu.Unlock()
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	average := uint64(0)
	if requests > 0 {
		average = total / requests
	}
	percentile := func(value int) uint64 {
		if len(samples) == 0 {
			return 0
		}
		return samples[((len(samples)-1)*value)/100]
	}
	return AggregateFactOperationLatency{
		Requests: requests, Failures: s.failures.Load(), TotalMicros: total, AverageMicros: average,
		P50Micros: percentile(50), P95Micros: percentile(95), P99Micros: percentile(99), MaxMicros: s.maxMicros.Load(),
	}
}

type factDelta struct {
	key       string
	dimension string
	sum       float64
	count     int64
	expiresAt time.Time
}

var beginScript = redis.NewScript(`
local marker = KEYS[1]
local manifest = ARGV[1]
local owner = ARGV[2]
local now_ms = tonumber(ARGV[3])
local lease_until_ms = ARGV[4]
local chunk_count = tonumber(ARGV[5])
local marker_ttl = tonumber(ARGV[6])
if redis.call('EXISTS', marker) == 0 then
  local state = 'preparing'
  if chunk_count == 0 then state = 'prepared' end
  redis.call('HSET', marker,
    'state', state, 'manifest', manifest, 'owner', owner,
    'lease_until_ms', lease_until_ms, 'chunk_count', chunk_count,
    'prepared_count', 0, 'applied_count', 0, 'finalized_count', 0)
  redis.call('EXPIRE', marker, marker_ttl)
  return 1
end
if redis.call('TYPE', marker).ok ~= 'hash' then
  return redis.error_reply('aggregate fact marker has an incompatible format')
end
local state = redis.call('HGET', marker, 'state')
if state == 'applied' then return 2 end
if redis.call('HGET', marker, 'manifest') ~= manifest then
  return redis.error_reply('aggregate fact manifest mismatch')
end
local current_owner = redis.call('HGET', marker, 'owner') or ''
local lease_until = tonumber(redis.call('HGET', marker, 'lease_until_ms') or '0')
if current_owner == owner or lease_until < now_ms then
  redis.call('HSET', marker, 'owner', owner, 'lease_until_ms', lease_until_ms)
  redis.call('EXPIRE', marker, marker_ttl)
  return 1
end
return 3
`)

var prepareChunkScript = redis.NewScript(`
local marker = KEYS[1]
local owner = ARGV[1]
local chunk = ARGV[2]
local lease_until_ms = ARGV[3]
local item_count = tonumber(ARGV[4])
local state = redis.call('HGET', marker, 'state')
if redis.call('HGET', marker, 'owner') ~= owner then return redis.error_reply('aggregate fact lease was lost') end
local chunk_field = 'prepared:' .. chunk
if redis.call('HEXISTS', marker, chunk_field) == 1 then
  redis.call('HSET', marker, 'lease_until_ms', lease_until_ms)
  return 0
end
if state ~= 'preparing' and state ~= 'prepared' then return redis.error_reply('aggregate fact write cannot be prepared in its current state') end
local arg = 5
for i = 1, item_count do
  redis.call('HINCRBY', KEYS[1 + i], ARGV[arg], 1)
  redis.call('EXPIREAT', KEYS[1 + i], ARGV[arg + 1])
  arg = arg + 2
end
redis.call('HSET', marker, chunk_field, 1, 'lease_until_ms', lease_until_ms)
local prepared = redis.call('HINCRBY', marker, 'prepared_count', 1)
if prepared == tonumber(redis.call('HGET', marker, 'chunk_count')) then
  redis.call('HSET', marker, 'state', 'prepared')
end
return 1
`)

var applyChunkScript = redis.NewScript(`
local marker = KEYS[1]
local owner = ARGV[1]
local chunk = ARGV[2]
local lease_until_ms = ARGV[3]
local item_count = tonumber(ARGV[4])
local state = redis.call('HGET', marker, 'state')
if state == 'applied' then return 2 end
if redis.call('HGET', marker, 'owner') ~= owner then return redis.error_reply('aggregate fact lease was lost') end
local chunk_field = 'applied:' .. chunk
if redis.call('HEXISTS', marker, chunk_field) == 1 then
  redis.call('HSET', marker, 'lease_until_ms', lease_until_ms)
  return 0
end
if state ~= 'prepared' and state ~= 'committing' then return redis.error_reply('aggregate fact write was not completely prepared') end
if redis.call('HEXISTS', marker, 'prepared:' .. chunk) == 0 then return redis.error_reply('aggregate fact chunk was not prepared') end
redis.call('HSET', marker, 'state', 'committing')
local arg = 5
for i = 1, item_count do
  local sum_delta = ARGV[arg]
  local count_delta = ARGV[arg + 1]
  local expiry = ARGV[arg + 2]
  local dimension = ARGV[arg + 3]
  if sum_delta ~= '' then redis.call('HINCRBYFLOAT', KEYS[1 + i], 's:' .. dimension, sum_delta) end
  if count_delta ~= '' then redis.call('HINCRBY', KEYS[1 + i], 'c:' .. dimension, count_delta) end
  redis.call('EXPIREAT', KEYS[1 + i], expiry)
  arg = arg + 4
end
redis.call('HSET', marker, chunk_field, 1, 'lease_until_ms', lease_until_ms)
local applied = redis.call('HINCRBY', marker, 'applied_count', 1)
if applied == tonumber(redis.call('HGET', marker, 'chunk_count')) then
  redis.call('HSET', marker, 'state', 'finalizing')
end
return 1
`)

var finalizeChunkScript = redis.NewScript(`
local marker = KEYS[1]
local owner = ARGV[1]
local chunk = ARGV[2]
local lease_until_ms = ARGV[3]
local marker_ttl = tonumber(ARGV[4])
local item_count = tonumber(ARGV[5])
local state = redis.call('HGET', marker, 'state')
if state == 'applied' then return 2 end
if redis.call('HGET', marker, 'owner') ~= owner then return redis.error_reply('aggregate fact lease was lost') end
if state ~= 'finalizing' then return redis.error_reply('aggregate fact write is not ready to finalize') end
local chunk_field = 'finalized:' .. chunk
if redis.call('HEXISTS', marker, chunk_field) == 1 then
  redis.call('HSET', marker, 'lease_until_ms', lease_until_ms)
  return 0
end
for i = 1, item_count do
  local pending_field = ARGV[5 + i]
  local current = tonumber(redis.call('HGET', KEYS[1 + i], pending_field) or '0')
  if current <= 1 then
    redis.call('HDEL', KEYS[1 + i], pending_field)
    if redis.call('HLEN', KEYS[1 + i]) == 0 then redis.call('DEL', KEYS[1 + i]) end
  else
    redis.call('HINCRBY', KEYS[1 + i], pending_field, -1)
  end
end
redis.call('HSET', marker, chunk_field, 1, 'lease_until_ms', lease_until_ms)
local finalized = redis.call('HINCRBY', marker, 'finalized_count', 1)
if finalized == tonumber(redis.call('HGET', marker, 'chunk_count')) then
  redis.call('HSET', marker, 'state', 'applied')
  redis.call('HDEL', marker, 'owner', 'lease_until_ms')
  redis.call('EXPIRE', marker, marker_ttl)
end
return 1
`)

var finishEmptyScript = redis.NewScript(`
if redis.call('HGET', KEYS[1], 'state') == 'applied' then return 0 end
if redis.call('HGET', KEYS[1], 'owner') ~= ARGV[1] then return redis.error_reply('aggregate fact lease was lost') end
if tonumber(redis.call('HGET', KEYS[1], 'chunk_count') or '-1') ~= 0 then return redis.error_reply('aggregate fact write is not empty') end
redis.call('HSET', KEYS[1], 'state', 'applied')
redis.call('HDEL', KEYS[1], 'owner', 'lease_until_ms')
redis.call('EXPIRE', KEYS[1], ARGV[2])
return 1
`)

var renewScript = redis.NewScript(`
if redis.call('HGET', KEYS[1], 'state') == 'applied' then return 0 end
if redis.call('HGET', KEYS[1], 'owner') ~= ARGV[1] then return 0 end
redis.call('HSET', KEYS[1], 'lease_until_ms', ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[3])
return 1
`)

var confirmScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
if redis.call('HGET', KEYS[1], 'state') ~= 'applied' then return 0 end
if redis.call('HGET', KEYS[1], 'manifest') ~= ARGV[1] then return redis.error_reply('aggregate fact manifest mismatch') end
redis.call('DEL', KEYS[1])
return 1
`)

var abortChunkScript = redis.NewScript(`
local marker = KEYS[1]
local owner = ARGV[1]
local chunk = ARGV[2]
local item_count = tonumber(ARGV[3])
local state = redis.call('HGET', marker, 'state')
if redis.call('HGET', marker, 'owner') ~= owner then return 0 end
if state ~= 'preparing' and state ~= 'prepared' then return 0 end
if tonumber(redis.call('HGET', marker, 'applied_count') or '0') ~= 0 then return 0 end
if redis.call('HEXISTS', marker, 'prepared:' .. chunk) == 0 then return 0 end
if redis.call('HEXISTS', marker, 'aborted:' .. chunk) == 1 then return 0 end
for i = 1, item_count do
  local pending_field = ARGV[3 + i]
  local current = tonumber(redis.call('HGET', KEYS[1 + i], pending_field) or '0')
  if current <= 1 then
    redis.call('HDEL', KEYS[1 + i], pending_field)
    if redis.call('HLEN', KEYS[1 + i]) == 0 then redis.call('DEL', KEYS[1 + i]) end
  else
    redis.call('HINCRBY', KEYS[1 + i], pending_field, -1)
  end
end
redis.call('HSET', marker, 'aborted:' .. chunk, 1)
return 1
`)

var finishAbortScript = redis.NewScript(`
local state = redis.call('HGET', KEYS[1], 'state')
if redis.call('HGET', KEYS[1], 'owner') ~= ARGV[1] then return 0 end
if state ~= 'preparing' and state ~= 'prepared' then return 0 end
if tonumber(redis.call('HGET', KEYS[1], 'applied_count') or '0') ~= 0 then return 0 end
redis.call('DEL', KEYS[1])
return 1
`)

func New(redisURL string, db *pgxpool.Pool) (*Writer, error) {
	options, err := redis.ParseURL(strings.TrimSpace(redisURL))
	if err != nil {
		return nil, fmt.Errorf("parse aggregate fact redis url: %w", err)
	}
	// Chunking keeps normal operations short. These timeouts are deliberately
	// longer than go-redis defaults so a temporarily busy durable Valkey does not
	// turn an otherwise recoverable write into a client-side timeout.
	options.ReadTimeout = 15 * time.Second
	options.WriteTimeout = 15 * time.Second
	options.PoolTimeout = 15 * time.Second
	return &Writer{client: redis.NewClient(options), db: db}, nil
}

func (w *Writer) Close() error {
	if w == nil || w.client == nil {
		return nil
	}
	return w.client.Close()
}

func (w *Writer) Ping(ctx context.Context) error {
	if w == nil || w.client == nil {
		return fmt.Errorf("aggregate fact writer is not configured")
	}
	return w.client.Ping(ctx).Err()
}

func (w *Writer) Prepare(ctx context.Context, tenantID uuid.UUID, objectType string, records []map[string]any, requestMarker string) (preparedWrite ports.PreparedAggregateFactWrite, resultErr error) {
	w.metrics.prepareRequests.Add(1)
	startedAt := time.Now()
	w.maintenance.RLock()
	guardTransferred := false
	defer func() {
		if !guardTransferred {
			w.maintenance.RUnlock()
		}
		if resultErr != nil {
			w.metrics.prepareFailures.Add(1)
		}
		w.latencies.prepare.record(time.Since(startedAt), resultErr)
	}()
	definitions, err := w.listActive(ctx, tenantID.String(), objectType)
	if err != nil {
		return ports.PreparedAggregateFactWrite{}, fmt.Errorf("load aggregate fact registry: %w", err)
	}
	write := ports.PreparedAggregateFactWrite{TenantID: tenantID, ObjectType: objectType, MaintenanceGuard: true}
	if len(definitions) == 0 || len(records) == 0 {
		w.metrics.noActiveFactWrites.Add(1)
		return write, nil
	}
	w.metrics.recordsEvaluated.Add(uint64(len(records)))
	w.metrics.definitionsEvaluated.Add(uint64(len(definitions)))
	if err := w.ensureCoverage(ctx, tenantID, objectType, definitions, records); err != nil {
		return ports.PreparedAggregateFactWrite{}, err
	}
	deltas, err := aggregateDeltas(tenantID.String(), definitions, records, time.Now().UTC())
	if err != nil {
		return ports.PreparedAggregateFactWrite{}, err
	}
	write.Deltas = publicDeltas(deltas)
	write.PendingBuckets = pendingBucketsFromDeltas(deltas)
	write.Manifest = factWriteManifest(write.Deltas)
	write.Owner = uuid.NewString()
	digest := sha256.Sum256([]byte(requestMarker))
	// Registry changes must not give an already accepted ingestion a new
	// idempotency identity. New definitions start prospectively; existing
	// definitions must never receive the same event twice after publication.
	write.Marker = fmt.Sprintf("facts:{%s}:request:%s", tenantID, hex.EncodeToString(digest[:16]))
	result, err := w.begin(ctx, write)
	if err != nil {
		return ports.PreparedAggregateFactWrite{}, fmt.Errorf("prepare aggregate fact write: %w", err)
	}
	write.Prepared = true
	write.AlreadyApplied = result == 2
	write.OwnsMarker = result == 1
	write.PendingRetry = result == 3
	if write.OwnsMarker && !write.AlreadyApplied {
		if err := w.prepareChunks(ctx, write); err != nil {
			w.abortAfterPrepareFailure(write)
			return ports.PreparedAggregateFactWrite{}, fmt.Errorf("prepare aggregate fact chunks: %w", err)
		}
	}
	w.metrics.preparedWrites.Add(1)
	if write.AlreadyApplied {
		w.metrics.alreadyAppliedWrites.Add(1)
	}
	guardTransferred = true
	return write, nil
}

func (w *Writer) Renew(ctx context.Context, write ports.PreparedAggregateFactWrite) error {
	if !write.Prepared || write.AlreadyApplied || !write.OwnsMarker {
		return nil
	}
	startedAt := time.Now()
	_, err := renewScript.Run(ctx, w.client, []string{write.Marker},
		write.Owner, leaseUntil(time.Now()), int64(markerTTL/time.Second)).Result()
	w.latencies.valkeyWrites.record(time.Since(startedAt), err)
	if err != nil {
		return fmt.Errorf("renew aggregate fact write lease: %w", err)
	}
	return nil
}

func (w *Writer) Confirm(ctx context.Context, write ports.PreparedAggregateFactWrite) (resultErr error) {
	if !write.Prepared {
		return nil
	}
	startedAt := time.Now()
	defer func() { w.latencies.valkeyWrites.record(time.Since(startedAt), resultErr) }()
	if _, err := confirmScript.Run(ctx, w.client, []string{write.Marker}, write.Manifest).Result(); err != nil {
		return fmt.Errorf("confirm aggregate fact write: %w", err)
	}
	return nil
}

func (w *Writer) Commit(ctx context.Context, write ports.PreparedAggregateFactWrite) (resultErr error) {
	defer w.releaseMaintenanceGuard(write)
	if !write.Prepared || write.AlreadyApplied {
		return nil
	}
	w.metrics.commitRequests.Add(1)
	startedAt := time.Now()
	defer func() {
		duration := time.Since(startedAt)
		w.metrics.commitLatencyTotalMicros.Add(uint64(duration.Microseconds()))
		w.latencies.commit.record(duration, resultErr)
		if resultErr != nil {
			w.metrics.commitFailures.Add(1)
		}
	}()
	claim, err := w.begin(ctx, write)
	if err != nil {
		return fmt.Errorf("claim aggregate fact write: %w", err)
	}
	if claim == 2 {
		return nil
	}
	if claim == 3 {
		return fmt.Errorf("aggregate fact write is still owned by another request")
	}
	if !write.OwnsMarker {
		w.metrics.recoveredWrites.Add(1)
	}
	write.OwnsMarker = true
	if err := w.prepareChunks(ctx, write); err != nil {
		return fmt.Errorf("resume aggregate fact preparation: %w", err)
	}
	chunks := deltaChunks(write.Deltas)
	if len(chunks) == 0 {
		startedAt := time.Now()
		_, err := finishEmptyScript.Run(ctx, w.client, []string{write.Marker},
			write.Owner, int64(markerTTL/time.Second)).Result()
		w.latencies.valkeyWrites.record(time.Since(startedAt), err)
		if err != nil {
			return fmt.Errorf("commit empty aggregate fact write: %w", err)
		}
	} else {
		for index, chunk := range chunks {
			if err := w.applyChunk(ctx, write, index, chunk); err != nil {
				return fmt.Errorf("apply aggregate fact chunk %d/%d: %w", index+1, len(chunks), err)
			}
			w.metrics.commitChunks.Add(1)
		}
		for index, chunk := range chunks {
			if err := w.finalizeChunk(ctx, write, index, chunk); err != nil {
				return fmt.Errorf("finalize aggregate fact chunk %d/%d: %w", index+1, len(chunks), err)
			}
			w.metrics.finalizeChunks.Add(1)
		}
	}
	w.metrics.committedWrites.Add(1)
	w.metrics.groupedDeltasCommitted.Add(uint64(len(write.Deltas)))
	return nil
}

func (w *Writer) Abort(ctx context.Context, write ports.PreparedAggregateFactWrite) error {
	defer w.releaseMaintenanceGuard(write)
	if !write.Prepared || write.AlreadyApplied || !write.OwnsMarker {
		return nil
	}
	chunks := deltaChunks(write.Deltas)
	for index, chunk := range chunks {
		keys := make([]string, 1, 1+len(chunk))
		keys[0] = write.Marker
		args := make([]any, 0, 3+len(chunk))
		args = append(args, write.Owner, index, len(chunk))
		for _, delta := range chunk {
			keys = append(keys, delta.Key)
			args = append(args, factPendingField(delta.Dimension))
		}
		startedAt := time.Now()
		_, err := abortChunkScript.Run(ctx, w.client, keys, args...).Result()
		w.latencies.valkeyWrites.record(time.Since(startedAt), err)
		if err != nil {
			return fmt.Errorf("abort aggregate fact chunk %d/%d: %w", index+1, len(chunks), err)
		}
	}
	startedAt := time.Now()
	_, err := finishAbortScript.Run(ctx, w.client, []string{write.Marker}, write.Owner).Result()
	w.latencies.valkeyWrites.record(time.Since(startedAt), err)
	if err != nil {
		return fmt.Errorf("finish abort aggregate fact write: %w", err)
	}
	w.metrics.abortedWrites.Add(1)
	return nil
}

func (w *Writer) releaseMaintenanceGuard(write ports.PreparedAggregateFactWrite) {
	if write.MaintenanceGuard {
		w.maintenance.RUnlock()
	}
}

func (w *Writer) begin(ctx context.Context, write ports.PreparedAggregateFactWrite) (int, error) {
	now := time.Now()
	startedAt := time.Now()
	result, err := beginScript.Run(ctx, w.client, []string{write.Marker},
		write.Manifest, write.Owner, now.UnixMilli(), leaseUntil(now), len(deltaChunks(write.Deltas)),
		int64(markerTTL/time.Second)).Int()
	w.latencies.valkeyWrites.record(time.Since(startedAt), err)
	return result, err
}

func (w *Writer) prepareChunks(ctx context.Context, write ports.PreparedAggregateFactWrite) error {
	for index, chunk := range deltaChunks(write.Deltas) {
		keys := make([]string, 1, 1+len(chunk))
		keys[0] = write.Marker
		args := make([]any, 0, 4+2*len(chunk))
		args = append(args, write.Owner, index, leaseUntil(time.Now()), len(chunk))
		for _, delta := range chunk {
			keys = append(keys, delta.Key)
			args = append(args, factPendingField(delta.Dimension), delta.ExpiresAt)
		}
		startedAt := time.Now()
		_, err := prepareChunkScript.Run(ctx, w.client, keys, args...).Result()
		w.latencies.valkeyWrites.record(time.Since(startedAt), err)
		if err != nil {
			return fmt.Errorf("chunk %d/%d: %w", index+1, len(deltaChunks(write.Deltas)), err)
		}
		w.metrics.prepareChunks.Add(1)
	}
	return nil
}

func (w *Writer) applyChunk(ctx context.Context, write ports.PreparedAggregateFactWrite, index int, chunk []ports.AggregateFactDelta) error {
	keys := make([]string, 1, 1+len(chunk))
	keys[0] = write.Marker
	args := make([]any, 0, 4+4*len(chunk))
	args = append(args, write.Owner, index, leaseUntil(time.Now()), len(chunk))
	for _, delta := range chunk {
		keys = append(keys, delta.Key)
		sum := ""
		if delta.Sum != 0 {
			sum = strconv.FormatFloat(delta.Sum, 'g', -1, 64)
		}
		count := ""
		if delta.Count != 0 {
			count = strconv.FormatInt(delta.Count, 10)
		}
		args = append(args, sum, count, delta.ExpiresAt, delta.Dimension)
	}
	startedAt := time.Now()
	_, err := applyChunkScript.Run(ctx, w.client, keys, args...).Result()
	duration := time.Since(startedAt)
	w.latencies.valkeyWrites.record(duration, err)
	w.latencies.bucketApply.record(duration, err)
	return err
}

func (w *Writer) finalizeChunk(ctx context.Context, write ports.PreparedAggregateFactWrite, index int, chunk []ports.AggregateFactDelta) error {
	keys := make([]string, 1, 1+len(chunk))
	keys[0] = write.Marker
	args := make([]any, 0, 5+len(chunk))
	args = append(args, write.Owner, index, leaseUntil(time.Now()), int64(markerTTL/time.Second), len(chunk))
	for _, delta := range chunk {
		keys = append(keys, delta.Key)
		args = append(args, factPendingField(delta.Dimension))
	}
	startedAt := time.Now()
	_, err := finalizeChunkScript.Run(ctx, w.client, keys, args...).Result()
	w.latencies.valkeyWrites.record(time.Since(startedAt), err)
	return err
}

func (w *Writer) abortAfterPrepareFailure(write ports.PreparedAggregateFactWrite) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Prepare still owns and releases the maintenance read lock on this path.
	write.MaintenanceGuard = false
	_ = w.Abort(ctx, write)
}

func leaseUntil(now time.Time) int64 {
	return now.Add(factWriteLease).UnixMilli()
}

func deltaChunks(deltas []ports.AggregateFactDelta) [][]ports.AggregateFactDelta {
	if len(deltas) == 0 {
		return nil
	}
	out := make([][]ports.AggregateFactDelta, 0, (len(deltas)+factWriteChunkSize-1)/factWriteChunkSize)
	for start := 0; start < len(deltas); start += factWriteChunkSize {
		end := start + factWriteChunkSize
		if end > len(deltas) {
			end = len(deltas)
		}
		out = append(out, deltas[start:end])
	}
	return out
}

func (w *Writer) Snapshot() any {
	if w == nil {
		return AggregateFactWriteMetrics{}
	}
	return AggregateFactWriteMetrics{
		PrepareRequests: w.metrics.prepareRequests.Load(), PrepareFailures: w.metrics.prepareFailures.Load(),
		PreparedWrites: w.metrics.preparedWrites.Load(), NoActiveFactWrites: w.metrics.noActiveFactWrites.Load(),
		AlreadyAppliedWrites: w.metrics.alreadyAppliedWrites.Load(), RecordsEvaluated: w.metrics.recordsEvaluated.Load(),
		DefinitionsEvaluated: w.metrics.definitionsEvaluated.Load(), CommitRequests: w.metrics.commitRequests.Load(),
		CommitFailures: w.metrics.commitFailures.Load(), CommittedWrites: w.metrics.committedWrites.Load(),
		GroupedDeltasCommitted:   w.metrics.groupedDeltasCommitted.Load(),
		PrepareChunks:            w.metrics.prepareChunks.Load(),
		CommitChunks:             w.metrics.commitChunks.Load(),
		FinalizeChunks:           w.metrics.finalizeChunks.Load(),
		RecoveredWrites:          w.metrics.recoveredWrites.Load(),
		CommitLatencyTotalMicros: w.metrics.commitLatencyTotalMicros.Load(), AbortedWrites: w.metrics.abortedWrites.Load(),
		PrepareLatency: w.latencies.prepare.snapshot(), CommitLatency: w.latencies.commit.snapshot(),
		ValkeyReads: w.latencies.valkeyReads.snapshot(), ValkeyWrites: w.latencies.valkeyWrites.snapshot(),
		BucketApplyWrites: w.latencies.bucketApply.snapshot(),
	}
}

func (w *Writer) listActive(ctx context.Context, tenantID, tableName string) ([]ports.AggregateFactDefinition, error) {
	rows, err := w.db.Query(ctx, `
		SELECT DISTINCT d.id, d.tenant_id, d.table_name, d.signature,
		       d.dimension_fields, d.event_time_field, d.measure_field,
		       d.needs_sum, d.needs_count, d.max_window_seconds,
		       d.minute_enabled, d.backfill_required, d.coverage_token, d.version,
		       COALESCE(v.version, 0)
		FROM core.aggregate_fact_definitions d
		JOIN core.aggregate_fact_bindings b ON b.definition_id = d.id AND b.tenant_id = d.tenant_id
		JOIN core.scenarios s ON s.id = b.scenario_id AND s.live_iteration_id = b.iteration_id
		LEFT JOIN core.aggregate_fact_registry_versions v
		  ON v.tenant_id = d.tenant_id AND v.table_name = d.table_name
		WHERE d.tenant_id = $1 AND d.table_name = $2
		ORDER BY d.signature
	`, tenantID, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ports.AggregateFactDefinition, 0)
	for rows.Next() {
		var item ports.AggregateFactDefinition
		if err := rows.Scan(&item.ID, &item.TenantID, &item.TableName, &item.Signature,
			&item.DimensionFields, &item.EventTimeField, &item.MeasureField,
			&item.NeedsSum, &item.NeedsCount, &item.MaxWindowSeconds,
			&item.MinuteEnabled, &item.BackfillRequired, &item.CoverageToken, &item.Version, &item.RegistryVersion); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (w *Writer) ensureCoverage(ctx context.Context, tenantID uuid.UUID, tableName string, definitions []ports.AggregateFactDefinition, records []map[string]any) error {
	missing := make([]ports.AggregateFactDefinition, 0, len(definitions))
	for _, definition := range definitions {
		key := factCoverageKey(tenantID.String(), definition)
		startedAt := time.Now()
		values, err := w.client.MGet(ctx, key, factDirtyKey(tenantID.String(), definition)).Result()
		w.latencies.valkeyReads.record(time.Since(startedAt), err)
		if err != nil {
			return fmt.Errorf("read aggregate fact coverage: %w", err)
		}
		if len(values) == 2 && values[1] != nil {
			return fmt.Errorf("aggregate fact backfill is in progress or incomplete")
		}
		if len(values) == 2 && values[0] != nil {
			value, ok := values[0].(string)
			if !ok {
				return fmt.Errorf("aggregate fact coverage has an incompatible format")
			}
			if !strings.HasPrefix(value, definition.CoverageToken+"|") {
				return fmt.Errorf("aggregate fact coverage generation mismatch")
			}
			continue
		}
		missing = append(missing, definition)
	}
	if len(missing) == 0 {
		return nil
	}
	hasRows, err := w.tableHasRows(ctx, tenantID, tableName)
	if err != nil {
		return err
	}
	for _, definition := range missing {
		key := factCoverageKey(tenantID.String(), definition)
		coverage := definition.CoverageToken + "|all"
		if hasRows {
			return fmt.Errorf("aggregate fact backfill is required before ingesting into a non-empty table")
		}
		startedAt := time.Now()
		created, err := w.client.SetNX(ctx, key, coverage, 0).Result()
		w.latencies.valkeyWrites.record(time.Since(startedAt), err)
		if err != nil {
			return fmt.Errorf("establish aggregate fact coverage: %w", err)
		}
		if !created {
			startedAt = time.Now()
			current, readErr := w.client.Get(ctx, key).Result()
			w.latencies.valkeyReads.record(time.Since(startedAt), readErr)
			if readErr != nil || !strings.HasPrefix(current, definition.CoverageToken+"|") {
				return fmt.Errorf("aggregate fact coverage generation mismatch")
			}
		}
	}
	return nil
}

func (w *Writer) tableHasRows(ctx context.Context, tenantID uuid.UUID, tableName string) (bool, error) {
	schema := "tenant_" + strings.ReplaceAll(tenantID.String(), "-", "")
	quote := func(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
	var relation *string
	if err := w.db.QueryRow(ctx, `SELECT to_regclass($1)::text`, schema+"."+tableName).Scan(&relation); err != nil {
		return false, err
	}
	if relation == nil {
		return false, nil
	}
	var exists bool
	query := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.%s LIMIT 1)`, quote(schema), quote(tableName))
	if err := w.db.QueryRow(ctx, query).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func aggregateDeltas(tenantID string, definitions []ports.AggregateFactDefinition, records []map[string]any, now time.Time) ([]factDelta, error) {
	type deltaKey struct {
		key, dimension string
		expiry         int64
	}
	grouped := map[deltaKey]factDelta{}
	for _, definition := range definitions {
		for _, record := range records {
			eventTime, ok := timeValue(record[definition.EventTimeField])
			if !ok {
				return nil, fmt.Errorf("fact event-time field %s is missing or invalid", definition.EventTimeField)
			}
			dimension, ok := dimensionValue(record, definition.DimensionFields)
			if !ok {
				continue
			}
			measureValue, measurePresent := record[definition.MeasureField]
			measure, measureOK := floatValue(measureValue)
			countOK := measurePresent && measureValue != nil
			if definition.NeedsSum {
				countOK = measureOK
			}
			for _, resolution := range resolutions(definition) {
				bucket := eventTime.UTC().Truncate(resolution.size)
				expiry := retainedFactExpiry(bucket, resolution, now)
				key := factBucketKey(tenantID, definition, resolution.name, bucket)
				groupKey := deltaKey{key: key, dimension: dimension, expiry: expiry.Unix()}
				delta := grouped[groupKey]
				delta.key, delta.dimension, delta.expiresAt = key, dimension, expiry
				if definition.NeedsSum && measureOK {
					delta.sum += measure
				}
				if definition.NeedsCount && countOK {
					delta.count++
				}
				grouped[groupKey] = delta
			}
		}
	}
	out := make([]factDelta, 0, len(grouped))
	for _, delta := range grouped {
		out = append(out, delta)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].key == out[j].key {
			return out[i].dimension < out[j].dimension
		}
		return out[i].key < out[j].key
	})
	return out, nil
}

func pendingBucketsFromDeltas(deltas []factDelta) []ports.AggregateFactPendingBucket {
	// Pending state is kept inside the exact aggregate bucket and namespaced by
	// dimension. An in-flight merchant A write therefore cannot block merchant B
	// or completed historical buckets for merchant A.
	out := make([]ports.AggregateFactPendingBucket, 0, len(deltas))
	for _, delta := range deltas {
		out = append(out, ports.AggregateFactPendingBucket{
			Key: delta.key, Dimension: delta.dimension, ExpiresAt: delta.expiresAt.Unix(),
		})
	}
	return out
}

func publicDeltas(deltas []factDelta) []ports.AggregateFactDelta {
	out := make([]ports.AggregateFactDelta, 0, len(deltas))
	for _, delta := range deltas {
		out = append(out, ports.AggregateFactDelta{
			Key: delta.key, Dimension: delta.dimension, Sum: delta.sum,
			Count: delta.count, ExpiresAt: delta.expiresAt.Unix(),
		})
	}
	return out
}

func factWriteManifest(deltas []ports.AggregateFactDelta) string {
	hash := sha256.New()
	for _, delta := range deltas {
		// Expiration is intentionally excluded. A retry may extend the safety
		// lifetime of an old historical bucket, but it must never change which
		// arithmetic deltas belong to the request.
		_, _ = fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%d\n",
			delta.Key, delta.Dimension, strconv.FormatFloat(delta.Sum, 'g', -1, 64), delta.Count)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

type resolutionSpec struct {
	name      string
	size      time.Duration
	retention time.Duration
}

func resolutions(definition ports.AggregateFactDefinition) []resolutionSpec {
	out := []resolutionSpec{
		{name: "hour", size: time.Hour, retention: 7 * 24 * time.Hour},
		{name: "day", size: 24 * time.Hour, retention: 365 * 24 * time.Hour},
	}
	if definition.MinuteEnabled {
		out = append([]resolutionSpec{{name: "minute", size: time.Minute, retention: 7 * 24 * time.Hour}}, out...)
	}
	return out
}

func retainedFactExpiry(bucket time.Time, resolution resolutionSpec, now time.Time) time.Time {
	expiry := bucket.Add(resolution.size + resolution.retention)
	// Historical replay and backfill data must live for a complete retention
	// period from the time it is written. Event-time-only expiry would make an
	// old bucket disappear immediately or midway through a replay.
	if minimum := now.Add(resolution.retention); expiry.Before(minimum) {
		return minimum
	}
	return expiry
}

func factPendingField(dimension string) string {
	return "p:" + dimension
}

func factCoverageKey(tenantID string, definition ports.AggregateFactDefinition) string {
	return fmt.Sprintf("facts:{%s}:definition:%s:v%d:coverage", tenantID, definition.ID, definition.Version)
}

func factDirtyKey(tenantID string, definition ports.AggregateFactDefinition) string {
	return fmt.Sprintf("facts:{%s}:definition:%s:v%d:dirty", tenantID, definition.ID, definition.Version)
}

func factBucketKey(tenantID string, definition ports.AggregateFactDefinition, resolution string, bucket time.Time) string {
	return fmt.Sprintf("facts:{%s}:definition:%s:v%d:%s:%d", tenantID, definition.ID, definition.Version, resolution, bucket.Unix())
}

func dimensionValue(record map[string]any, fields []string) (string, bool) {
	values := make([]string, len(fields))
	for index, field := range fields {
		value, exists := record[field]
		if !exists || value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
			return "", false
		}
		canonical, ok := canonicalDimensionToken(value)
		if !ok {
			return "", false
		}
		values[index] = canonical
	}
	payload, _ := json.Marshal(values)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:16]), true
}

func canonicalDimensionToken(value any) (string, bool) {
	switch typed := value.(type) {
	case time.Time:
		return "t:" + typed.UTC().Format(time.RFC3339Nano), true
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
			return "t:" + parsed.UTC().Format(time.RFC3339Nano), true
		}
		return "s:" + typed, true
	case bool:
		return "b:" + strconv.FormatBool(typed), true
	case int:
		return "n:" + strconv.FormatInt(int64(typed), 10), true
	case int8:
		return "n:" + strconv.FormatInt(int64(typed), 10), true
	case int16:
		return "n:" + strconv.FormatInt(int64(typed), 10), true
	case int32:
		return "n:" + strconv.FormatInt(int64(typed), 10), true
	case int64:
		return "n:" + strconv.FormatInt(typed, 10), true
	case uint:
		return "n:" + strconv.FormatUint(uint64(typed), 10), true
	case uint8:
		return "n:" + strconv.FormatUint(uint64(typed), 10), true
	case uint16:
		return "n:" + strconv.FormatUint(uint64(typed), 10), true
	case uint32:
		return "n:" + strconv.FormatUint(uint64(typed), 10), true
	case uint64:
		return "n:" + strconv.FormatUint(typed, 10), true
	case float32:
		return canonicalDimensionFloat(float64(typed))
	case float64:
		return canonicalDimensionFloat(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return "", false
		}
		return canonicalDimensionFloat(parsed)
	default:
		return "", false
	}
}

func canonicalDimensionFloat(value float64) (string, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "", false
	}
	return "n:" + strconv.FormatFloat(value, 'g', -1, 64), true
}

func timeValue(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC(), true
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, typed)
		return parsed.UTC(), err == nil
	default:
		return time.Time{}, false
	}
}

func floatValue(value any) (float64, bool) {
	var result float64
	switch typed := value.(type) {
	case float64:
		result = typed
	case float32:
		result = float64(typed)
	case int:
		result = float64(typed)
	case int64:
		result = float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		result = parsed
	default:
		return 0, false
	}
	return result, !math.IsNaN(result) && !math.IsInf(result, 0)
}

var _ ports.AggregateFactWriter = (*Writer)(nil)
