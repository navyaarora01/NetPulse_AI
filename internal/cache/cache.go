package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"

	"netpulse/internal/domain"
	"netpulse/internal/metrics"
)

type Cache struct {
	Client *redis.Client
	TTL    time.Duration
}

func New(addr, password string, db int, ttl time.Duration) *Cache {
	return &Cache{redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, MaxRetries: 0}), ttl}
}
func (c *Cache) Get(ctx context.Context, id string) (domain.Device, bool) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var d domain.Device
	b, err := c.Client.Get(ctx, "device:"+id+":latest").Bytes()
	if err != nil {
		result := "error"
		if err == redis.Nil {
			result = "miss"
		}
		metrics.Cache.WithLabelValues(result).Inc()
		return d, false
	}
	if json.Unmarshal(b, &d) != nil {
		metrics.Cache.WithLabelValues("error").Inc()
		return d, false
	}
	metrics.Cache.WithLabelValues("hit").Inc()
	return d, true
}

// Set is atomic and version-aware, so an older API read cannot replace newer worker state.
func (c *Cache) Set(ctx context.Context, d domain.Device) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	version := d.Updated.UTC().Format("2006-01-02T15:04:05.000000000Z")
	b, err := json.Marshal(struct {
		domain.Device
		Version string `json:"cache_version"`
	}{d, version})
	if err == nil {
		err = c.Client.Eval(ctx, `local old=redis.call('GET',KEYS[1]); if old then local v=cjson.decode(old); if v.cache_version and v.cache_version > ARGV[2] then return 0 end end; redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[3]); return 1`, []string{"device:" + d.ID + ":latest"}, b, d.Updated.UTC().Format("2006-01-02T15:04:05.000000000Z"), c.TTL.Milliseconds()).Err()
	}
	if err != nil {
		metrics.Cache.WithLabelValues("error").Inc()
	} else {
		metrics.Cache.WithLabelValues("set").Inc()
	}
}
func (c *Cache) Device(ctx context.Context, id string, fallback func(context.Context, string) (domain.Device, error)) (domain.Device, error) {
	if d, ok := c.Get(ctx, id); ok {
		return d, nil
	}
	d, err := fallback(ctx, id)
	if err == nil {
		c.Set(ctx, d)
	}
	return d, err
}
