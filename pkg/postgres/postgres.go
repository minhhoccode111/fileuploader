// Package postgres implements postgres connection.
package postgres

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	_defaultMaxPoolSize  = 1
	_defaultConnAttempts = 10
	_defaultConnTimeout  = time.Second
)

// Postgres -.
type Postgres struct {
	maxPoolSize  int
	connAttempts int
	connTimeout  time.Duration

	Builder squirrel.StatementBuilderType

	pool   Pool
	logger Logger
}

// New -.
func New(url string, opts ...Option) (*Postgres, error) {
	pg := &Postgres{
		maxPoolSize:  _defaultMaxPoolSize,
		connAttempts: _defaultConnAttempts,
		connTimeout:  _defaultConnTimeout,
	}

	// Custom options
	for _, opt := range opts {
		opt(pg)
	}

	pg.Builder = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)

	poolConfig, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("postgres - NewPostgres - pgxpool.ParseConfig: %w", err)
	}

	poolConfig.MaxConns = safeIntToInt32(pg.maxPoolSize)

	var pool *pgxpool.Pool

	for pg.connAttempts > 0 {
		pool, err = pgxpool.NewWithConfig(context.Background(), poolConfig)
		if err == nil {
			break
		}

		if pg.logger != nil {
			pg.logger.Info("Postgres is trying to connect, attempts left: %d", pg.connAttempts)
		}

		time.Sleep(pg.connTimeout)

		pg.connAttempts--
	}

	if err != nil {
		return nil, fmt.Errorf("postgres - NewPostgres - connAttempts == 0: %w", err)
	}

	pg.pool = pool

	return pg, nil
}

// PoolStat is a snapshot of the connection pool, read at scrape time.
type PoolStat struct {
	Total    int32
	Idle     int32
	Acquired int32
	Max      int32
}

// Stat reports the current pool usage. A closed pool, or one that does not
// expose stats, reports zero.
func (p *Postgres) Stat() PoolStat {
	if p.pool == nil {
		return PoolStat{}
	}

	stat := p.pool.Stat()
	if stat == nil {
		return PoolStat{}
	}

	return PoolStat{
		Total:    stat.TotalConns(),
		Idle:     stat.IdleConns(),
		Acquired: stat.AcquiredConns(),
		Max:      stat.MaxConns(),
	}
}

// Close -.
func (p *Postgres) Close() {
	if p.pool != nil {
		p.pool.Close()
	}
}

func safeIntToInt32(v int) int32 {
	clamped := v & math.MaxInt32

	return int32(clamped)
}
