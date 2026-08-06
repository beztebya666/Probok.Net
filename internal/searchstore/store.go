package searchstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/greenroute/greenroute/internal/domain"
	"github.com/redis/go-redis/v9"
)

var (
	ErrNotFound      = errors.New("search not found")
	ErrAlreadyExists = errors.New("search already exists")
)

const maximumEventReplayBatch = 128

type Store interface {
	Create(context.Context, domain.RouteSearchResult, time.Duration) error
	Get(context.Context, string) (domain.RouteSearchResult, error)
	Update(context.Context, domain.RouteSearchResult, time.Duration) error
	Delete(context.Context, string) error
	AppendEvent(context.Context, domain.SearchEvent, time.Duration) (domain.SearchEvent, error)
	Finalize(context.Context, domain.RouteSearchResult, domain.SearchEvent, time.Duration) error
	EventsAfter(context.Context, string, int64) ([]domain.SearchEvent, error)
	ActiveBefore(context.Context, time.Time, int) ([]domain.RouteSearchResult, error)
	Ping(context.Context) error
	Close() error
}

type memoryRecord struct {
	result    domain.RouteSearchResult
	events    []domain.SearchEvent
	expiresAt time.Time
	nextEvent int64
}

type Memory struct {
	mu      sync.RWMutex
	records map[string]*memoryRecord
}

func NewMemory() *Memory { return &Memory{records: make(map[string]*memoryRecord)} }

func (m *Memory) Create(_ context.Context, result domain.RouteSearchResult, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeExpiredLocked(time.Now())
	if _, exists := m.records[result.SearchID]; exists {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, result.SearchID)
	}
	m.records[result.SearchID] = &memoryRecord{result: cloneResult(result), expiresAt: time.Now().Add(ttl)}
	return nil
}

func (m *Memory) Get(_ context.Context, id string) (domain.RouteSearchResult, error) {
	m.mu.RLock()
	record, ok := m.records[id]
	if !ok || time.Now().After(record.expiresAt) {
		m.mu.RUnlock()
		return domain.RouteSearchResult{}, ErrNotFound
	}
	result := cloneResult(record.result)
	m.mu.RUnlock()
	return result, nil
}

func (m *Memory) Update(_ context.Context, result domain.RouteSearchResult, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[result.SearchID]
	if !ok || time.Now().After(record.expiresAt) {
		return ErrNotFound
	}
	record.result = cloneResult(result)
	record.expiresAt = time.Now().Add(ttl)
	return nil
}

func (m *Memory) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.records[id]; !exists {
		return ErrNotFound
	}
	delete(m.records, id)
	return nil
}

func (m *Memory) AppendEvent(_ context.Context, event domain.SearchEvent, ttl time.Duration) (domain.SearchEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[event.SearchID]
	if !ok || time.Now().After(record.expiresAt) {
		return domain.SearchEvent{}, ErrNotFound
	}
	record.nextEvent++
	event.EventID = record.nextEvent
	record.events = append(record.events, cloneEvent(event))
	record.expiresAt = time.Now().Add(ttl)
	return event, nil
}

func (m *Memory) Finalize(_ context.Context, result domain.RouteSearchResult, event domain.SearchEvent, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[result.SearchID]
	if !ok || time.Now().After(record.expiresAt) {
		return ErrNotFound
	}
	if terminalStatus(record.result.Status) {
		return nil
	}
	record.nextEvent++
	event.EventID = record.nextEvent
	record.events = append(record.events, cloneEvent(event))
	record.result = cloneResult(result)
	record.expiresAt = time.Now().Add(ttl)
	return nil
}

func (m *Memory) EventsAfter(_ context.Context, id string, after int64) ([]domain.SearchEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.records[id]
	if !ok || time.Now().After(record.expiresAt) {
		return nil, ErrNotFound
	}
	if after < 0 {
		after = 0
	}
	// Event IDs are contiguous and start at one, so the cursor is also the
	// zero-based index of the first unread list item.
	if after >= int64(len(record.events)) {
		return []domain.SearchEvent{}, nil
	}
	end := minInt64(int64(len(record.events)), after+maximumEventReplayBatch)
	result := make([]domain.SearchEvent, 0, end-after)
	for _, event := range record.events[after:end] {
		result = append(result, cloneEvent(event))
	}
	return result, nil
}

func (m *Memory) ActiveBefore(_ context.Context, before time.Time, limit int) ([]domain.RouteSearchResult, error) {
	if limit < 1 {
		return []domain.RouteSearchResult{}, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now()
	result := make([]domain.RouteSearchResult, 0, minInt(limit, len(m.records)))
	for _, record := range m.records {
		if now.After(record.expiresAt) || terminalStatus(record.result.Status) || !record.result.GeneratedAt.Before(before) {
			continue
		}
		result = append(result, cloneResult(record.result))
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (m *Memory) Ping(context.Context) error { return nil }
func (m *Memory) Close() error               { return nil }

func (m *Memory) purgeExpiredLocked(now time.Time) {
	for id, record := range m.records {
		if now.After(record.expiresAt) {
			delete(m.records, id)
		}
	}
}

type Redis struct {
	client *redis.Client
	prefix string
}

var appendEventScript = redis.NewScript(`
if redis.call("EXISTS", KEYS[1]) == 0 then
  return -1
end
local sequence = redis.call("INCR", KEYS[3])
local event = cjson.decode(ARGV[1])
event["eventId"] = sequence
redis.call("RPUSH", KEYS[2], cjson.encode(event))
redis.call("PEXPIRE", KEYS[1], ARGV[2])
redis.call("PEXPIRE", KEYS[2], ARGV[2])
redis.call("PEXPIRE", KEYS[3], ARGV[2])
return sequence
`)

var createSearchScript = redis.NewScript(`
if redis.call("EXISTS", KEYS[1]) ~= 0 then
  return 0
end
redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
redis.call("ZADD", KEYS[2], ARGV[3], ARGV[4])
return 1
`)

var finalizeScript = redis.NewScript(`
local currentPayload = redis.call("GET", KEYS[1])
if not currentPayload then
  redis.call("ZREM", KEYS[4], ARGV[4])
  return -1
end
local current = cjson.decode(currentPayload)
if current["status"] ~= "ACCEPTED" and current["status"] ~= "SEARCHING" then
  redis.call("ZREM", KEYS[4], ARGV[4])
  return 0
end
local sequence = redis.call("INCR", KEYS[3])
local event = cjson.decode(ARGV[2])
event["eventId"] = sequence
redis.call("RPUSH", KEYS[2], cjson.encode(event))
redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[3])
redis.call("PEXPIRE", KEYS[2], ARGV[3])
redis.call("PEXPIRE", KEYS[3], ARGV[3])
redis.call("ZREM", KEYS[4], ARGV[4])
return sequence
`)

var deleteSearchScript = redis.NewScript(`
local existed = redis.call("EXISTS", KEYS[1])
redis.call("DEL", KEYS[1], KEYS[2], KEYS[3])
redis.call("ZREM", KEYS[4], ARGV[1])
return existed
`)

func NewRedis(options *redis.Options, prefix string) *Redis {
	if prefix == "" {
		prefix = "greenroute"
	}
	return &Redis{client: redis.NewClient(options), prefix: prefix}
}

func (s *Redis) resultKey(id string) string { return s.prefix + ":search:" + id + ":result" }
func (s *Redis) eventKey(id string) string  { return s.prefix + ":search:" + id + ":events" }
func (s *Redis) seqKey(id string) string    { return s.prefix + ":search:" + id + ":seq" }
func (s *Redis) activeKey() string          { return s.prefix + ":searches:active" }

func (s *Redis) Create(ctx context.Context, result domain.RouteSearchResult, ttl time.Duration) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	ttlMilliseconds := maxInt64(1, ttl.Milliseconds())
	ok, err := createSearchScript.Run(ctx, s.client,
		[]string{s.resultKey(result.SearchID), s.activeKey()},
		payload, ttlMilliseconds, result.GeneratedAt.UnixMilli(), result.SearchID,
	).Int()
	if err != nil {
		return err
	}
	if ok == 0 {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, result.SearchID)
	}
	return nil
}

func (s *Redis) Get(ctx context.Context, id string) (domain.RouteSearchResult, error) {
	payload, err := s.client.Get(ctx, s.resultKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return domain.RouteSearchResult{}, ErrNotFound
	}
	if err != nil {
		return domain.RouteSearchResult{}, err
	}
	var result domain.RouteSearchResult
	if err := json.Unmarshal(payload, &result); err != nil {
		return domain.RouteSearchResult{}, err
	}
	return result, nil
}

func (s *Redis) Update(ctx context.Context, result domain.RouteSearchResult, ttl time.Duration) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = s.client.SetArgs(ctx, s.resultKey(result.SearchID), payload, redis.SetArgs{Mode: "XX", TTL: ttl}).Result()
	if errors.Is(err, redis.Nil) {
		return ErrNotFound
	}
	return err
}

func (s *Redis) Delete(ctx context.Context, id string) error {
	removed, err := deleteSearchScript.Run(ctx, s.client,
		[]string{s.resultKey(id), s.eventKey(id), s.seqKey(id), s.activeKey()}, id,
	).Int()
	if err != nil {
		return err
	}
	if removed == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Redis) AppendEvent(ctx context.Context, event domain.SearchEvent, ttl time.Duration) (domain.SearchEvent, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return event, err
	}
	ttlMilliseconds := ttl.Milliseconds()
	if ttlMilliseconds < 1 {
		ttlMilliseconds = 1
	}
	sequence, err := appendEventScript.Run(ctx, s.client,
		[]string{s.resultKey(event.SearchID), s.eventKey(event.SearchID), s.seqKey(event.SearchID)},
		payload, ttlMilliseconds,
	).Int64()
	if err != nil {
		return event, err
	}
	if sequence < 0 {
		return event, ErrNotFound
	}
	event.EventID = sequence
	return event, nil
}

func (s *Redis) Finalize(ctx context.Context, result domain.RouteSearchResult, event domain.SearchEvent, ttl time.Duration) error {
	resultPayload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	eventPayload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	ttlMilliseconds := ttl.Milliseconds()
	if ttlMilliseconds < 1 {
		ttlMilliseconds = 1
	}
	sequence, err := finalizeScript.Run(ctx, s.client,
		[]string{s.resultKey(result.SearchID), s.eventKey(result.SearchID), s.seqKey(result.SearchID), s.activeKey()},
		resultPayload, eventPayload, ttlMilliseconds, result.SearchID,
	).Int64()
	if err != nil {
		return err
	}
	if sequence < 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Redis) EventsAfter(ctx context.Context, id string, after int64) ([]domain.SearchEvent, error) {
	if count, err := s.client.Exists(ctx, s.resultKey(id)).Result(); err != nil {
		return nil, err
	} else if count == 0 {
		return nil, ErrNotFound
	}
	if after < 0 {
		after = 0
	}
	end := after + maximumEventReplayBatch - 1
	if end < after { // guard an untrusted cursor at MaxInt64
		end = after
	}
	// Event IDs are contiguous and one-based. LRANGE therefore starts exactly
	// at the cursor rather than decoding the complete event history per poll.
	payloads, err := s.client.LRange(ctx, s.eventKey(id), after, end).Result()
	if err != nil {
		return nil, err
	}
	result := make([]domain.SearchEvent, 0, len(payloads))
	for _, payload := range payloads {
		var event domain.SearchEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return nil, err
		}
		if event.EventID > after {
			result = append(result, event)
		}
	}
	return result, nil
}

func (s *Redis) ActiveBefore(ctx context.Context, before time.Time, limit int) ([]domain.RouteSearchResult, error) {
	if limit < 1 {
		return []domain.RouteSearchResult{}, nil
	}
	if limit > 1000 {
		limit = 1000
	}
	ids, err := s.client.ZRangeByScore(ctx, s.activeKey(), &redis.ZRangeBy{
		Min: "-inf", Max: fmt.Sprintf("%d", before.UnixMilli()), Count: int64(limit),
	}).Result()
	if err != nil || len(ids) == 0 {
		return []domain.RouteSearchResult{}, err
	}
	keys := make([]string, len(ids))
	for index, id := range ids {
		keys[index] = s.resultKey(id)
	}
	values, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	results := make([]domain.RouteSearchResult, 0, len(values))
	remove := make([]interface{}, 0)
	for index, value := range values {
		if value == nil {
			remove = append(remove, ids[index])
			continue
		}
		payload, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("active search %s has an invalid Redis value", ids[index])
		}
		var result domain.RouteSearchResult
		if err := json.Unmarshal([]byte(payload), &result); err != nil {
			return nil, err
		}
		if terminalStatus(result.Status) {
			remove = append(remove, ids[index])
			continue
		}
		results = append(results, result)
	}
	if len(remove) > 0 {
		if err := s.client.ZRem(ctx, s.activeKey(), remove...).Err(); err != nil {
			return nil, err
		}
	}
	return results, nil
}

func (s *Redis) Ping(ctx context.Context) error { return s.client.Ping(ctx).Err() }
func (s *Redis) Close() error                   { return s.client.Close() }

func cloneResult(value domain.RouteSearchResult) domain.RouteSearchResult {
	payload, _ := json.Marshal(value)
	var result domain.RouteSearchResult
	_ = json.Unmarshal(payload, &result)
	return result
}

func cloneEvent(value domain.SearchEvent) domain.SearchEvent {
	payload, _ := json.Marshal(value)
	var result domain.SearchEvent
	_ = json.Unmarshal(payload, &result)
	return result
}

func terminalStatus(status domain.SearchStatus) bool {
	switch status {
	case domain.SearchCompleted, domain.SearchDegraded, domain.SearchFailed, domain.SearchCancelled:
		return true
	default:
		return false
	}
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
