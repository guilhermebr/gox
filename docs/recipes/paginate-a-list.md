# Paginate a list (keyset, not offset)

```go path=main.go
package main

import (
	"net/http"
	"os"
	"time"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"
)

type invoice struct {
	ID        string    `json:"id"`
	Customer  string    `json:"customer"`
	CreatedAt time.Time `json:"createdAt"`
}

// cursor is the sort key of the last row a client saw. The order must be
// total: created_at alone can tie, so the id breaks ties.
type cursor struct {
	CreatedAt time.Time `json:"c"`
	ID        string    `json:"i"`
}

func main() {
	a := gox.MustNew("billing", gox.HTTP(), postgres.Enable())
	db := postgres.From(a)

	a.HandleFunc("GET /invoices", func(w http.ResponseWriter, r *http.Request) {
		page, err := gox.ParsePage(r, 50, 100) // ?after=<cursor>&limit=<n>
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		after := cursor{CreatedAt: time.Now().Add(24 * time.Hour)} // first page: from the newest
		if err := page.Cursor(&after); err != nil {
			gox.Error(w, r, err)
			return
		}
		// One row more than asked tells whether there is a next page.
		rows, err := db.Query(r.Context(),
			`SELECT id, customer, created_at FROM invoices
			 WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3`,
			after.CreatedAt, after.ID, page.Limit+1)
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		defer rows.Close()
		items := []invoice{}
		for rows.Next() {
			var inv invoice
			if err := rows.Scan(&inv.ID, &inv.Customer, &inv.CreatedAt); err != nil {
				gox.Error(w, r, err)
				return
			}
			items = append(items, inv)
		}
		var next *string
		if len(items) > page.Limit {
			items = items[:page.Limit]
			last := items[len(items)-1]
			token, _ := gox.EncodeCursor(cursor{CreatedAt: last.CreatedAt, ID: last.ID})
			next = &token
		}
		_ = gox.JSON(w, http.StatusOK, map[string]any{"data": items, "page": map[string]any{"next": next}})
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

Index `(created_at DESC, id DESC)`. The first page passes an empty id, which
sorts before every real id only with `<` on a future timestamp, as above.
