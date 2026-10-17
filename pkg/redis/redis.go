// Package redis implements a redis connection.
package redis

import (
	"context"
	"fmt"
	"log"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	_defaultPoolSize     = 10
	_defaultConnAttempts = 10
	_defaultConnTimeout  = time.Second
)

// Redis -.
type Redis struct {
	poolSize     int
	connAttempts int
	connTimeout  time.Duration

	Client *goredis.Client
}

// New -.
func New(addr string, opts ...Option) (*Redis, error) {
	r := &Redis{
		poolSize:     _defaultPoolSize,
		connAttempts: _defaultConnAttempts,
		connTimeout:  _defaultConnTimeout,
	}

	// Custom options
	for _, opt := range opts {
		opt(r)
	}

	r.Client = goredis.NewClient(&goredis.Options{
		Addr:     addr,
		PoolSize: r.poolSize,
	})

	err := r.ping(context.Background())
	if err != nil {
		r.Close()

		return nil, err
	}

	return r, nil
}

// ping pings the server, retrying until the attempts run out.
func (r *Redis) ping(ctx context.Context) error {
	var err error

	for r.connAttempts > 0 {
		err = r.Client.Ping(ctx).Err()
		if err == nil {
			return nil
		}

		log.Printf("Redis is trying to connect, attempts left: %d", r.connAttempts)

		time.Sleep(r.connTimeout)

		r.connAttempts--
	}

	return fmt.Errorf("redis - ping - connAttempts == 0: %w", err)
}

// Close -.
func (r *Redis) Close() {
	if r.Client == nil {
		return
	}

	if err := r.Client.Close(); err != nil {
		log.Printf("redis - Close - client.Close: %v", err)
	}
}
