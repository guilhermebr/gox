package httpx_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/httpx"
)

type cursor struct {
	CreatedAt time.Time `json:"c"`
	ID        string    `json:"i"`
}

func TestParsePageAppliesTheDefaultAndTheCeiling(t *testing.T) {
	for query, want := range map[string]int{"": 50, "limit=10": 10, "limit=100": 100, "limit=5000": 100} {
		p, err := httpx.ParsePage(httptest.NewRequest("GET", "/items?"+query, nil), 50, 100)
		if err != nil || p.Limit != want || p.After != "" {
			t.Errorf("%q: %+v %v", query, p, err)
		}
	}
	for _, bad := range []string{"limit=0", "limit=-3", "limit=ten"} {
		_, err := httpx.ParsePage(httptest.NewRequest("GET", "/items?"+bad, nil), 50, 100)
		if errors.CodeOf(err) != errors.CodeInvalidArgument || !strings.Contains(err.Error(), "limit") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestCursorsRoundTripAndRejectTampering(t *testing.T) {
	in := cursor{CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), ID: "0190f3a2"}
	token, err := httpx.EncodeCursor(in)
	if err != nil || strings.ContainsAny(token, "+/= ") {
		t.Fatalf("token = %q %v", token, err)
	}
	p, err := httpx.ParsePage(httptest.NewRequest("GET", "/items?after="+token, nil), 50, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out cursor
	if err := p.Cursor(&out); err != nil || !out.CreatedAt.Equal(in.CreatedAt) || out.ID != in.ID {
		t.Fatalf("out = %+v %v", out, err)
	}
	bad := httpx.Page{After: "!!not-base64!!"}
	if err := bad.Cursor(&out); errors.CodeOf(err) != errors.CodeInvalidArgument {
		t.Fatalf("garbage cursor = %v", err)
	}
	first := httpx.Page{}
	if err := first.Cursor(&out); err != nil {
		t.Fatalf("the first page has no cursor and that is fine: %v", err)
	}
	if !first.First() || p.First() {
		t.Fatal("First reports whether a cursor was sent")
	}
}
