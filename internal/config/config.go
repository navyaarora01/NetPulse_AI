package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env              string
	Addr             string
	DatabaseURL      string
	RedisAddr        string
	RedisPassword    string
	RedisDB          int
	Brokers          string
	Group            string
	TelemetryTopic   string
	DLQTopic         string
	IncidentTopic    string
	AnalysisTopic    string
	AnalysisDLQ      string
	Origin           string
	Token            string
	KafkaTLS         bool
	KafkaUser        string
	KafkaPassword    string
	Latency          float64
	Loss             float64
	CPU              float64
	Memory           float64
	Bandwidth        float64
	SeverityHigh     float64
	SeverityCritical float64
	Window           time.Duration
	Offline          time.Duration
	Recovery         time.Duration
	Cooldown         time.Duration
	Retention        time.Duration
	CacheTTL         time.Duration
	AITimeout        time.Duration
	MinSamples       int
	Instability      int
	AIEnabled        bool
	AIProvider       string
	AIURL            string
	AIKey            string
	AIModel          string
}

func Load() (Config, error) {
	c := Config{
		Env:            env("APP_ENV", "local"),
		Addr:           env("API_ADDR", ":8080"),
		DatabaseURL:    env("DATABASE_URL", "postgres://netpulse:netpulse@localhost:5432/netpulse?sslmode=disable"),
		RedisAddr:      env("REDIS_ADDR", "localhost:6379"),
		RedisPassword:  os.Getenv("REDIS_PASSWORD"),
		Brokers:        env("KAFKA_BROKERS", "localhost:9092"),
		Group:          env("KAFKA_CONSUMER_GROUP", "netpulse-worker"),
		TelemetryTopic: env("KAFKA_TELEMETRY_TOPIC", "network-telemetry"),
		DLQTopic:       env("KAFKA_TELEMETRY_DLQ_TOPIC", "network-telemetry-dlq"),
		IncidentTopic:  env("KAFKA_INCIDENT_TOPIC", "incident-events"),
		AnalysisTopic:  env("KAFKA_ANALYSIS_TOPIC", "incident-analysis-requests"),
		AnalysisDLQ:    env("KAFKA_ANALYSIS_DLQ_TOPIC", "incident-analysis-dlq"),
		Origin:         env("CORS_ORIGIN", "http://localhost:5173"),
		Token:          os.Getenv("API_TOKEN"),
		KafkaUser:      os.Getenv("KAFKA_USERNAME"),
		KafkaPassword:  os.Getenv("KAFKA_PASSWORD"),
		AIProvider:     env("AI_PROVIDER", "mock"),
		AIURL:          os.Getenv("AI_BASE_URL"),
		AIKey:          os.Getenv("AI_API_KEY"),
		AIModel:        os.Getenv("AI_MODEL"),
	}
	var err error
	for _, d := range []struct {
		k, def string
		p      *time.Duration
	}{{"ANOMALY_WINDOW", "60s", &c.Window}, {"DEVICE_OFFLINE_TIMEOUT", "60s", &c.Offline}, {"RECOVERY_WINDOW", "2m", &c.Recovery}, {"INCIDENT_COOLDOWN", "5m", &c.Cooldown}, {"TELEMETRY_RETENTION", "168h", &c.Retention}, {"CACHE_TTL", "30s", &c.CacheTTL}, {"AI_TIMEOUT", "20s", &c.AITimeout}} {
		*d.p, err = time.ParseDuration(env(d.k, d.def))
		if err != nil || *d.p <= 0 {
			return c, fmt.Errorf("%s must be a positive duration", d.k)
		}
	}
	for _, n := range []struct {
		k, def string
		p      *float64
	}{{"LATENCY_THRESHOLD_MS", "150", &c.Latency}, {"PACKET_LOSS_THRESHOLD_PCT", "5", &c.Loss}, {"CPU_THRESHOLD_PCT", "90", &c.CPU}, {"MEMORY_THRESHOLD_PCT", "90", &c.Memory}, {"BANDWIDTH_THRESHOLD_PCT", "90", &c.Bandwidth}, {"SEVERITY_HIGH_MULTIPLIER", "1.25", &c.SeverityHigh}, {"SEVERITY_CRITICAL_MULTIPLIER", "2", &c.SeverityCritical}} {
		*n.p, err = strconv.ParseFloat(env(n.k, n.def), 64)
		if err != nil || !(*n.p > 0) || *n.p > 60000 {
			return c, fmt.Errorf("invalid %s", n.k)
		}
	}
	c.MinSamples, err = strconv.Atoi(env("ANOMALY_MIN_SAMPLES", "5"))
	if err != nil || c.MinSamples < 1 || c.MinSamples > 1000 {
		return c, fmt.Errorf("ANOMALY_MIN_SAMPLES must be 1..1000")
	}
	c.Instability, err = strconv.Atoi(env("INSTABILITY_TRANSITIONS", "4"))
	if err != nil || c.Instability < 2 {
		return c, fmt.Errorf("INSTABILITY_TRANSITIONS must be >=2")
	}
	c.RedisDB, err = strconv.Atoi(env("REDIS_DB", "0"))
	if err != nil || c.RedisDB < 0 {
		return c, fmt.Errorf("invalid REDIS_DB")
	}
	c.AIEnabled, err = strconv.ParseBool(env("AI_ENABLED", "false"))
	if err != nil {
		return c, fmt.Errorf("invalid AI_ENABLED")
	}
	c.KafkaTLS, err = strconv.ParseBool(env("KAFKA_TLS", "false"))
	if err != nil {
		return c, fmt.Errorf("invalid KAFKA_TLS")
	}
	if c.Env != "local" && (len(c.Token) < 32 || !c.KafkaTLS) {
		return c, fmt.Errorf("non-local environments require API_TOKEN (32+ characters) and KAFKA_TLS=true")
	}
	if c.AIEnabled && c.AIProvider != "mock" && (c.AIProvider != "openai-compatible" || !strings.HasPrefix(c.AIURL, "https://") || c.AIKey == "" || c.AIModel == "") {
		return c, fmt.Errorf("AI requires provider mock or openai-compatible with HTTPS AI_BASE_URL, AI_API_KEY, AI_MODEL")
	}
	if c.SeverityHigh <= 1 || c.SeverityCritical <= c.SeverityHigh {
		return c, fmt.Errorf("severity multipliers require 1 < high < critical")
	}
	if (c.KafkaUser == "") != (c.KafkaPassword == "") {
		return c, fmt.Errorf("KAFKA_USERNAME and KAFKA_PASSWORD must be configured together")
	}
	if strings.TrimSpace(c.Origin) == "*" {
		return c, fmt.Errorf("CORS_ORIGIN must be an exact origin")
	}
	if c.AITimeout > 90*time.Second {
		return c, fmt.Errorf("AI_TIMEOUT must be <=90s for the 2-minute analysis lease")
	}
	if c.Loss > 100 || c.CPU > 100 || c.Memory > 100 || c.Bandwidth > 100 {
		return c, fmt.Errorf("percentage thresholds must be <=100")
	}
	if c.Retention < 24*time.Hour || c.Window > 24*time.Hour {
		return c, fmt.Errorf("retention must be >=24h; anomaly window <=24h")
	}
	return c, nil
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
