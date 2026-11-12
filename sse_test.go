package gox_test

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guilhermebr/gox"
)

// runSSE runs an app with HTTP(WithSSE(cfg)). The request, read and write
// timeouts are short so the tests prove the stream is exempt from them.
func runSSE(t *testing.T, cfg gox.SSEConfig) (base string, hub *gox.SSEHub, stop func() error) {
	t.Helper()
	t.Setenv("BILLING_HTTP_REQUEST_TIMEOUT", "200ms")
	t.Setenv("BILLING_HTTP_READ_TIMEOUT", "200ms")
	t.Setenv("BILLING_HTTP_WRITE_TIMEOUT", "200ms")
	base, stopRun := runHTTP(t, func(a *gox.App) { hub = a.SSE() }, []gox.HTTPOption{gox.WithSSE(cfg)})
	var once sync.Once
	stop = func() error {
		once.Do(stopRun)
		return nil
	}
	t.Cleanup(func() { _ = stop() })
	return base, hub, stop
}

type event struct{ id, name, data string }

type client struct {
	t    *testing.T
	resp *http.Response
	r    *bufio.Reader
}

func subscribe(t *testing.T, base string, header http.Header, topics ...string) *client {
	t.Helper()
	q := url.Values{"topic": topics}
	req, _ := http.NewRequest(http.MethodGet, base+"/events?"+q.Encode(), nil)
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("subscribe = %d %s", resp.StatusCode, b)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
	c := &client{t: t, resp: resp, r: bufio.NewReader(resp.Body)}
	if line, err := c.r.ReadString('\n'); err != nil || line != ":\n" {
		t.Fatalf("opening comment = %q, %v", line, err)
	}
	if line, err := c.r.ReadString('\n'); err != nil || line != "\n" {
		t.Fatalf("opening comment end = %q, %v", line, err)
	}
	return c
}

// next returns the next event, skipping comments. ok is false at the end of
// the stream.
func (c *client) next() (ev event, ok bool) {
	c.t.Helper()
	var data []string
	got := false
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return ev, false
		}
		line = strings.TrimSuffix(line, "\n")
		switch {
		case line == "":
			if got {
				ev.data = strings.Join(data, "\n")
				return ev, true
			}
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "id: "):
			ev.id, got = line[4:], true
		case strings.HasPrefix(line, "event: "):
			ev.name, got = line[7:], true
		case strings.HasPrefix(line, "data: "):
			data, got = append(data, line[6:]), true
		}
	}
}

func (c *client) mustNext() event {
	c.t.Helper()
	ev, ok := c.next()
	if !ok {
		c.t.Fatal("stream ended")
	}
	return ev
}

func publish(t *testing.T, h *gox.SSEHub, u gox.SSEUpdate) string {
	t.Helper()
	id, err := h.Publish(context.Background(), u)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return id
}

func TestSSESubscriberReceivesUpdatesForItsTopicsOnly(t *testing.T) {
	base, h, _ := runSSE(t, gox.SSEConfig{})
	c := subscribe(t, base, nil, "/orders/1")

	publish(t, h, gox.SSEUpdate{Topics: []string{"/orders/2"}, Data: "other"})
	id := publish(t, h, gox.SSEUpdate{Topics: []string{"/orders/1"}, Event: "order.paid", Data: "line one\nline two"})
	time.Sleep(300 * time.Millisecond) // past HTTP_REQUEST_TIMEOUT: the stream must be exempt
	publish(t, h, gox.SSEUpdate{Topics: []string{"/x", "/orders/1"}, Data: "late", ID: "custom-id"})

	ev := c.mustNext()
	if ev.id != id || id == "" || ev.name != "order.paid" || ev.data != "line one\nline two" {
		t.Fatalf("event = %+v, want id %q", ev, id)
	}
	if ev := c.mustNext(); ev.id != "custom-id" || ev.data != "late" || ev.name != "" {
		t.Fatalf("event = %+v", ev)
	}
}

func TestSSETemplatesAndWildcardSelectTopics(t *testing.T) {
	base, h, _ := runSSE(t, gox.SSEConfig{})
	orders := subscribe(t, base, nil, "/orders/{id}")
	all := subscribe(t, base, nil, "*")
	rest := subscribe(t, base, nil, "/files/{path...}")

	publish(t, h, gox.SSEUpdate{Topics: []string{"/users/1"}, Data: "user"})
	publish(t, h, gox.SSEUpdate{Topics: []string{"/orders/1/items"}, Data: "item"})
	publish(t, h, gox.SSEUpdate{Topics: []string{"/files/a/b.txt"}, Data: "file"})
	publish(t, h, gox.SSEUpdate{Topics: []string{"/orders/7"}, Data: "order"})

	if ev := orders.mustNext(); ev.data != "order" {
		t.Fatalf("orders got %q, want order", ev.data)
	}
	if ev := rest.mustNext(); ev.data != "file" {
		t.Fatalf("files got %q, want file", ev.data)
	}
	for _, want := range []string{"user", "item", "file", "order"} {
		if ev := all.mustNext(); ev.data != want {
			t.Fatalf("* got %q, want %q", ev.data, want)
		}
	}
}

func TestSSEPrivateUpdatesNeedAuthorization(t *testing.T) {
	base, h, _ := runSSE(t, gox.SSEConfig{Authorize: func(r *http.Request, topic string) bool {
		return r.Header.Get("X-User") == "ana" && topic == "/users/ana"
	}})
	ana := subscribe(t, base, http.Header{"X-User": {"ana"}}, "/users/{id}")
	bob := subscribe(t, base, http.Header{"X-User": {"bob"}}, "/users/{id}")

	publish(t, h, gox.SSEUpdate{Topics: []string{"/users/ana"}, Data: "secret", Private: true})
	publish(t, h, gox.SSEUpdate{Topics: []string{"/users/bob"}, Data: "bob-secret", Private: true})
	publish(t, h, gox.SSEUpdate{Topics: []string{"/users/ana"}, Data: "public"})

	if ev := ana.mustNext(); ev.data != "secret" {
		t.Fatalf("ana got %q, want secret", ev.data)
	}
	if ev := ana.mustNext(); ev.data != "public" {
		t.Fatalf("ana got %q, want public (bob's private update must be skipped)", ev.data)
	}
	if ev := bob.mustNext(); ev.data != "public" {
		t.Fatalf("bob got %q, want public (private updates must be skipped)", ev.data)
	}
}

func TestSSEPrivateUpdatesReachNoOneWithoutAuthorize(t *testing.T) {
	base, h, _ := runSSE(t, gox.SSEConfig{})
	c := subscribe(t, base, nil, "*")
	publish(t, h, gox.SSEUpdate{Topics: []string{"/a"}, Data: "secret", Private: true})
	publish(t, h, gox.SSEUpdate{Topics: []string{"/a"}, Data: "public"})
	if ev := c.mustNext(); ev.data != "public" {
		t.Fatalf("got %q, want public", ev.data)
	}
}

func TestSSEReconnectWithLastEventIDReplaysMissedUpdates(t *testing.T) {
	base, h, _ := runSSE(t, gox.SSEConfig{})
	first := publish(t, h, gox.SSEUpdate{Topics: []string{"/a"}, Data: "1"})
	publish(t, h, gox.SSEUpdate{Topics: []string{"/b"}, Data: "not mine"})
	publish(t, h, gox.SSEUpdate{Topics: []string{"/a"}, Data: "2"})
	publish(t, h, gox.SSEUpdate{Topics: []string{"/a"}, Data: "3"})

	c := subscribe(t, base, http.Header{"Last-Event-ID": {first}}, "/a")
	publish(t, h, gox.SSEUpdate{Topics: []string{"/a"}, Data: "4"})
	for _, want := range []string{"2", "3", "4"} {
		if ev := c.mustNext(); ev.data != want {
			t.Fatalf("got %q, want %q", ev.data, want)
		}
	}
}

func TestSSEHeartbeatKeepsTheStreamAlive(t *testing.T) {
	base, _, _ := runSSE(t, gox.SSEConfig{Heartbeat: 20 * time.Millisecond})
	c := subscribe(t, base, nil, "/a")
	if line, err := c.r.ReadString('\n'); err != nil || line != ":\n" {
		t.Fatalf("heartbeat = %q, %v", line, err)
	}
}

func TestSSEBadSubscriptionsAreRejected(t *testing.T) {
	base, _, _ := runSSE(t, gox.SSEConfig{MaxTopics: 2})
	for name, query := range map[string]string{
		"no topic":         "",
		"partial template": "?topic=/orders/x{id}",
		"rest not last":    "?topic=/{rest...}/x",
		"too many":         "?topic=/a&topic=/b&topic=/c",
	} {
		resp, err := http.Get(base + "/events" + query)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, resp.StatusCode)
		}
	}
}

func TestSSEPublishValidatesTheUpdate(t *testing.T) {
	_, h, _ := runSSE(t, gox.SSEConfig{})
	for name, u := range map[string]gox.SSEUpdate{
		"no topics":       {Data: "x"},
		"empty topic":     {Topics: []string{""}},
		"newline in type": {Topics: []string{"/a"}, Event: "a\nb"},
		"newline in id":   {Topics: []string{"/a"}, ID: "a\rb"},
	} {
		if _, err := h.Publish(context.Background(), u); err == nil {
			t.Errorf("%s: Publish succeeded", name)
		}
	}
}

func TestSSEShutdownEndsStreamsAndDrainsPromptly(t *testing.T) {
	base, _, stop := runSSE(t, gox.SSEConfig{})
	c := subscribe(t, base, nil, "/a")
	began := time.Now()
	if err := stop(); err != nil {
		t.Fatalf("RunContext = %v", err)
	}
	if d := time.Since(began); d > 2*time.Second {
		t.Fatalf("shutdown took %v; open streams must not hold the server", d)
	}
	if _, ok := c.next(); ok {
		t.Fatal("stream still open after shutdown")
	}
}

type loopback struct {
	mu        sync.Mutex
	deliver   func(gox.SSEUpdate)
	resync    func()
	published int
	ready     chan struct{}
}

func (l *loopback) Publish(_ context.Context, u gox.SSEUpdate) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.published++
	if l.deliver != nil {
		l.deliver(u)
	}
	return nil
}

func (l *loopback) Run(ctx context.Context, deliver func(gox.SSEUpdate), resync func()) error {
	l.mu.Lock()
	l.deliver, l.resync = deliver, resync
	l.mu.Unlock()
	close(l.ready)
	<-ctx.Done()
	return nil
}

// fromReplica delivers u as if another replica had published it.
func (l *loopback) fromReplica(u gox.SSEUpdate) {
	<-l.ready
	l.mu.Lock()
	defer l.mu.Unlock()
	l.deliver(u)
}

func (l *loopback) lost() {
	<-l.ready
	l.mu.Lock()
	defer l.mu.Unlock()
	l.resync()
}

func TestSSEPublishGoesThroughTheTransport(t *testing.T) {
	tr := &loopback{ready: make(chan struct{})}
	base, h, _ := runSSE(t, gox.SSEConfig{Transport: func(*gox.App) (gox.SSETransport, error) { return tr, nil }})
	c := subscribe(t, base, nil, "/a")
	publish(t, h, gox.SSEUpdate{Topics: []string{"/a"}, Data: "via transport"})
	if ev := c.mustNext(); ev.data != "via transport" {
		t.Fatalf("got %q", ev.data)
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.published != 1 {
		t.Fatalf("transport saw %d publishes, want 1", tr.published)
	}
}

func TestSSEUpdatesFromOtherReplicasArriveAndOwnEchoesDoNot(t *testing.T) {
	tr := &loopback{ready: make(chan struct{})}
	base, h, _ := runSSE(t, gox.SSEConfig{Transport: func(*gox.App) (gox.SSETransport, error) { return tr, nil }})
	c := subscribe(t, base, nil, "/a")
	tr.fromReplica(gox.SSEUpdate{Topics: []string{"/a"}, ID: "r1", Data: "remote", Origin: "other-replica"})
	if ev := c.mustNext(); ev.id != "r1" || ev.data != "remote" {
		t.Fatalf("got %+v", ev)
	}
	// The loopback echoes every publish back, as Postgres does; the
	// subscriber must see the update once.
	publish(t, h, gox.SSEUpdate{Topics: []string{"/a"}, Data: "local"})
	publish(t, h, gox.SSEUpdate{Topics: []string{"/a"}, Data: "next"})
	for _, want := range []string{"local", "next"} {
		if ev := c.mustNext(); ev.data != want {
			t.Fatalf("got %q, want %q", ev.data, want)
		}
	}
}

func TestSSEAPublishReachesLocalSubscribersEvenIfTheTransportDoesNotEcho(t *testing.T) {
	tr := &sink{}
	base, h, _ := runSSE(t, gox.SSEConfig{Transport: func(*gox.App) (gox.SSETransport, error) { return tr, nil }})
	c := subscribe(t, base, nil, "/a")
	publish(t, h, gox.SSEUpdate{Topics: []string{"/a"}, Data: "while the listener is down"})
	if ev := c.mustNext(); ev.data != "while the listener is down" {
		t.Fatalf("got %q", ev.data)
	}
}

// sink takes publishes and never delivers: a listener that is down.
type sink struct{}

func (sink) Publish(context.Context, gox.SSEUpdate) error { return nil }
func (sink) Run(ctx context.Context, _ func(gox.SSEUpdate), _ func()) error {
	<-ctx.Done()
	return nil
}

func TestSSEALostTransportTellsSubscribersToResync(t *testing.T) {
	tr := &loopback{ready: make(chan struct{})}
	base, h, _ := runSSE(t, gox.SSEConfig{Transport: func(*gox.App) (gox.SSETransport, error) { return tr, nil }})
	before := publish(t, h, gox.SSEUpdate{Topics: []string{"/a"}, Data: "before"})
	c := subscribe(t, base, nil, "/a")
	tr.lost()
	ev := c.mustNext()
	if ev.name != gox.SSEResyncEvent || ev.id == "" || ev.data != "{}" {
		t.Fatalf("event = %+v, want a resync", ev)
	}
	// The history has a hole now: resuming from before it resyncs too,
	// while resuming from the resync's ID replays from there.
	c2 := subscribe(t, base, http.Header{"Last-Event-ID": {before}}, "/a")
	if ev := c2.mustNext(); ev.name != gox.SSEResyncEvent {
		t.Fatalf("resume from before the loss = %+v, want a resync", ev)
	}
	publish(t, h, gox.SSEUpdate{Topics: []string{"/a"}, Data: "after"})
	c3 := subscribe(t, base, http.Header{"Last-Event-ID": {ev.id}}, "/a")
	if ev := c3.mustNext(); ev.data != "after" {
		t.Fatalf("resume from the resync = %+v, want after", ev)
	}
}

func TestSSEAnUnknownLastEventIDGetsAResync(t *testing.T) {
	base, h, _ := runSSE(t, gox.SSEConfig{})
	c := subscribe(t, base, http.Header{"Last-Event-ID": {"from-before-a-deploy"}}, "/a")
	ev := c.mustNext()
	if ev.name != gox.SSEResyncEvent || ev.id == "" {
		t.Fatalf("event = %+v, want a resync", ev)
	}
	publish(t, h, gox.SSEUpdate{Topics: []string{"/a"}, Data: "x"})
	c2 := subscribe(t, base, http.Header{"Last-Event-ID": {ev.id}}, "/a")
	if ev := c2.mustNext(); ev.data != "x" {
		t.Fatalf("resume from the resync = %+v", ev)
	}
}

func TestSSEStreamsEndAtMaxAge(t *testing.T) {
	base, _, _ := runSSE(t, gox.SSEConfig{MaxAge: 50 * time.Millisecond})
	c := subscribe(t, base, nil, "/a")
	began := time.Now()
	if _, ok := c.next(); ok {
		t.Fatal("got an event, want the stream to end")
	}
	if d := time.Since(began); d > 2*time.Second {
		t.Fatalf("stream lasted %v", d)
	}
}

func TestSSERefusesSubscribersBeyondMaxSubscribers(t *testing.T) {
	base, _, _ := runSSE(t, gox.SSEConfig{MaxSubscribers: 1})
	subscribe(t, base, nil, "/a")
	resp, err := http.Get(base + "/events?topic=/a")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("second subscriber = %d Retry-After %q, want 503 with Retry-After", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
}

func TestSSEOtherRoutesKeepTheRequestTimeout(t *testing.T) {
	t.Setenv("BILLING_HTTP_REQUEST_TIMEOUT", "50ms")
	base, stop := runHTTP(t, func(a *gox.App) {
		a.HandleFunc("GET /slow", func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	}, []gox.HTTPOption{gox.WithSSE(gox.SSEConfig{})})
	defer stop()
	resp, _ := do(t, http.MethodGet, base+"/slow", nil)
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("slow = %d, want 504", resp.StatusCode)
	}
}

func TestSSEPanicsWithoutWithSSE(t *testing.T) {
	setArgs(t)
	a, err := gox.New("billing", base(gox.HTTP())...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if p := recover(); p == nil || !strings.Contains(p.(string), "gox.WithSSE()") {
			t.Fatalf("panic = %v", p)
		}
	}()
	a.SSE()
}

func TestWithSSERejectsABadConfig(t *testing.T) {
	setArgs(t)
	_, err := gox.New("billing", base(gox.HTTP(gox.WithSSE(gox.SSEConfig{Path: "events"})))...)
	if err == nil || !strings.Contains(err.Error(), "WithSSE") || !strings.Contains(err.Error(), "Path") {
		t.Fatalf("err = %v", err)
	}
}
