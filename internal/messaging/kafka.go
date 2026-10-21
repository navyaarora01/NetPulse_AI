package messaging

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"

	"netpulse/internal/config"
	"netpulse/internal/metrics"
)

type Bus struct{ Client *kgo.Client }

func New(c config.Config, topic, group string) (*Bus, error) {
	opts := []kgo.Opt{kgo.SeedBrokers(strings.Split(c.Brokers, ",")...), kgo.RecordPartitioner(kgo.StickyKeyPartitioner(nil)), kgo.ProducerLinger(5 * time.Millisecond), kgo.ProduceRequestTimeout(10 * time.Second), kgo.RecordDeliveryTimeout(20 * time.Second), kgo.MaxBufferedRecords(10000)}
	if c.KafkaTLS {
		opts = append(opts, kgo.DialTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}))
	}
	if c.KafkaUser != "" {
		opts = append(opts, kgo.SASL(plain.Auth{User: c.KafkaUser, Pass: c.KafkaPassword}.AsMechanism()))
	}
	if topic != "" {
		opts = append(opts, kgo.ConsumeTopics(topic), kgo.ConsumerGroup(group), kgo.DisableAutoCommit(), kgo.BlockRebalanceOnPoll(), kgo.RebalanceTimeout(5*time.Minute))
	}
	client, err := kgo.NewClient(opts...)
	return &Bus{client}, err
}
func (b *Bus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	err := b.Client.ProduceSync(ctx, &kgo.Record{Topic: topic, Key: []byte(key), Value: payload}).FirstErr()
	if err != nil {
		metrics.PublishFailed.Inc()
	}
	return err
}
func Backoff(attempt int) time.Duration {
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<attempt)*100*time.Millisecond + time.Duration(rand.IntN(100))*time.Millisecond
}
func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Consume commits only after durable handling or a broker-acknowledged DLQ write.
// A commit or DLQ publish failure terminates the loop so later offsets cannot skip it.
func (b *Bus) Consume(ctx context.Context, dlq string, handle func(context.Context, []byte) error, permanent func(error) bool) error {
	for ctx.Err() == nil {
		fetches := b.Client.PollRecords(ctx, 1)
		if ctx.Err() != nil {
			b.Client.AllowRebalance()
			return ctx.Err()
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			b.Client.AllowRebalance()
			return fmt.Errorf("Kafka fetch failed: %w", errs[0].Err)
		}
		for _, r := range fetches.Records() {
			var err error
			for attempt := 0; attempt < 3; attempt++ {
				err = handle(ctx, r.Value)
				if err == nil || permanent(err) {
					break
				}
				metrics.Failed.Inc()
				if e := Sleep(ctx, Backoff(attempt)); e != nil {
					b.Client.AllowRebalance()
					return e
				}
			}
			if err != nil {
				if ctx.Err() != nil {
					b.Client.AllowRebalance()
					return ctx.Err()
				}
				payload, _ := json.Marshal(map[string]any{"schema_version": 1, "source_topic": r.Topic, "partition": r.Partition, "offset": r.Offset, "key": string(r.Key), "reason": safeReason(err, permanent), "original": json.RawMessage(validJSON(r.Value)), "failed_at": time.Now().UTC()})
				if err = b.Publish(ctx, dlq, string(r.Key), payload); err != nil {
					b.Client.AllowRebalance()
					return err
				}
				metrics.DLQ.Inc()
			}
			if err = b.Client.CommitRecords(ctx, r); err != nil {
				b.Client.AllowRebalance()
				return err
			}
		}
		b.Client.AllowRebalance()
	}
	return ctx.Err()
}
func safeReason(err error, permanent func(error) bool) string {
	if permanent(err) {
		return "invalid or unknown event"
	}
	return "processing failed after bounded retries"
}
func validJSON(b []byte) []byte {
	if json.Valid(b) {
		return b
	}
	out, _ := json.Marshal(string(b))
	return out
}
