// Package sse is a topic-based server-sent events hub in the spirit of
// Mercure: clients subscribe with GET ?topic=… (exact topics, {name}
// segment templates, or *), the service publishes Updates, private updates
// reach only the subscribers Authorize allows, and a reconnecting client
// resumes from Last-Event-ID. A Transport fans updates out across replicas.
package sse

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	mrand "math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	gerrors "github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/httpx"
)

// ResyncEvent is the event type a subscriber receives when updates meant
// for it are gone: its Last-Event-ID is no longer in the history (a
// restart, a deploy, a long absence), or the transport lost updates while
// reconnecting. The client should refetch the state it shows. Its data is
// "{}" and its ID is a valid Last-Event-ID for the next reconnect.
const ResyncEvent = "resync"

// authorizeTTL bounds how long a stream trusts an Authorize answer before
// asking again, so revoking access takes effect without a reconnect.
const authorizeTTL = time.Minute

// Config configures a Hub. Zero values take the defaults noted per field.
type Config struct {
	// Path subscribers connect to with GET ?topic=. Default: /events.
	Path string
	// Authorize decides which private updates a subscriber receives: an
	// update with Private set reaches the subscription made by r only if
	// Authorize returns true for one of its topics. It sees the request
	// with everything the middleware chain put there (claims, session);
	// answers are cached per topic for a minute. Without it, private
	// updates reach no one.
	Authorize func(r *http.Request, topic string) bool
	// Heartbeat is the interval of the keep-alive comment that stops
	// proxies closing idle streams. Default: 15s.
	Heartbeat time.Duration
	// WriteTimeout bounds each write to a subscriber; a client that stalls
	// longer is disconnected. Default: 10s.
	WriteTimeout time.Duration
	// MaxAge ends a stream after this long (less up to 10% jitter); the
	// client reconnects with Last-Event-ID and goes through the middleware
	// chain again, so an expired token or a closed session stops the
	// stream. Default: 15m; negative keeps streams open indefinitely.
	MaxAge time.Duration
	// Buffer is the number of updates queued per subscriber; one that falls
	// further behind is disconnected and resumes with Last-Event-ID.
	// Default: 64.
	Buffer int
	// History is the number of recent updates kept for Last-Event-ID
	// replay. Default: 1000; negative disables replay.
	History int
	// MaxTopics caps the topic selectors of one subscription. Default: 32.
	MaxTopics int
	// MaxSubscribers caps the open streams of this process; more are
	// refused with 503 and Retry-After. Default: 10000; negative means no
	// cap.
	MaxSubscribers int
}

func (c *Config) defaults() error {
	if c.Path == "" {
		c.Path = "/events"
	}
	if !strings.HasPrefix(c.Path, "/") || strings.ContainsAny(c.Path, " {}") {
		return fmt.Errorf("sse: Path must be a plain path starting with /, got %q", c.Path)
	}
	if c.Heartbeat == 0 {
		c.Heartbeat = 15 * time.Second
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 10 * time.Second
	}
	if c.MaxAge == 0 {
		c.MaxAge = 15 * time.Minute
	}
	if c.Buffer == 0 {
		c.Buffer = 64
	}
	switch {
	case c.History == 0:
		c.History = 1000
	case c.History < 0:
		c.History = 0
	}
	if c.MaxTopics == 0 {
		c.MaxTopics = 32
	}
	if c.MaxSubscribers == 0 {
		c.MaxSubscribers = 10000
	}
	if c.Heartbeat < 0 || c.WriteTimeout < 0 || c.Buffer < 0 || c.MaxTopics < 0 {
		return errors.New("sse: Heartbeat, WriteTimeout, Buffer and MaxTopics must not be negative")
	}
	return nil
}

// Transport carries published updates to every replica of the service.
// Without one, a Hub delivers only to the subscribers of its own process.
type Transport interface {
	// Publish sends u to every hub on the transport. The publishing hub
	// delivers u to its own subscribers itself and ignores the copy that
	// comes back; a transport must carry Update.Origin for that.
	Publish(ctx context.Context, u Update) error
	// Run hands every update published on the transport to deliver until
	// ctx is done. It should ride out outages by reconnecting, and call
	// resync once it is receiving again (the first time included), since
	// updates sent while it was not are lost to this replica. An error it
	// returns stops the service.
	Run(ctx context.Context, deliver func(Update), resync func()) error
}

// New returns a hub for cfg; t may be nil for a single process.
func New(cfg Config, t Transport, log *slog.Logger) (*Hub, error) {
	if err := cfg.defaults(); err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Hub{cfg: cfg, log: log, transport: t, origin: newID(), subs: make(map[*subscriber]struct{})}, nil
}

// Path returns the subscription path the hub serves.
func (h *Hub) Path() string { return h.cfg.Path }

// Run delivers transport updates until ctx is done, then ends every stream
// and refuses new ones, so an HTTP server can drain instead of waiting on
// connections that never finish.
func (h *Hub) Run(ctx context.Context) error {
	defer h.shutdown()
	if h.transport == nil {
		<-ctx.Done()
		return nil
	}
	return h.transport.Run(ctx, h.receive, h.resync)
}

// Update is one event published to the subscribers of its topics.
type Update struct {
	// Topics the update belongs to; at least one. A subscriber receives it
	// once if any of its selectors matches any of these.
	Topics []string `json:"topics"`
	// Data is the event payload, sent as data: lines (often JSON).
	Data string `json:"data"`
	// Event is the SSE event type the client listens for; empty means
	// "message".
	Event string `json:"event,omitempty"`
	// ID identifies the update for Last-Event-ID resumption. Publish
	// generates one when empty.
	ID string `json:"id"`
	// Private updates reach only subscribers Config.Authorize allows for
	// one of the topics; without Authorize they reach no one.
	Private bool `json:"private,omitempty"`
	// Origin identifies the hub that published the update; Publish sets
	// it, so a hub can skip its own updates when a transport echoes them.
	Origin string `json:"origin,omitempty"`
}

// control reports whether u is a hub-generated event (a resync) rather than
// a published update; published updates always have a topic.
func (u Update) control() bool { return len(u.Topics) == 0 }

var (
	// errClosed refuses subscriptions once the hub has shut down.
	errClosed = errors.New("sse: hub is shut down")
	// errFull refuses subscriptions beyond Config.MaxSubscribers.
	errFull = errors.New("sse: too many subscribers")
)

// Hub fans updates out to the subscribers connected to this process and,
// through its Transport, to the other replicas.
type Hub struct {
	cfg       Config
	log       *slog.Logger
	transport Transport
	origin    string // Update.Origin of what this hub publishes

	noDeadlines sync.Once // logs once that the writer cannot lift deadlines

	mu      sync.Mutex
	subs    map[*subscriber]struct{}
	history []Update // ring buffer of the last cfg.History updates
	head    int      // index of the oldest entry once the ring is full
	closed  bool
}

type subscriber struct {
	selectors []selector
	ch        chan Update
}

func (s *subscriber) matches(topics []topic) bool {
	for _, sel := range s.selectors {
		if slices.ContainsFunc(topics, sel.match) {
			return true
		}
	}
	return false
}

// Publish sends u to every subscriber of its topics, on every replica when
// a transport is configured, and returns its ID. This replica's
// subscribers and history always get u once the transport has taken it.
// Delivery is at most once: a subscriber that is disconnected misses the
// update unless it reconnects with Last-Event-ID while the update is still
// in the history, and gets a ResyncEvent otherwise.
func (h *Hub) Publish(ctx context.Context, u Update) (string, error) {
	if len(u.Topics) == 0 {
		return "", errors.New("sse: an update needs at least one topic")
	}
	u.Topics = append([]string(nil), u.Topics...)
	for _, t := range u.Topics {
		if t == "" {
			return "", errors.New("sse: empty topic")
		}
	}
	if strings.ContainsAny(u.Event, "\r\n") || strings.ContainsAny(u.ID, "\r\n") {
		return "", errors.New("sse: Event and ID must not contain line breaks")
	}
	if u.ID == "" {
		u.ID = newID()
	}
	u.Origin = h.origin
	if h.transport != nil {
		if err := h.transport.Publish(ctx, u); err != nil {
			return "", fmt.Errorf("sse: publish: %w", err)
		}
	}
	h.dispatch(u)
	return u.ID, nil
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// receive takes an update from the transport, skipping this hub's own:
// Publish has delivered those already.
func (h *Hub) receive(u Update) {
	if u.Origin == h.origin || u.control() {
		return
	}
	h.dispatch(u)
}

// dispatch hands u to this process's subscribers. It never blocks: a
// subscriber whose queue is full is disconnected and resumes with
// Last-Event-ID. Matching runs under h.mu, which keeps subscribe and replay
// atomic with delivery; topics are split once per update.
func (h *Hub) dispatch(u Update) {
	topics := splitTopics(u.Topics)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.remember(u)
	for s := range h.subs {
		if s.matches(topics) {
			h.offer(s, u)
		}
	}
}

// offer queues u for s or, if s is too far behind, disconnects it; h.mu
// must be held.
func (h *Hub) offer(s *subscriber, u Update) {
	select {
	case s.ch <- u:
	default:
		h.log.Warn("sse subscriber fell behind; disconnected", slog.Int("buffer", h.cfg.Buffer))
		h.drop(s)
	}
}

// resync tells every subscriber that updates were lost (the transport was
// down) and forgets the history, which has a hole now: a client resuming
// from before it gets a ResyncEvent instead of an incomplete replay.
func (h *Hub) resync() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.history, h.head = nil, 0
	u := h.resyncMarker()
	for s := range h.subs {
		h.offer(s, u)
	}
}

// resyncMarker returns a ResyncEvent whose ID is in the history, so a client
// reconnecting with it resumes from there; h.mu must be held. It reuses the
// newest entry when there is one, so a wave of reconnects does not flush the
// history with markers.
func (h *Hub) resyncMarker() Update {
	u := Update{Event: ResyncEvent, Data: "{}"}
	switch n := len(h.history); {
	case h.cfg.History == 0:
	case n == 0:
		u.ID = newID()
		h.remember(u)
	default:
		u.ID = h.history[(h.head+n-1)%n].ID
	}
	return u
}

func (h *Hub) remember(u Update) {
	switch n := h.cfg.History; {
	case n == 0:
	case len(h.history) < n:
		h.history = append(h.history, u)
	default:
		h.history[h.head] = u
		h.head = (h.head + 1) % n
	}
}

// since returns the remembered updates after lastID that s matches, oldest
// first; found is false when lastID is no longer (or never was) in the
// history.
func (h *Hub) since(lastID string, s *subscriber) (out []Update, found bool) {
	n := len(h.history)
	for i := range n {
		if h.history[(h.head+i)%n].ID != lastID {
			continue
		}
		for j := i + 1; j < n; j++ {
			if u := h.history[(h.head+j)%n]; !u.control() && s.matches(splitTopics(u.Topics)) {
				out = append(out, u)
			}
		}
		return out, true
	}
	return nil, false
}

// subscribe registers s and returns what it missed since lastID, atomically
// with respect to dispatch so nothing falls between the two. When lastID is
// gone from the history, what it missed is a ResyncEvent.
func (h *Hub) subscribe(s *subscriber, lastID string) ([]Update, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, errClosed
	}
	if h.cfg.MaxSubscribers > 0 && len(h.subs) >= h.cfg.MaxSubscribers {
		return nil, errFull
	}
	var missed []Update
	if lastID != "" && h.cfg.History > 0 {
		var found bool
		if missed, found = h.since(lastID, s); !found {
			missed = []Update{h.resyncMarker()}
		}
	}
	h.subs[s] = struct{}{}
	return missed, nil
}

func (h *Hub) unsubscribe(s *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, s)
}

// drop disconnects s; h.mu must be held.
func (h *Hub) drop(s *subscriber) {
	if _, ok := h.subs[s]; ok {
		delete(h.subs, s)
		close(s.ch)
	}
}

// shutdown ends every stream and refuses new ones, so the HTTP server can
// drain instead of waiting on connections that never finish.
func (h *Hub) shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for s := range h.subs {
		h.drop(s)
	}
}

// ServeHTTP is the subscription endpoint mounted at Config.Path:
// GET ?topic=<selector>&topic=… streams every matching update until the
// client goes away, the stream reaches MaxAge or the service shuts down.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query()["topic"]
	if len(raw) == 0 {
		httpx.Error(w, r, gerrors.InvalidArgument("at least one topic query parameter is required"))
		return
	}
	if len(raw) > h.cfg.MaxTopics {
		httpx.Error(w, r, gerrors.InvalidArgument("at most %d topics per subscription", h.cfg.MaxTopics))
		return
	}
	s := &subscriber{ch: make(chan Update, h.cfg.Buffer)}
	for _, t := range raw {
		sel, err := parseSelector(t)
		if err != nil {
			httpx.Error(w, r, gerrors.InvalidArgument("topic: %v", err))
			return
		}
		s.selectors = append(s.selectors, sel)
	}
	missed, err := h.subscribe(s, r.Header.Get("Last-Event-ID"))
	switch {
	case errors.Is(err, errFull):
		w.Header().Set("Retry-After", "5")
		httpx.Error(w, r, gerrors.Unavailable("too many subscribers"))
		return
	case err != nil:
		httpx.Error(w, r, gerrors.Unavailable("shutting down"))
		return
	}
	defer h.unsubscribe(s)

	st := &stream{w: w, rc: http.NewResponseController(w), timeout: h.cfg.WriteTimeout, r: r, authorize: h.cfg.Authorize}
	// The server's read deadline would end the stream; middleware.Streaming
	// lifts it, and this reports a writer that cannot.
	if err := st.rc.SetReadDeadline(time.Time{}); err != nil {
		h.warnNoDeadlines(r, err)
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("X-Accel-Buffering", "no") // nginx would otherwise buffer the stream
	w.WriteHeader(http.StatusOK)
	if err := st.comment(); err != nil {
		h.log.ErrorContext(r.Context(), "sse stream cannot flush; is the route wrapped in a writer without Flush?", slog.String("error", err.Error()))
		return
	}
	if st.deadlineErr != nil {
		h.warnNoDeadlines(r, st.deadlineErr)
	}
	for _, u := range missed {
		if st.send(u) != nil {
			return
		}
	}

	heartbeat := time.NewTicker(h.cfg.Heartbeat)
	defer heartbeat.Stop()
	var expired <-chan time.Time
	if h.cfg.MaxAge > 0 {
		age := h.cfg.MaxAge - time.Duration(mrand.Int64N(int64(h.cfg.MaxAge)/10+1))
		t := time.NewTimer(age)
		defer t.Stop()
		expired = t.C
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-expired:
			return
		case u, ok := <-s.ch:
			if !ok || st.send(u) != nil {
				return
			}
		case <-heartbeat.C:
			if st.comment() != nil {
				return
			}
		}
	}
}

// warnNoDeadlines logs, once per hub, that the response writer cannot set
// deadlines, so the server's read and write timeouts will cut every stream.
func (h *Hub) warnNoDeadlines(r *http.Request, err error) {
	h.noDeadlines.Do(func() {
		h.log.ErrorContext(r.Context(), "sse stream cannot set connection deadlines; the server's read and write timeouts will cut streams (is the route wrapped in a writer without Unwrap?)",
			slog.String("error", err.Error()))
	})
}

// lineBreaks normalizes CR and CRLF so every data line gets its own field.
var lineBreaks = strings.NewReplacer("\r\n", "\n", "\r", "\n")

// stream writes events to one subscriber.
type stream struct {
	w           http.ResponseWriter
	rc          *http.ResponseController
	timeout     time.Duration
	r           *http.Request
	authorize   func(r *http.Request, topic string) bool
	allowed     map[string]verdict // Authorize answers, per topic, for this connection
	deadlineErr error              // the first SetWriteDeadline failure
}

type verdict struct {
	ok      bool
	expires time.Time
}

func (st *stream) mayReceive(u Update) bool {
	if !u.Private {
		return true
	}
	if st.authorize == nil {
		return false
	}
	if st.allowed == nil || len(st.allowed) > 1024 {
		st.allowed = make(map[string]verdict)
	}
	now := time.Now()
	for _, t := range u.Topics {
		v, seen := st.allowed[t]
		if !seen || now.After(v.expires) {
			v = verdict{ok: st.authorize(st.r, t), expires: now.Add(authorizeTTL)}
			st.allowed[t] = v
		}
		if v.ok {
			return true
		}
	}
	return false
}

func (st *stream) send(u Update) error {
	if !st.mayReceive(u) {
		return nil
	}
	var b strings.Builder
	field := func(name, value string) {
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteByte('\n')
	}
	if u.ID != "" {
		field("id", u.ID)
	}
	if u.Event != "" {
		field("event", u.Event)
	}
	for line := range strings.SplitSeq(lineBreaks.Replace(u.Data), "\n") {
		field("data", line)
	}
	b.WriteString("\n")
	return st.write(b.String())
}

// comment writes an SSE comment: the first one opens the stream for the
// client, later ones keep proxies from closing it as idle.
func (st *stream) comment() error { return st.write(":\n\n") }

func (st *stream) write(s string) error {
	if err := st.rc.SetWriteDeadline(time.Now().Add(st.timeout)); err != nil && st.deadlineErr == nil {
		st.deadlineErr = err
	}
	if _, err := st.w.Write([]byte(s)); err != nil {
		return err
	}
	return st.rc.Flush()
}
