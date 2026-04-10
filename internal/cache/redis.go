package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var rdb *redis.Client

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

func InitRedis(config *RedisConfig) error {
	if config.Addr == "" {
		return fmt.Errorf("redis addr is empty")
	}

	rdb = redis.NewClient(&redis.Options{
		Addr:     config.Addr,
		Password: config.Password,
		DB:       config.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("redis ping failed: %v", err)
	}

	return nil
}

func GetRedis() *redis.Client {
	return rdb
}

func ComputeSHA256(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}

func SetDesensitizedContent(ctx context.Context, desensitizedContent, originalContent string) error {
	if rdb == nil {
		return fmt.Errorf("redis not initialized")
	}

	key := ComputeSHA256(desensitizedContent)
	ttl := 24 * time.Hour

	return rdb.Set(ctx, key, originalContent, ttl).Err()
}

func GetOriginalContent(ctx context.Context, content string) (string, error) {
	if rdb == nil {
		return "", fmt.Errorf("redis not initialized")
	}

	key := ComputeSHA256(content)
	result, err := rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return result, nil
}
