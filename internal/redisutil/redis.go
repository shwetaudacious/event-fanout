package redisutil

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

// NewClient creates a Redis client from a redis:// or rediss:// URL.
func NewClient(redisURL string) (*redis.Client, error) {
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}

	u, err := url.Parse(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	opts := &redis.Options{
		Addr: fmt.Sprintf("%s:%s", u.Hostname(), redisPort(u)),
	}

	if u.User != nil {
		if password, ok := u.User.Password(); ok {
			opts.Password = password
		}
	}

	if u.Scheme == "rediss" {
		opts.TLSConfig = nil
	}

	if db := strings.TrimPrefix(u.Path, "/"); db != "" {
		if parsed, err := strconv.Atoi(db); err == nil {
			opts.DB = parsed
		}
	}

	return redis.NewClient(opts), nil
}

func redisPort(u *url.URL) string {
	if u.Port() != "" {
		return u.Port()
	}
	return "6379"
}
