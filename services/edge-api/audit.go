package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/greenroute/greenroute/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type auditor struct {
	pool    *pgxpool.Pool
	hashKey []byte
}

func newAuditor(ctx context.Context, databaseURL, hashKey string, required bool) (*auditor, error) {
	if databaseURL == "" {
		if required {
			return nil, fmt.Errorf("database is required")
		}
		return &auditor{hashKey: []byte(hashKey)}, nil
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	pingContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := pool.Ping(pingContext); err != nil {
		pool.Close()
		if required {
			return nil, err
		}
		return &auditor{hashKey: []byte(hashKey)}, nil
	}
	return &auditor{pool: pool, hashKey: []byte(hashKey)}, nil
}

func (a *auditor) recordSearch(ctx context.Context, owner string, request domain.RouteSearchRequest, result domain.RouteSearchResult) error {
	if a == nil || a.pool == nil {
		return nil
	}
	_, err := a.pool.Exec(ctx, `INSERT INTO route_search_audit
        (search_id, request_id, subject_hash, routing_mode, max_extra_distance_bucket_m, provider_request_budget, accepted_at)
        VALUES ($1,$2,$3,$4,$5,$6,now()) ON CONFLICT (search_id) DO NOTHING`,
		result.SearchID, result.RequestID, a.hashSubject(owner), request.RoutingMode, bucketDistance(request.MaxExtraDistanceMeters), request.MaxProviderRequests)
	return err
}

func (a *auditor) purgeBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if a == nil || a.pool == nil {
		return 0, nil
	}
	command, err := a.pool.Exec(ctx, `DELETE FROM route_search_audit WHERE accepted_at < $1`, cutoff.UTC())
	if err != nil {
		return 0, err
	}
	return command.RowsAffected(), nil
}

func (a *auditor) runRetention(ctx context.Context, retention, interval time.Duration, success func(int64), failure func(error)) {
	if a == nil || a.pool == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			purgeContext, cancel := context.WithTimeout(ctx, 30*time.Second)
			rows, err := a.purgeBefore(purgeContext, retentionCutoff(time.Now(), retention))
			cancel()
			if err != nil {
				failure(err)
			} else {
				success(rows)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func retentionCutoff(now time.Time, retention time.Duration) time.Time {
	return now.UTC().Add(-retention)
}

func (a *auditor) ping(ctx context.Context) error {
	if a == nil || a.pool == nil {
		return nil
	}
	return a.pool.Ping(ctx)
}

func (a *auditor) close() {
	if a != nil && a.pool != nil {
		a.pool.Close()
	}
}

func (a *auditor) hashSubject(subject string) string {
	if len(a.hashKey) == 0 {
		hash := sha256.Sum256([]byte(subject))
		return hex.EncodeToString(hash[:])
	}
	hash := hmac.New(sha256.New, a.hashKey)
	_, _ = hash.Write([]byte(subject))
	return hex.EncodeToString(hash.Sum(nil))
}

func bucketDistance(distance int64) int64 {
	const bucket = int64(5_000)
	if distance <= 0 {
		return 0
	}
	return ((distance + bucket - 1) / bucket) * bucket
}

func runMigrations(ctx context.Context, databaseURL string) error {
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required for migrations")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		name := entry.Name()
		var applied bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, name).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(body)); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, name)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
