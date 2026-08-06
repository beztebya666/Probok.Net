package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/greenroute/greenroute/internal/ids"
	"github.com/redis/go-redis/v9"
)

var (
	errStateNotFound   = errors.New("state not found")
	errStateConflict   = errors.New("state conflict")
	errStateInProgress = errors.New("state in progress")
)

type idempotencyRecord struct {
	BodyHash   string    `json:"bodyHash"`
	ClaimToken string    `json:"claimToken,omitempty"`
	Pending    bool      `json:"pending"`
	SearchID   string    `json:"searchId,omitempty"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type edgeState interface {
	BeginIdempotency(context.Context, string, string, time.Duration, time.Duration) (*idempotencyRecord, string, error)
	CompleteIdempotency(context.Context, string, string, string, string, time.Duration) error
	ForgetIdempotency(context.Context, string, string) error
	SetOwner(context.Context, string, string, time.Duration) error
	Owns(context.Context, string, string) (bool, error)
	DeleteOwner(context.Context, string) error
	Allow(context.Context, string, int, time.Duration) (bool, error)
	Ping(context.Context) error
	Close() error
}

type memoryValue struct {
	value     any
	expiresAt time.Time
}

type memoryState struct {
	mu                      sync.Mutex
	idempotency             map[string]memoryValue
	idempotencyFingerprints map[string]memoryValue
	owners                  map[string]memoryValue
	counters                map[string]memoryValue
}

func newMemoryState() *memoryState {
	return &memoryState{
		idempotency: make(map[string]memoryValue), idempotencyFingerprints: make(map[string]memoryValue),
		owners: make(map[string]memoryValue), counters: make(map[string]memoryValue),
	}
}

func (s *memoryState) BeginIdempotency(_ context.Context, key, bodyHash string, recordTTL, claimTTL time.Duration) (*idempotencyRecord, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if stored, ok := s.idempotency[key]; ok && now.Before(stored.expiresAt) {
		record := stored.value.(idempotencyRecord)
		if record.BodyHash != bodyHash {
			return nil, "", errStateConflict
		}
		// The authoritative record repairs a missing/expired fingerprint without
		// allowing a conflicting retry to poison it.
		s.idempotencyFingerprints[key] = memoryValue{value: record.BodyHash, expiresAt: now.Add(recordTTL)}
		if record.Pending {
			return nil, record.ClaimToken, errStateInProgress
		}
		copy := record
		return &copy, "", nil
	}
	delete(s.idempotency, key)
	if fingerprint, ok := s.idempotencyFingerprints[key]; ok && now.Before(fingerprint.expiresAt) {
		if fingerprint.value.(string) != bodyHash {
			return nil, "", errStateConflict
		}
	} else {
		delete(s.idempotencyFingerprints, key)
		s.idempotencyFingerprints[key] = memoryValue{value: bodyHash, expiresAt: now.Add(recordTTL)}
	}
	claimToken := ids.New()
	record := idempotencyRecord{BodyHash: bodyHash, ClaimToken: claimToken, Pending: true, ExpiresAt: now.Add(claimTTL)}
	s.idempotency[key] = memoryValue{value: record, expiresAt: record.ExpiresAt}
	return nil, claimToken, nil
}

func (s *memoryState) CompleteIdempotency(_ context.Context, key, bodyHash, claimToken, searchID string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.idempotency[key]
	if !ok || time.Now().After(stored.expiresAt) {
		delete(s.idempotency, key)
		return errStateNotFound
	}
	record := stored.value.(idempotencyRecord)
	if record.BodyHash != bodyHash || record.ClaimToken != claimToken || !record.Pending {
		return errStateConflict
	}
	record.Pending, record.SearchID, record.ExpiresAt = false, searchID, time.Now().Add(ttl)
	record.ClaimToken = ""
	s.idempotency[key] = memoryValue{value: record, expiresAt: record.ExpiresAt}
	s.idempotencyFingerprints[key] = memoryValue{value: bodyHash, expiresAt: record.ExpiresAt}
	return nil
}

func (s *memoryState) ForgetIdempotency(_ context.Context, key, claimToken string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.idempotency[key]
	if !ok || time.Now().After(stored.expiresAt) {
		delete(s.idempotency, key)
		return errStateNotFound
	}
	record := stored.value.(idempotencyRecord)
	if !record.Pending || record.ClaimToken != claimToken {
		return errStateConflict
	}
	delete(s.idempotency, key)
	delete(s.idempotencyFingerprints, key)
	return nil
}

func (s *memoryState) SetOwner(_ context.Context, searchID, owner string, ttl time.Duration) error {
	s.mu.Lock()
	s.owners[searchID] = memoryValue{value: owner, expiresAt: time.Now().Add(ttl)}
	s.mu.Unlock()
	return nil
}

func (s *memoryState) Owns(_ context.Context, searchID, owner string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.owners[searchID]
	if !ok || time.Now().After(stored.expiresAt) {
		delete(s.owners, searchID)
		return false, nil
	}
	return stored.value.(string) == owner, nil
}

func (s *memoryState) DeleteOwner(_ context.Context, searchID string) error {
	s.mu.Lock()
	delete(s.owners, searchID)
	s.mu.Unlock()
	return nil
}

func (s *memoryState) Allow(_ context.Context, key string, limit int, window time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	stored, ok := s.counters[key]
	count := int64(0)
	if ok && now.Before(stored.expiresAt) {
		count = stored.value.(int64)
	} else {
		stored.expiresAt = now.Add(window)
	}
	count++
	s.counters[key] = memoryValue{value: count, expiresAt: stored.expiresAt}
	return count <= int64(limit), nil
}

func (s *memoryState) Ping(context.Context) error { return nil }
func (s *memoryState) Close() error               { return nil }

type redisState struct {
	client *redis.Client
}

func newRedisState(options *redis.Options) *redisState {
	return &redisState{client: redis.NewClient(options)}
}

func (s *redisState) BeginIdempotency(ctx context.Context, key, bodyHash string, recordTTL, claimTTL time.Duration) (*idempotencyRecord, string, error) {
	recordKey, fingerprintKey := idempotencyRedisKeys(key)
	existingPayload, err := s.client.Get(ctx, recordKey).Bytes()
	if err == nil {
		existing, claimToken, stateErr := existingIdempotency(existingPayload, bodyHash)
		if stateErr == nil || errors.Is(stateErr, errStateInProgress) {
			if _, repairErr := s.client.SetNX(ctx, fingerprintKey, bodyHash, recordTTL).Result(); repairErr != nil {
				return nil, "", repairErr
			}
		}
		return existing, claimToken, stateErr
	}
	if !errors.Is(err, redis.Nil) {
		return nil, "", err
	}
	fingerprintCreated, err := s.client.SetNX(ctx, fingerprintKey, bodyHash, recordTTL).Result()
	if err != nil {
		return nil, "", err
	}
	if !fingerprintCreated {
		existingHash, getErr := s.client.Get(ctx, fingerprintKey).Result()
		if errors.Is(getErr, redis.Nil) {
			return s.BeginIdempotency(ctx, key, bodyHash, recordTTL, claimTTL)
		}
		if getErr != nil {
			return nil, "", getErr
		}
		if existingHash != bodyHash {
			return nil, "", errStateConflict
		}
	}
	claimToken := ids.New()
	record := idempotencyRecord{BodyHash: bodyHash, ClaimToken: claimToken, Pending: true, ExpiresAt: time.Now().Add(claimTTL)}
	payload, _ := json.Marshal(record)
	ok, err := s.client.SetNX(ctx, recordKey, payload, claimTTL).Result()
	if err != nil {
		return nil, "", err
	}
	if ok {
		return nil, claimToken, nil
	}
	existingPayload, err = s.client.Get(ctx, recordKey).Bytes()
	if errors.Is(err, redis.Nil) {
		return s.BeginIdempotency(ctx, key, bodyHash, recordTTL, claimTTL)
	}
	if err != nil {
		return nil, "", err
	}
	return existingIdempotency(existingPayload, bodyHash)
}

func existingIdempotency(payload []byte, bodyHash string) (*idempotencyRecord, string, error) {
	var existing idempotencyRecord
	if err := json.Unmarshal(payload, &existing); err != nil {
		return nil, "", err
	}
	if existing.BodyHash != bodyHash {
		return nil, "", errStateConflict
	}
	if existing.Pending {
		return nil, existing.ClaimToken, errStateInProgress
	}
	return &existing, "", nil
}

var completeIdempotencyScript = redis.NewScript(`
local payload = redis.call('GET', KEYS[1])
if not payload then return -1 end
local record = cjson.decode(payload)
if record['bodyHash'] ~= ARGV[1] or record['claimToken'] ~= ARGV[2] or record['pending'] ~= true then return -2 end
record['pending'] = false
record['claimToken'] = nil
record['searchId'] = ARGV[3]
record['expiresAt'] = ARGV[4]
redis.call('SET', KEYS[1], cjson.encode(record), 'PX', ARGV[5])
redis.call('SET', KEYS[2], ARGV[1], 'PX', ARGV[5])
return 1
`)

func (s *redisState) CompleteIdempotency(ctx context.Context, key, bodyHash, claimToken, searchID string, ttl time.Duration) error {
	expiresAt := time.Now().Add(ttl)
	recordKey, fingerprintKey := idempotencyRedisKeys(key)
	result, err := completeIdempotencyScript.Run(ctx, s.client, []string{recordKey, fingerprintKey},
		bodyHash, claimToken, searchID, expiresAt.Format(time.RFC3339Nano), durationMilliseconds(ttl),
	).Int()
	if err != nil {
		return err
	}
	switch result {
	case -1:
		return errStateNotFound
	case -2:
		return errStateConflict
	default:
		return nil
	}
}

var forgetIdempotencyScript = redis.NewScript(`
local payload = redis.call('GET', KEYS[1])
if not payload then return -1 end
local record = cjson.decode(payload)
if record['claimToken'] ~= ARGV[1] or record['pending'] ~= true then return -2 end
redis.call('DEL', KEYS[1])
redis.call('DEL', KEYS[2])
return 1
`)

func (s *redisState) ForgetIdempotency(ctx context.Context, key, claimToken string) error {
	recordKey, fingerprintKey := idempotencyRedisKeys(key)
	result, err := forgetIdempotencyScript.Run(ctx, s.client, []string{recordKey, fingerprintKey}, claimToken).Int()
	if err != nil {
		return err
	}
	switch result {
	case -1:
		return errStateNotFound
	case -2:
		return errStateConflict
	default:
		return nil
	}
}

func idempotencyRedisKeys(key string) (string, string) {
	hashTag := "{" + key + "}"
	return "greenroute:idem:" + hashTag + ":record", "greenroute:idem:" + hashTag + ":fingerprint"
}

func (s *redisState) SetOwner(ctx context.Context, searchID, owner string, ttl time.Duration) error {
	return s.client.Set(ctx, "greenroute:owner:"+searchID, owner, ttl).Err()
}

func (s *redisState) Owns(ctx context.Context, searchID, owner string) (bool, error) {
	value, err := s.client.Get(ctx, "greenroute:owner:"+searchID).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	return value == owner, err
}

func (s *redisState) DeleteOwner(ctx context.Context, searchID string) error {
	return s.client.Del(ctx, "greenroute:owner:"+searchID).Err()
}

var rateLimitScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return current
`)

func (s *redisState) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	count, err := rateLimitScript.Run(ctx, s.client, []string{"greenroute:rate:" + key}, window.Milliseconds()).Int64()
	return count <= int64(limit), err
}

func (s *redisState) Ping(ctx context.Context) error { return s.client.Ping(ctx).Err() }
func (s *redisState) Close() error                   { return s.client.Close() }

func durationMilliseconds(value time.Duration) int64 {
	if milliseconds := value.Milliseconds(); milliseconds > 0 {
		return milliseconds
	}
	return 1
}
