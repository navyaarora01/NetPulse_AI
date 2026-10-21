package config

import "testing"

func TestDefaultsAndInvalidConfig(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"NaN", "-1", "0", "Inf"} {
		t.Setenv("LATENCY_THRESHOLD_MS", v)
		if _, err := Load(); err == nil {
			t.Fatalf("accepted %s", v)
		}
	}
}
func TestProductionRequiresProtection(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("API_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("production allowed without protection")
	}
}
