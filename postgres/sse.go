package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/guilhermebr/gox"
)

// SSETransport fans gox.WithSSE updates out across replicas with
// LISTEN/NOTIFY on the pool Enable built. Pass it as SSEConfig.Transport:
//
//	gox.HTTP(gox.WithSSE(gox.SSEConfig{Transport: postgres.SSETransport}))
//
// The channel is derived from the service name, so services sharing a
// database stay apart. Each replica holds one extra connection for LISTEN
// and reconnects with backoff if it drops; its subscribers then get a
// gox.SSEResyncEvent, since updates from other replicas sent meanwhile are
// lost to it. LISTEN needs a session: POSTGRES_URL must point at the
// database or at a session-mode pooler, not a transaction-mode one
// (PgBouncer in transaction mode, Supabase's port 6543), where LISTEN
// succeeds but nothing arrives; the listener probes for this on every
// connect and logs an error when its probe does not come back. NOTIFY
// carries about 7.9 KB, so Publish returns ErrSSETooLarge for bigger
// updates.
func SSETransport(a *gox.App) (gox.SSETransport, error) {
	return &sseTransport{
		pool:    From(a),
		channel: channelName(a.Name()),
		log:     a.Log().With(slog.String("component", "sse")),
	}, nil
}

// maxNotifyPayload is Postgres's NOTIFY limit (8000 bytes) minus headroom.
const maxNotifyPayload = 7900

// probeTimeout is how long the listener waits for its own probe before
// concluding notifications do not reach it.
const probeTimeout = 10 * time.Second

// ErrSSETooLarge is returned by SSETransport for an update whose encoded
// form exceeds what NOTIFY carries (about 7.9 KB). Publish an ID or a URL
// and let clients fetch the body.
var ErrSSETooLarge = errors.New("postgres: sse update too large for NOTIFY")

type sseTransport struct {
	pool    *pgxpool.Pool
	channel string
	log     *slog.Logger
}

// channelName derives the NOTIFY channel from the service name so two
// services sharing a database do not see each other's updates: a readable
// prefix plus a hash of the exact name, since sanitizing alone would map
// billing-api and billing_api to the same channel.
func channelName(service string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(service) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	readable := b.String()
	// Postgres truncates identifiers at 63 bytes: 8 for "gox_sse_", 9 for
	// "_" and the hash, 46 for the readable part.
	if len(readable) > 46 {
		readable = readable[:46]
	}
	sum := sha256.Sum256([]byte(service))
	return "gox_sse_" + readable + "_" + hex.EncodeToString(sum[:4])
}

// notification is what travels on the channel: an update, or the probe a
// listener sends itself to check that notifications reach it.
type notification struct {
	gox.SSEUpdate
	Probe string `json:"probe,omitempty"`
}

func (t *sseTransport) Publish(ctx context.Context, u gox.SSEUpdate) error {
	payload, err := json.Marshal(notification{SSEUpdate: u})
	if err != nil {
		return fmt.Errorf("postgres: sse encode: %w", err)
	}
	if len(payload) > maxNotifyPayload {
		return fmt.Errorf("%w: %d bytes encoded, limit %d", ErrSSETooLarge, len(payload), maxNotifyPayload)
	}
	return t.notify(ctx, payload)
}

func (t *sseTransport) notify(ctx context.Context, payload []byte) error {
	if _, err := t.pool.Exec(ctx, "SELECT pg_notify($1, $2)", t.channel, string(payload)); err != nil {
		return fmt.Errorf("postgres: sse notify: %w", err)
	}
	return nil
}

func (t *sseTransport) Run(ctx context.Context, deliver func(gox.SSEUpdate), resync func()) error {
	const maxBackoff = 30 * time.Second
	backoff := 500 * time.Millisecond
	for {
		listened, err := t.listen(ctx, deliver, resync)
		if ctx.Err() != nil {
			return nil
		}
		if listened {
			backoff = 500 * time.Millisecond
		}
		t.log.Warn("sse listener lost; reconnecting, subscribers will be told to resync",
			slog.String("error", err.Error()), slog.Duration("retry_in", backoff))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

// listen holds a dedicated connection (not one of the pool's) on LISTEN and
// delivers notifications until it fails. listened reports whether LISTEN
// succeeded, which resets the reconnect backoff.
func (t *sseTransport) listen(ctx context.Context, deliver func(gox.SSEUpdate), resync func()) (listened bool, err error) {
	conn, err := pgx.ConnectConfig(ctx, t.pool.Config().ConnConfig.Copy())
	if err != nil {
		return false, fmt.Errorf("postgres: sse connect: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{t.channel}.Sanitize()); err != nil {
		return false, fmt.Errorf("postgres: sse listen: %w", err)
	}
	// Whatever was sent before LISTEN took effect is lost to this replica.
	resync()

	// The probe's watchdog lives only as long as this connection.
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	probe, arrived := t.sendProbe(probeCtx)
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, fmt.Errorf("postgres: sse wait: %w", err)
		}
		var m notification
		if err := json.Unmarshal([]byte(n.Payload), &m); err != nil {
			t.log.Error("sse dropped an undecodable notification", slog.String("error", err.Error()))
			continue
		}
		if m.Probe != "" {
			if m.Probe == probe && arrived != nil {
				close(arrived)
				arrived = nil
			}
			continue
		}
		deliver(m.SSEUpdate)
	}
}

// sendProbe notifies the channel through the pool and logs an error unless
// the listener reports the probe back (by closing arrived) in time: behind
// a transaction-mode pooler LISTEN succeeds but notifications never arrive.
func (t *sseTransport) sendProbe(ctx context.Context) (probe string, arrived chan struct{}) {
	var b [8]byte
	_, _ = rand.Read(b[:])
	probe = hex.EncodeToString(b[:])
	payload, _ := json.Marshal(notification{Probe: probe})
	if err := t.notify(ctx, payload); err != nil {
		t.log.Warn("sse could not probe the listener", slog.String("error", err.Error()))
		return probe, nil
	}
	arrived = make(chan struct{})
	go func() {
		timer := time.NewTimer(probeTimeout)
		defer timer.Stop()
		select {
		case <-arrived:
		case <-ctx.Done():
		case <-timer.C:
			t.log.Error("sse listener receives no notifications; is POSTGRES_URL a transaction-mode pooler? LISTEN needs a direct or session-mode connection",
				slog.String("channel", t.channel))
		}
	}()
	return probe, arrived
}
