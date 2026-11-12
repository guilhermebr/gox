//go:build integration

package postgres_test

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"
)

// sseReplica runs one app of a fleet: its own HTTP port, the shared
// database, updates fanned out with postgres.SSETransport.
func sseReplica(t *testing.T) (base string, hub *gox.SSEHub) {
	t.Helper()
	setArgs(t)
	t.Setenv("BILLING_POSTGRES_URL", databaseURL(t))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	t.Setenv("BILLING_HTTP_ADDR", addr)

	a, err := newApp(t, postgres.Enable(), gox.HTTP(gox.WithSSE(gox.SSEConfig{Transport: postgres.SSETransport})))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	waitReady(t, a)
	return "http://" + addr, a.SSE()
}

// sseData streams the data: lines of an SSE subscription.
func sseData(t *testing.T, url string) <-chan string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	out := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if d, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				out <- d
			}
		}
	}()
	return out
}

// publishUntilSeen publishes on hub until data arrives: listeners connect
// in the background, and NOTIFY reaches only those already listening.
func publishUntilSeen(t *testing.T, hub *gox.SSEHub, got <-chan string, data string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		if _, err := hub.Publish(context.Background(), gox.SSEUpdate{Topics: []string{"/orders/1"}, Data: data}); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		select {
		case d := <-got:
			if d == data {
				return
			}
		case <-time.After(100 * time.Millisecond):
		case <-deadline:
			t.Fatalf("update %q never reached the subscriber", data)
		}
	}
}

func TestIntegrationSSEFansOutAcrossReplicasAndSurvivesALostListener(t *testing.T) {
	baseA, _ := sseReplica(t)
	_, hubB := sseReplica(t)
	got := sseData(t, baseA+"/events?topic=/orders/{id}")

	publishUntilSeen(t, hubB, got, `{"status":"paid"}`)

	pool, err := pgxpool.New(context.Background(), databaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tag, err := pool.Exec(context.Background(),
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE query LIKE 'LISTEN %gox_sse_billing%'")
	if err != nil || tag.RowsAffected() == 0 {
		t.Fatalf("terminate listeners: %v (rows %d)", err, tag.RowsAffected())
	}
	// Once A's listener is back, its subscriber is told it may have missed
	// updates (a resync carries "{}").
	for resynced := false; !resynced; {
		select {
		case d := <-got:
			resynced = d == "{}"
		case <-time.After(10 * time.Second):
			t.Fatal("no resync after the listener came back")
		}
	}
	publishUntilSeen(t, hubB, got, `{"status":"shipped"}`)
}

func TestIntegrationSSERefusesUpdatesNotifyCannotCarry(t *testing.T) {
	_, hub := sseReplica(t)
	_, err := hub.Publish(context.Background(), gox.SSEUpdate{Topics: []string{"/a"}, Data: strings.Repeat("x", 9000)})
	if !errors.Is(err, postgres.ErrSSETooLarge) {
		t.Fatalf("err = %v, want ErrSSETooLarge", err)
	}
}
