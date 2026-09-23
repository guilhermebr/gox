package temporal

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

// testCA returns a self-signed CA certificate in PEM, generated for this test.
func testCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestTLSConfigAppliesCAAndServerName(t *testing.T) {
	c := &Config{Address: "temporal.example:7233", TLS: "true", TLSCA: testCA(t), TLSServerName: "temporal.internal"}
	got := c.tlsConfig()
	if got == nil {
		t.Fatal("tlsConfig = nil, want a config")
	}
	if got.RootCAs == nil {
		t.Error("RootCAs not set from TEMPORAL_TLS_CA")
	}
	if got.ServerName != "temporal.internal" {
		t.Errorf("ServerName = %q, want temporal.internal", got.ServerName)
	}
	if got.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d", got.MinVersion)
	}
}

func TestTLSCAAloneTurnsTLSOnInAutoMode(t *testing.T) {
	c := &Config{Address: "temporal.example:7233", TLS: "auto", TLSCA: testCA(t)}
	if c.tlsConfig() == nil {
		t.Fatal("a CA alone must imply TLS in auto mode: it is only useful to verify a TLS server")
	}
}

func TestTLSConfigWithoutCAKeepsTheSystemPool(t *testing.T) {
	c := &Config{Address: "temporal.example:7233", TLS: "true"}
	got := c.tlsConfig()
	if got == nil {
		t.Fatal("tlsConfig = nil")
	}
	if got.RootCAs != nil {
		t.Error("RootCAs must stay nil so the system pool is used")
	}
	if got.ServerName != "" {
		t.Error("ServerName must stay empty so the address host is used")
	}
}

func TestValidateRejectsAMalformedCA(t *testing.T) {
	c := &Config{Address: "temporal.example:7233", TLS: "auto", TLSCA: "not a pem", ConnectTimeout: time.Second}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "TEMPORAL_TLS_CA") {
		t.Fatalf("Validate() = %v, want an error naming TEMPORAL_TLS_CA", err)
	}
}

func TestValidateAcceptsAWellFormedCA(t *testing.T) {
	c := &Config{Address: "temporal.example:7233", TLS: "auto", TLSCA: testCA(t), ConnectTimeout: time.Second}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}
