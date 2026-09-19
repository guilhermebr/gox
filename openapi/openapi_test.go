package openapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/openapi"
)

const spec = `openapi: 3.1.0
info: {title: Shop API, version: 1.0.0}
servers:
  - url: https://shop.example
paths:
  /api/v1/invoices:
    get:
      operationId: listInvoices
      parameters:
        - {name: limit, in: query, required: false, schema: {type: integer, minimum: 1, maximum: 100}}
      responses: {"200": {description: ok}}
    post:
      operationId: createInvoice
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [customer, amountCents]
              unevaluatedProperties: false
              properties:
                customer: {type: string, minLength: 1}
                amountCents: {type: integer, minimum: 1}
                note: {type: [string, "null"]}
      responses: {"201": {description: created}}
  /api/v1/invoices/{id}:
    get:
      operationId: getInvoice
      parameters:
        - {name: id, in: path, required: true, schema: {type: string, format: uuid}}
      responses: {"200": {description: ok}}
`

func start(t *testing.T, opts ...gox.Option) string {
	t.Helper()
	old := os.Args
	os.Args = []string{"svc"}
	t.Cleanup(func() { os.Args = old })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	t.Setenv("SHOP_HTTP_ADDR", addr)
	a, err := gox.New("shop", append([]gox.Option{gox.WithoutAdminServer(), gox.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))), gox.HTTP()}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	a.HandleFunc("POST /api/v1/invoices", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Customer    string  `json:"customer"`
			AmountCents int     `json:"amountCents"`
			Note        *string `json:"note"`
		}
		if err := gox.Decode(r, &in); err != nil { // the validator must leave the body readable
			gox.Error(w, r, err)
			return
		}
		_ = gox.JSON(w, http.StatusCreated, in)
	})
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	a.HandleFunc("GET /api/v1/invoices", ok)
	a.HandleFunc("GET /api/v1/invoices/{id}", ok)
	a.HandleFunc("GET /undocumented", ok)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	for !a.Health().IsReady() {
		time.Sleep(5 * time.Millisecond)
	}
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return "http://" + addr
}

type reply struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details struct {
		Errors []openapi.Violation `json:"errors"`
	} `json:"details"`
}

func call(t *testing.T, method, url, body string) (int, reply, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var out reply
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out, string(raw)
}

func TestValidRequestsReachTheHandlerWithTheirBody(t *testing.T) {
	base := start(t, openapi.Enable([]byte(spec)))
	status, _, raw := call(t, http.MethodPost, base+"/api/v1/invoices", `{"customer":"ana","amountCents":1250,"note":null}`)
	if status != http.StatusCreated || !strings.Contains(raw, `"amountCents":1250`) {
		t.Fatalf("%d %s", status, raw)
	}
	for _, path := range []string{"/api/v1/invoices?limit=50", "/api/v1/invoices", "/api/v1/invoices/0190f3a2-7b1c-7def-8a3b-5c6d7e8f9a0b"} {
		if status, _, raw := call(t, http.MethodGet, base+path, ""); status != http.StatusOK {
			t.Errorf("%s = %d %s", path, status, raw)
		}
	}
}

func TestInvalidRequestsAre400WithOneViolationPerProblem(t *testing.T) {
	base := start(t, openapi.Enable([]byte(spec)))
	cases := []struct {
		name, method, path, body string
		pointer, mention         string
	}{
		{"wrong type", http.MethodPost, "/api/v1/invoices", `{"customer":"ana","amountCents":"lots"}`, "/amountCents", "integer"},
		{"missing property", http.MethodPost, "/api/v1/invoices", `{"customer":"ana"}`, "", "amountCents"},
		{"unknown property", http.MethodPost, "/api/v1/invoices", `{"customer":"ana","amountCents":1,"extra":true}`, "", "extra"},
		{"below minimum", http.MethodPost, "/api/v1/invoices", `{"customer":"ana","amountCents":0}`, "/amountCents", "minimum"},
		{"query out of range", http.MethodGet, "/api/v1/invoices?limit=500", "", "/limit", "limit"},
		{"query not a number", http.MethodGet, "/api/v1/invoices?limit=many", "", "/limit", "limit"},
		{"path format", http.MethodGet, "/api/v1/invoices/not-a-uuid", "", "/id", "id"},
	}
	for _, tc := range cases {
		status, out, raw := call(t, tc.method, base+tc.path, tc.body)
		if status != http.StatusBadRequest || out.Code != "invalid_argument" || len(out.Details.Errors) == 0 {
			t.Errorf("%s: %d %s", tc.name, status, raw)
			continue
		}
		found := false
		for _, v := range out.Details.Errors {
			if v.Code == "" || v.Message == "" {
				t.Errorf("%s: incomplete violation %+v", tc.name, v)
			}
			if (tc.pointer == "" || v.Pointer == tc.pointer) && strings.Contains(strings.ToLower(v.Message+v.Pointer), strings.ToLower(tc.mention)) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no violation with pointer %q mentioning %q in %s", tc.name, tc.pointer, tc.mention, raw)
		}
	}
}

func TestRequestsTheDocumentDoesNotDescribeGoToTheMux(t *testing.T) {
	base := start(t, openapi.Enable([]byte(spec)))
	if status, _, raw := call(t, http.MethodGet, base+"/undocumented", ""); status != http.StatusOK {
		t.Fatalf("undocumented route = %d %s", status, raw)
	}
	if status, _, _ := call(t, http.MethodGet, base+"/nowhere", ""); status != http.StatusNotFound {
		t.Fatalf("unknown path = %d", status)
	}
	if status, _, _ := call(t, http.MethodDelete, base+"/api/v1/invoices", ""); status != http.StatusMethodNotAllowed {
		t.Fatalf("undocumented method = %d", status)
	}
	if status, _, _ := call(t, http.MethodGet, base+"/healthz", ""); status != http.StatusOK {
		t.Fatalf("health = %d", status)
	}
}

func TestABrokenDocumentFailsAtStartup(t *testing.T) {
	old := os.Args
	os.Args = []string{"svc"}
	defer func() { os.Args = old }()
	_, err := gox.New("shop", gox.WithoutAdminServer(), gox.HTTP(), openapi.Enable([]byte(spec), []byte("title: not an openapi document")))
	if err == nil || !strings.Contains(err.Error(), "openapi: document 2") {
		t.Fatalf("err = %v", err)
	}
	_, err = gox.New("shop", gox.WithoutAdminServer(), gox.HTTP(), openapi.Enable())
	if err == nil || !strings.Contains(err.Error(), "at least one document") {
		t.Fatalf("no documents: %v", err)
	}
}
