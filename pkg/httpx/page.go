package httpx

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/guilhermebr/gox/pkg/errors"
)

// Page is a keyset pagination request: ?after=<cursor>&limit=<n>. Keyset
// pages stay correct while rows are inserted and cost the same on page one
// and page one thousand; offsets do neither.
type Page struct {
	After string // opaque cursor of the last item the client saw; "" for the first page
	Limit int
}

// ParsePage reads after and limit from the query. A missing limit is def, a
// larger one is capped at ceiling, and anything that is not a positive
// number is an invalid-argument error.
func ParsePage(r *http.Request, def, ceiling int) (Page, error) {
	q := r.URL.Query()
	p := Page{After: q.Get("after"), Limit: def}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return Page{}, errors.Newf(errors.CodeInvalidArgument, "limit must be a positive number, got %q", raw)
		}
		p.Limit = min(n, ceiling)
	}
	return p, nil
}

// First reports whether this is the first page.
func (p Page) First() bool { return p.After == "" }

// Cursor decodes the after cursor into v, which EncodeCursor produced. On
// the first page it leaves v untouched. A cursor that does not decode is an
// invalid-argument error: cursors are opaque to clients, not trusted.
func (p Page) Cursor(v any) error {
	if p.After == "" {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(p.After)
	if err != nil {
		return errors.New(errors.CodeInvalidArgument, "after is not a valid cursor")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return errors.New(errors.CodeInvalidArgument, "after is not a valid cursor")
	}
	return nil
}

// EncodeCursor turns the sort key of the last item of a page (for example
// {created_at, id}) into the opaque cursor clients send back as after.
func EncodeCursor(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
