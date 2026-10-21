package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"netpulse/internal/domain"
)

func TestCacheHitMissExpiryAndFallback(t *testing.T) {
	server := miniredis.RunT(t)
	c := New(server.Addr(), "", 0, time.Minute)
	defer c.Client.Close()
	ctx := context.Background()
	calls := 0
	d := domain.Device{ID: "test", Name: "router", Status: "online", Updated: time.Now().UTC()}
	fallback := func(context.Context, string) (domain.Device, error) { calls++; return d, nil }
	if _, hit := c.Get(ctx, d.ID); hit {
		t.Fatal("expected miss")
	}
	if _, err := c.Device(ctx, d.ID, fallback); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Device(ctx, d.ID, fallback); err != nil || calls != 1 {
		t.Fatal("cache hit did not avoid database")
	}
	older := d
	older.Name = "stale"
	older.Updated = d.Updated.Add(-time.Second)
	c.Set(ctx, older)
	got, hit := c.Get(ctx, d.ID)
	if !hit || got.Name != "router" {
		t.Fatal("stale cache write accepted")
	}
	server.FastForward(2 * time.Minute)
	if _, hit = c.Get(ctx, d.ID); hit {
		t.Fatal("entry failed to expire")
	}
	server.Close()
	if _, err := c.Device(ctx, d.ID, fallback); err != nil || calls != 2 {
		t.Fatal("Redis outage did not fall back")
	}
}
