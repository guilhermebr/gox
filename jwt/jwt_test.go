package jwt_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/jwt"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func setArgs(t *testing.T) {
	t.Helper()
	old := os.Args
	os.Args = []string{"svc"}
	t.Cleanup(func() { os.Args = old })
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func rsaPair(t *testing.T) (*rsa.PrivateKey, string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return key, string(privPEM), string(pubPEM)
}

func TestHS256RoundTrip(t *testing.T) {
	svc := jwt.NewHS256([]byte("test-secret-at-least-32-bytes-long"), "billing", time.Hour)
	token, err := svc.GenerateToken("u1", "u1@example.com", "admin")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != "u1" || claims.Email != "u1@example.com" || claims.AccountType != "admin" || claims.Issuer != "billing" {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.ExpiresAt == nil || time.Until(claims.ExpiresAt.Time) > time.Hour {
		t.Fatalf("expiry = %v", claims.ExpiresAt)
	}
}

func TestHS256RejectsWrongSecretAndGarbage(t *testing.T) {
	a := jwt.NewHS256([]byte("secret-a-secret-a-secret-a-secret"), "x", time.Hour)
	b := jwt.NewHS256([]byte("secret-b-secret-b-secret-b-secret"), "x", time.Hour)
	token, _ := a.GenerateToken("u", "e", "t")
	if _, err := b.ValidateToken(token); !errors.Is(err, jwt.ErrInvalidToken) {
		t.Fatalf("wrong secret: err = %v", err)
	}
	if _, err := a.ValidateToken("not.a.token"); !errors.Is(err, jwt.ErrInvalidToken) {
		t.Fatalf("garbage: err = %v", err)
	}
}

func TestRS256RoundTripAndVerifyOnlyService(t *testing.T) {
	key, _, _ := rsaPair(t)
	signer := jwt.NewRS256(key, &key.PublicKey, "billing", time.Hour)
	verifier := jwt.NewRS256(nil, &key.PublicKey, "billing", time.Hour)

	token, err := signer.GenerateToken("u2", "u2@example.com", "user")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := verifier.ValidateToken(token)
	if err != nil || claims.UserID != "u2" {
		t.Fatalf("verify: claims=%+v err=%v", claims, err)
	}
	if _, err := verifier.GenerateToken("u", "e", "t"); err == nil {
		t.Fatal("a verify-only service must refuse to sign")
	}
}

func TestAlgorithmConfusionIsRejected(t *testing.T) {
	key, _, _ := rsaPair(t)
	hs := jwt.NewHS256([]byte("shared-secret-shared-secret-shared"), "x", time.Hour)
	rs := jwt.NewRS256(key, &key.PublicKey, "x", time.Hour)
	hsToken, _ := hs.GenerateToken("u", "e", "t")
	rsToken, _ := rs.GenerateToken("u", "e", "t")
	if _, err := rs.ValidateToken(hsToken); err == nil {
		t.Fatal("RS256 service accepted an HS256 token")
	}
	if _, err := hs.ValidateToken(rsToken); err == nil {
		t.Fatal("HS256 service accepted an RS256 token")
	}
}

func TestRefreshOnlyNearExpiry(t *testing.T) {
	fresh := jwt.NewHS256([]byte("refresh-secret-refresh-secret-refr"), "x", time.Hour)
	token, _ := fresh.GenerateToken("u", "e", "t")
	same, err := fresh.RefreshToken(token)
	if err != nil || same != token {
		t.Fatalf("fresh token should be returned unchanged: %v", err)
	}
	near := jwt.NewHS256([]byte("refresh-secret-refresh-secret-refr"), "x", 2*time.Minute)
	token, _ = near.GenerateToken("u", "e", "t")
	renewed, err := near.RefreshToken(token)
	if err != nil || renewed == token {
		t.Fatalf("token near expiry should be renewed: err=%v same=%v", err, renewed == token)
	}
}

func TestEnableRequiresASecretOrAPublicKey(t *testing.T) {
	setArgs(t)
	_, err := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()), jwt.Enable())
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"JWT_SECRET_KEY", "JWT_PUBLIC_KEY", "jwt.Enable()"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err %q lacks %q", err, want)
		}
	}
}

func TestEnableBuildsHS256FromConfig(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_JWT_SECRET_KEY", "config-secret-config-secret-config")
	t.Setenv("BILLING_JWT_ISSUER", "billing-api")
	t.Setenv("BILLING_JWT_EXPIRY", "2h")
	a, err := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()), jwt.Enable())
	if err != nil {
		t.Fatal(err)
	}
	svc := jwt.From(a)
	token, _ := svc.GenerateToken("u", "e", "t")
	claims, err := svc.ValidateToken(token)
	if err != nil || claims.Issuer != "billing-api" {
		t.Fatalf("claims=%+v err=%v", claims, err)
	}
}

func TestEnableBuildsRS256FromPEMConfig(t *testing.T) {
	setArgs(t)
	_, privPEM, pubPEM := rsaPair(t)
	t.Setenv("BILLING_JWT_PUBLIC_KEY", pubPEM)
	t.Setenv("BILLING_JWT_PRIVATE_KEY", privPEM)
	a, err := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()), jwt.Enable())
	if err != nil {
		t.Fatal(err)
	}
	svc := jwt.From(a)
	token, err := svc.GenerateToken("u", "e", "t")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateToken(token); err != nil {
		t.Fatal(err)
	}
}

func TestEnableRejectsBadPEM(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_JWT_PUBLIC_KEY", "not a pem")
	_, err := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()), jwt.Enable())
	if err == nil || !strings.Contains(err.Error(), "JWT_PUBLIC_KEY") {
		t.Fatalf("err = %v", err)
	}
}

func TestFromPanicsWithoutEnable(t *testing.T) {
	setArgs(t)
	a, _ := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()))
	defer func() {
		want := "gox: jwt.From called but jwt.Enable() was not passed to gox.New"
		if r := recover(); r != want {
			t.Fatalf("panic = %v", r)
		}
	}()
	jwt.From(a)
}

func TestAuthMiddleware(t *testing.T) {
	svc := jwt.NewHS256([]byte("auth-secret-auth-secret-auth-secre"), "x", time.Hour)
	var got *jwt.Claims
	h := jwt.Auth(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = jwt.ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("missing header is a 401 envelope", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"code":"unauthenticated"`) {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})
	t.Run("bad token is a 401 envelope", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer nope")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})
	t.Run("valid token puts claims in the context", func(t *testing.T) {
		token, _ := svc.GenerateToken("u9", "e", "admin")
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent || got == nil || got.UserID != "u9" {
			t.Fatalf("code=%d claims=%+v", rec.Code, got)
		}
	})
}

func TestEnableWithAuthProtectsEveryRouteButHealth(t *testing.T) {
	setArgs(t)
	addr := freeAddr(t)
	t.Setenv("BILLING_HTTP_ADDR", addr)
	t.Setenv("BILLING_JWT_SECRET_KEY", "config-secret-config-secret-config")
	a, err := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()), gox.HTTP(), jwt.Enable(jwt.WithAuth()))
	if err != nil {
		t.Fatal(err)
	}
	a.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		c, _ := jwt.ClaimsFromContext(r.Context())
		_, _ = w.Write([]byte(c.UserID))
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	for !a.Health().IsReady() {
		time.Sleep(5 * time.Millisecond)
	}
	defer func() {
		cancel()
		<-done
	}()

	resp, _ := http.Get("http://" + addr + "/me")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token = %d", resp.StatusCode)
	}
	resp, _ = http.Get("http://" + addr + "/readyz")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health probe must bypass auth, got %d", resp.StatusCode)
	}
	token, _ := jwt.From(a).GenerateToken("u7", "e", "t")
	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "u7" {
		t.Fatalf("with token = %d %s", resp.StatusCode, body)
	}
}
