// Package httpx holds the three helpers every handler uses (JSON, Error,
// Decode) and the envelope writer the middleware chain shares with them, so
// a client sees one error shape whether a handler or the framework produced
// it.
package httpx

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/log"
)

// JSON writes v as JSON with the given status.
func JSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// WriteEnvelope writes an error envelope with the given status.
func WriteEnvelope(w http.ResponseWriter, status int, env errors.Envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}

// Error renders err as the standard envelope. It applies the error mappers
// stored in the request context (the app installs its WithErrorMapper
// functions there), stamps the request id, and logs server-side failures
// with their cause. Client errors are not logged: they are the API working
// as designed.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	err = errors.Map(err, Mappers(ctx)...)
	status, env := errors.ToEnvelope(err, log.RequestID(ctx))
	if status >= http.StatusInternalServerError {
		log.FromContext(ctx).ErrorContext(ctx, "request failed",
			slog.String(log.KeyError, err.Error()),
			slog.Int("status", status),
			slog.String("code", env.Code))
	}
	WriteEnvelope(w, status, env)
}

// Decode reads a JSON body into v strictly: unknown fields, trailing data,
// empty bodies and bodies over the MaxBytes limit are all invalid-argument
// errors with a message that says what was wrong.
func Decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case stderrors.As(err, &maxErr):
			return errors.Newf(errors.CodeInvalidArgument, "request body too large (limit %d bytes)", maxErr.Limit).
				WithHTTPStatus(http.StatusRequestEntityTooLarge)
		case stderrors.Is(err, io.EOF):
			return errors.New(errors.CodeInvalidArgument, "request body is empty")
		}
		return errors.Wrap(err, errors.CodeInvalidArgument, fmt.Sprintf("invalid JSON body: %v", err))
	}
	if dec.More() {
		return errors.New(errors.CodeInvalidArgument, "request body has trailing data after the JSON value")
	}
	return nil
}

type mappersKey struct{}

// WithMappers stores error mappers in ctx for Error to apply.
func WithMappers(ctx context.Context, mappers ...errors.Mapper) context.Context {
	if len(mappers) == 0 {
		return ctx
	}
	return context.WithValue(ctx, mappersKey{}, mappers)
}

// Mappers returns the mappers stored by WithMappers, or nil.
func Mappers(ctx context.Context) []errors.Mapper {
	m, _ := ctx.Value(mappersKey{}).([]errors.Mapper)
	return m
}
