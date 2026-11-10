package eventbus

import (
	"context"
	"errors"
	"github.com/redis/go-redis/v9"
	"net/url"
	"regexp"
	"time"
)

var streamKey = regexp.MustCompile(`^[A-Za-z0-9:._-]{1,128}$`)

// RedisStream publishes once per outbox attempt. Consumers deduplicate by eventId
// because losing the Redis reply after XADD has an ambiguous outcome.
type RedisStream struct {
	client *redis.Client
	key    string
	limit  int64
}

func NewRedisStream(raw, key string, limit int) (*RedisStream, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || (u.Scheme != "redis" && u.Scheme != "rediss") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || !streamKey.MatchString(key) || limit < 1 || limit > 1000000 {
		return nil, errors.New("invalid Redis Streams configuration")
	}
	opts, e := redis.ParseURL(raw)
	if e != nil {
		return nil, errors.New("invalid Redis Streams connection settings")
	}
	opts.ContextTimeoutEnabled = true
	opts.MaxRetries = -1
	opts.DialTimeout = 3 * time.Second
	opts.ReadTimeout = 3 * time.Second
	opts.WriteTimeout = 3 * time.Second
	opts.PoolSize = 8
	return &RedisStream{client: redis.NewClient(opts), key: key, limit: int64(limit)}, nil
}
func (t *RedisStream) Name() string { return "redis-streams" }
func (t *RedisStream) Close() error { return t.client.Close() }
func (t *RedisStream) Deliver(ctx context.Context, e Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id, err := t.client.XAdd(ctx, &redis.XAddArgs{Stream: t.key, MaxLen: t.limit, Values: []any{"eventId", e.ID, "session", e.Session, "event", e.Name, "createdAt", e.Created, "payload", string(e.Data)}}).Result()
	if err != nil || id == "" {
		return errors.New("Redis Streams append was not confirmed")
	}
	return nil
}
