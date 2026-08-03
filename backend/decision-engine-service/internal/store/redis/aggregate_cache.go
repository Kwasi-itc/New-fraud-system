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

func (c *AggregateCache) GetMany(ctx context.Context, keys []string) (map[string][]byte, error) {
	result := make(map[string][]byte, len(keys))
	if len(keys) == 0 {
		return result, nil
	}
	values, err := c.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	for i, value := range values {
		if value == nil {
			continue
		}
		switch item := value.(type) {
		case string:
			result[keys[i]] = []byte(item)
		case []byte:
			result[keys[i]] = item
		}
	}
	return result, nil
}

func (c *AggregateCache) SetMany(ctx context.Context, values map[string][]byte) error {
	if len(values) == 0 {
		return nil
	}
	items := make([]any, 0, len(values)*2)
	for key, value := range values {
		items = append(items, key, value)
	}
	return c.client.MSet(ctx, items...).Err()
}

func (c *AggregateCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *AggregateCache) Close() error {
	return c.client.Close()
}
