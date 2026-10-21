package messaging

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestBackoff(t *testing.T) {
	for i := 0; i < 12; i++ {
		d := Backoff(i)
		if d < 100*time.Millisecond || d > 6500*time.Millisecond {
			t.Fatalf("unbounded delay %v", d)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Sleep(ctx, time.Hour) == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestDLQEncoding(t *testing.T) {
	for _, raw := range []string{`{"ok":true}`, "invalid\x00"} {
		if !json.Valid(validJSON([]byte(raw))) {
			t.Fatal("invalid DLQ envelope")
		}
	}
}
