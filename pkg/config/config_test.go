package config

import "testing"

// clearNATSAuthEnv resets every env var natsHasCredentials/Load consult, so
// a developer's own shell (or CI) having one of these set doesn't make the
// test's "no credentials configured" cases flaky.
func clearNATSAuthEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"NATS_CREDENTIALS_FILE",
		"NATS_USERNAME",
		"NATS_PASSWORD",
		"NATS_TLS_CA_FILE",
		"NATS_TLS_CERT_FILE",
		"NATS_TLS_KEY_FILE",
		"NATS_ALLOW_UNAUTHENTICATED",
	} {
		t.Setenv(key, "")
	}
}

func TestLoad_RefusesUnauthenticatedNATSByDefault(t *testing.T) {
	clearNATSAuthEnv(t)

	if _, err := Load(); err == nil {
		t.Fatal("Load() with no NATS credentials and NATS_ALLOW_UNAUTHENTICATED unset = nil error, want an error")
	}
}

func TestLoad_AllowsUnauthenticatedNATSWhenExplicitlyOptedIn(t *testing.T) {
	clearNATSAuthEnv(t)
	t.Setenv("NATS_ALLOW_UNAUTHENTICATED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() with NATS_ALLOW_UNAUTHENTICATED=true = %v, want no error", err)
	}
	if !cfg.NATSAllowUnauthenticated {
		t.Fatal("cfg.NATSAllowUnauthenticated = false, want true")
	}
}

func TestLoad_AllowsUnauthenticatedNATSValidationWhenCredentialsPresent(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"credentials file", map[string]string{"NATS_CREDENTIALS_FILE": "/etc/nats/creds.creds"}},
		{"username", map[string]string{"NATS_USERNAME": "sentinel5g"}},
		{"tls cert", map[string]string{"NATS_TLS_CERT_FILE": "/etc/nats/tls.crt"}},
		{"tls ca", map[string]string{"NATS_TLS_CA_FILE": "/etc/nats/ca.crt"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearNATSAuthEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			if _, err := Load(); err != nil {
				t.Fatalf("Load() with %s set = %v, want no error", tc.name, err)
			}
		})
	}
}

func TestLoad_RejectsThreatScoreThresholdOutOfRange(t *testing.T) {
	clearNATSAuthEnv(t)
	t.Setenv("NATS_ALLOW_UNAUTHENTICATED", "true")
	t.Setenv("THREAT_SCORE_THRESHOLD", "1.5")

	if _, err := Load(); err == nil {
		t.Fatal("Load() with THREAT_SCORE_THRESHOLD=1.5 = nil error, want an error")
	}
}
