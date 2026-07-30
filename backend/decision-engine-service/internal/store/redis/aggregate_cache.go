package redis

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"
)

type AggregateCache struct {
	client *goredis.Client
}

func NewAggregateCache(redisURL string) (*AggregateCache, error) {
	options, err := goredis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	return &AggregateCache{client: goredis.NewClient(options)}, nil
}

func (c *AggregateCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := c.client.Get(ctx, key).Bytes()
	if err == goredis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

func (c *AggregateCache) Set(ctx context.Context, key string, value []byte) error {
	return c.client.Set(ctx, key, value, 0).Err()
}

func (c *AggregateCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *AggregateCache) Close() error {
	return c.client.Close()
}
