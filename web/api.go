package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/httpclient"
	"github.com/guilhermebr/gox/pkg/log"
)

// API calls the service's own JSON API as the signed-in user: the session
// token goes out as a bearer, request ids and trace context propagate, and
// a gox error envelope in a response becomes a coded error, so web.Error
// renders a backend 404 as a 404 page.
type API struct {
	client  *http.Client
	baseURL string
}

func newAPI(client *http.Client, baseURL, token string) *API {
	if token != "" {
		client = httpclient.WithBearer(client, token)
	}
	return &API{client: client, baseURL: strings.TrimRight(baseURL, "/")}
}

var withRequestID = log.WithRequestID

type apiKey struct{}

// APIFrom returns the request's backend client. It panics if WithBackend
// was not passed to web.Enable.
func APIFrom(r *http.Request) *API {
	a, ok := r.Context().Value(apiKey{}).(*API)
	if !ok {
		panic("gox: web.APIFrom called but web.WithBackend() was not passed to web.Enable")
	}
	return a
}

// Get decodes the JSON at path into out (nil to ignore the body).
func (a *API) Get(ctx context.Context, path string, out any) error {
	return a.do(ctx, http.MethodGet, path, nil, out)
}

// Post sends in as JSON and decodes the response into out.
func (a *API) Post(ctx context.Context, path string, in, out any) error {
	return a.do(ctx, http.MethodPost, path, in, out)
}

// Put sends in as JSON and decodes the response into out.
func (a *API) Put(ctx context.Context, path string, in, out any) error {
	return a.do(ctx, http.MethodPut, path, in, out)
}

// Delete issues a DELETE.
func (a *API) Delete(ctx context.Context, path string) error {
	return a.do(ctx, http.MethodDelete, path, nil, nil)
}

func (a *API) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("web: api: encode: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("web: api: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return errors.Wrap(err, errors.CodeUnavailable, "backend unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return errors.Wrap(err, errors.CodeUnavailable, "backend response unreadable")
	}
	if resp.StatusCode >= 400 {
		return apiError(resp.StatusCode, resp.Header.Get("Content-Type"), raw)
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.Wrap(err, errors.CodeInternal, "backend response is not the expected JSON")
	}
	return nil
}

// apiError turns a failed response into a coded error. A gox envelope keeps
// its code, message and details; anything else maps by status with a
// generic message so a foreign body never reaches the user.
func apiError(status int, contentType string, raw []byte) error {
	if strings.HasPrefix(contentType, "application/json") {
		var env errors.Envelope
		if json.Unmarshal(raw, &env) == nil && env.Code != "" {
			e := errors.New(codeFromName(env.Code, status), env.Message).WithHTTPStatus(status)
			for k, v := range env.Details {
				e = e.WithDetail(k, v)
			}
			return e
		}
	}
	switch {
	case status == http.StatusNotFound:
		return errors.NotFound("not found")
	case status == http.StatusUnauthorized:
		return errors.Unauthenticated("authentication required")
	case status == http.StatusForbidden:
		return errors.PermissionDenied("forbidden")
	case status == http.StatusConflict:
		return errors.AlreadyExists("conflict")
	case status == http.StatusTooManyRequests:
		return errors.ResourceExhausted("rate limited")
	case status >= 500:
		return errors.Newf(errors.CodeUnavailable, "backend returned %d", status)
	default:
		return errors.Newf(errors.CodeInvalidArgument, "backend rejected the request (%d)", status)
	}
}

func codeFromName(name string, status int) errors.Code {
	if c, ok := errors.ParseCode(name); ok {
		return c
	}
	if status >= 500 {
		return errors.CodeUnavailable
	}
	return errors.CodeInvalidArgument
}
