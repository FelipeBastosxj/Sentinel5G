package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/config"
)

// writeThrowawayCert generates a real, throwaway self-signed cert/key pair
// (not fixture files -- new key material per test run) and writes them as
// PEM files under t.TempDir(), returning their paths. Used to exercise
// hubbleTLSConfig's actual PEM parsing/pool-building/key-pairing logic, not
// just its zero/non-zero branching.
func writeThrowawayCert(t *testing.T) (certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "hubble-tls-test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath
}

func TestHubbleTLSConfig_AllEmptyReturnsNil(t *testing.T) {
	got := hubbleTLSConfig(testr.New(t), config.OperatorConfig{})
	if got != nil {
		t.Fatalf("expected nil *tls.Config when no HUBBLE_TLS_* is set, got %+v", got)
	}
}

func TestHubbleTLSConfig_CAFileOnlyBuildsRootCAPool(t *testing.T) {
	certPath, _ := writeThrowawayCert(t)

	got := hubbleTLSConfig(testr.New(t), config.OperatorConfig{HubbleTLSCAFile: certPath})
	if got == nil {
		t.Fatal("expected a non-nil *tls.Config when HubbleTLSCAFile is set")
	}
	if got.RootCAs == nil {
		t.Fatal("expected RootCAs to be populated from HubbleTLSCAFile")
	}
	if len(got.Certificates) != 0 {
		t.Fatalf("expected no client certificates when only a CA file is set, got %d", len(got.Certificates))
	}
}

func TestHubbleTLSConfig_CertAndKeyBuildsClientCertificate(t *testing.T) {
	certPath, keyPath := writeThrowawayCert(t)

	got := hubbleTLSConfig(testr.New(t), config.OperatorConfig{
		HubbleTLSCertFile: certPath,
		HubbleTLSKeyFile:  keyPath,
	})
	if got == nil {
		t.Fatal("expected a non-nil *tls.Config when HubbleTLSCertFile/HubbleTLSKeyFile are set")
	}
	if len(got.Certificates) != 1 {
		t.Fatalf("expected exactly one client certificate, got %d", len(got.Certificates))
	}
	if got.RootCAs != nil {
		t.Fatal("expected RootCAs to stay nil when no CA file is set")
	}
}

func TestHubbleTLSConfig_MinTLSVersionIsAtLeast12(t *testing.T) {
	certPath, _ := writeThrowawayCert(t)
	got := hubbleTLSConfig(testr.New(t), config.OperatorConfig{HubbleTLSCAFile: certPath})
	if got.MinVersion < 0x0303 { // tls.VersionTLS12
		t.Fatalf("expected MinVersion >= TLS 1.2, got %#x", got.MinVersion)
	}
}
