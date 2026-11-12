package sse

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSelectorMatching(t *testing.T) {
	cases := []struct {
		sel   string
		topic string
		want  bool
	}{
		{"*", "anything", true},
		{"/orders/1", "/orders/1", true},
		{"/orders/1", "/orders/12", false},
		{"/orders/{id}", "/orders/1", true},
		{"/orders/{id}", "/orders/", false},
		{"/orders/{id}", "/orders/1/items", false},
		{"/orders/{id}/items", "/orders/1/items", true},
		{"https://example.com/users/{id}", "https://example.com/users/ana", true},
		{"https://example.com/users/{id}", "https://other.com/users/ana", false},
		{"/files/{rest...}", "/files/a/b", true},
		{"/files/{rest...}", "/files/", true},
		{"/files/{rest...}", "/files", false},
		{"/files/{rest...}", "/other/a", false},
	}
	for _, c := range cases {
		s, err := parseSelector(c.sel)
		if err != nil {
			t.Fatalf("parse %q: %v", c.sel, err)
		}
		if got := s.match(splitTopics([]string{c.topic})[0]); got != c.want {
			t.Errorf("%q.match(%q) = %v, want %v", c.sel, c.topic, got, c.want)
		}
	}
	for _, bad := range []string{"", "/a/{", "/a/{}", "/a/x{id}", "/{a...}/b", "/{{a}}"} {
		if _, err := parseSelector(bad); err == nil {
			t.Errorf("parse %q succeeded", bad)
		}
	}
}

func testHub(cfg Config) *Hub {
	return &Hub{cfg: cfg, log: slog.New(slog.NewTextHandler(io.Discard, nil)), origin: "me", subs: make(map[*subscriber]struct{})}
}

func TestAHubSkipsItsOwnUpdatesFromTheTransport(t *testing.T) {
	h := testHub(Config{Buffer: 2, History: 10})
	sel, _ := parseSelector("*")
	s := &subscriber{selectors: []selector{sel}, ch: make(chan Update, 2)}
	if _, err := h.subscribe(s, ""); err != nil {
		t.Fatal(err)
	}
	h.receive(Update{Topics: []string{"/a"}, ID: "mine", Origin: "me"})
	h.receive(Update{Topics: []string{"/a"}, ID: "theirs", Origin: "other"})
	if u := <-s.ch; u.ID != "theirs" {
		t.Fatalf("got %q, want theirs", u.ID)
	}
	if len(s.ch) != 0 {
		t.Fatal("own echo was delivered")
	}
}

func TestAuthorizeAnswersExpire(t *testing.T) {
	calls := 0
	st := &stream{authorize: func(*http.Request, string) bool { calls++; return true }}
	u := Update{Topics: []string{"/u/1"}, Private: true}
	st.mayReceive(u)
	st.mayReceive(u)
	if calls != 1 {
		t.Fatalf("Authorize called %d times, want a cached answer", calls)
	}
	v := st.allowed["/u/1"]
	v.expires = time.Now().Add(-time.Second)
	st.allowed["/u/1"] = v
	st.mayReceive(u)
	if calls != 2 {
		t.Fatalf("Authorize called %d times, want it asked again after the TTL", calls)
	}
}

func TestMaxSubscribersCapsSubscriptions(t *testing.T) {
	h := testHub(Config{Buffer: 1, MaxSubscribers: 1})
	if _, err := h.subscribe(&subscriber{ch: make(chan Update, 1)}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.subscribe(&subscriber{ch: make(chan Update, 1)}, ""); !errors.Is(err, errFull) {
		t.Fatalf("err = %v, want errFull", err)
	}
}

func TestASubscriberThatFallsBehindIsDisconnected(t *testing.T) {
	h := testHub(Config{Buffer: 1})
	sel, _ := parseSelector("*")
	s := &subscriber{selectors: []selector{sel}, ch: make(chan Update, 1)}
	if _, err := h.subscribe(s, ""); err != nil {
		t.Fatal(err)
	}
	h.dispatch(Update{Topics: []string{"/a"}, ID: "1"})
	h.dispatch(Update{Topics: []string{"/a"}, ID: "2"}) // queue full
	if u := <-s.ch; u.ID != "1" {
		t.Fatalf("first = %q", u.ID)
	}
	if _, ok := <-s.ch; ok {
		t.Fatal("channel still open after overflow")
	}
	if len(h.subs) != 0 {
		t.Fatal("dropped subscriber still registered")
	}
}

func TestHistoryIsARingOfTheLastUpdates(t *testing.T) {
	h := testHub(Config{Buffer: 1, History: 3})
	for _, id := range []string{"1", "2", "3", "4", "5"} {
		h.dispatch(Update{Topics: []string{"/a"}, ID: id})
	}
	sel, _ := parseSelector("/a")
	s := &subscriber{selectors: []selector{sel}, ch: make(chan Update, 1)}
	ids := func(us []Update) string {
		var out []string
		for _, u := range us {
			out = append(out, u.ID)
		}
		return strings.Join(out, ",")
	}
	if got, found := h.since("3", s); !found || ids(got) != "4,5" {
		t.Fatalf("since 3 = %q, %v", ids(got), found)
	}
	if got, found := h.since("1", s); found || got != nil {
		t.Fatalf("since an evicted id = %v, %v; want nil, false", got, found)
	}
}

func TestShutdownRefusesNewSubscribers(t *testing.T) {
	h := testHub(Config{Buffer: 1})
	h.shutdown()
	if _, err := h.subscribe(&subscriber{ch: make(chan Update, 1)}, ""); err == nil {
		t.Fatal("subscribe after shutdown succeeded")
	}
}

func TestNewAppliesDefaultsAndRejectsBadConfig(t *testing.T) {
	h, err := New(Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.cfg.Path != "/events" || h.cfg.Heartbeat != 15*time.Second || h.cfg.MaxAge != 15*time.Minute || h.cfg.Buffer != 64 ||
		h.cfg.History != 1000 || h.cfg.MaxTopics != 32 || h.cfg.MaxSubscribers != 10000 {
		t.Fatalf("defaults = %+v", h.cfg)
	}
	if h, _ := New(Config{History: -1}, nil, nil); h.cfg.History != 0 {
		t.Fatalf("History -1 = %d, want replay off", h.cfg.History)
	}
	for _, bad := range []Config{{Path: "events"}, {Path: "/a/{id}"}, {Buffer: -1}} {
		if _, err := New(bad, nil, nil); err == nil {
			t.Errorf("New(%+v) succeeded", bad)
		}
	}
}
