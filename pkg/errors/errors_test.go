package errors_test

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/guilhermebr/gox/pkg/errors"
)

func TestConstructorsCarryCodeAndFormattedMessage(t *testing.T) {
	err := errors.NotFound("invoice %s", "inv_42")
	if err.Code() != errors.CodeNotFound {
		t.Fatalf("Code = %v, want NotFound", err.Code())
	}
	if err.Message() != "invoice inv_42" {
		t.Fatalf("Message = %q", err.Message())
	}
	if err.Error() != "invoice inv_42" {
		t.Fatalf("Error = %q", err.Error())
	}
}

func TestCodeOfSeesThroughWrapping(t *testing.T) {
	base := errors.PermissionDenied("not the owner")
	wrapped := fmt.Errorf("handler: %w", base)
	if got := errors.CodeOf(wrapped); got != errors.CodePermissionDenied {
		t.Fatalf("CodeOf(wrapped) = %v", got)
	}
	if !stderrors.Is(wrapped, base) {
		t.Fatal("errors.Is should find the original *Error through fmt.Errorf")
	}
}

func TestCodeOfDefaultsForForeignErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want errors.Code
	}{
		{"nil", nil, errors.CodeOK},
		{"plain", stderrors.New("boom"), errors.CodeUnknown},
		{"deadline", context.DeadlineExceeded, errors.CodeDeadlineExceeded},
		{"canceled", context.Canceled, errors.CodeCanceled},
		{"wrapped deadline", fmt.Errorf("db: %w", context.DeadlineExceeded), errors.CodeDeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.CodeOf(tt.err); got != tt.want {
				t.Fatalf("CodeOf = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWrapKeepsCauseAndExposesPublicMessage(t *testing.T) {
	cause := stderrors.New("pq: connection refused")
	err := errors.Wrap(cause, errors.CodeUnavailable, "database unavailable")
	if !stderrors.Is(err, cause) {
		t.Fatal("Wrap must preserve the cause for errors.Is")
	}
	if err.Message() != "database unavailable" {
		t.Fatalf("Message = %q; the public message must not contain the cause", err.Message())
	}
	if err.Error() != "database unavailable: pq: connection refused" {
		t.Fatalf("Error = %q; the log form should include the cause", err.Error())
	}
	if err.Code() != errors.CodeUnavailable {
		t.Fatalf("Code = %v", err.Code())
	}
}

func TestHTTPStatusMapping(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{nil, http.StatusOK},
		{errors.InvalidArgument("bad"), http.StatusBadRequest},
		{errors.Unauthenticated("no token"), http.StatusUnauthorized},
		{errors.PermissionDenied("no"), http.StatusForbidden},
		{errors.NotFound("x"), http.StatusNotFound},
		{errors.AlreadyExists("x"), http.StatusConflict},
		{errors.Aborted("x"), http.StatusConflict},
		{errors.FailedPrecondition("x"), http.StatusBadRequest},
		{errors.ResourceExhausted("x"), http.StatusTooManyRequests},
		{errors.Unimplemented("x"), http.StatusNotImplemented},
		{errors.Unavailable("x"), http.StatusServiceUnavailable},
		{errors.DeadlineExceeded("x"), http.StatusGatewayTimeout},
		{errors.Internal("x"), http.StatusInternalServerError},
		{stderrors.New("anything else"), http.StatusInternalServerError},
		{context.Canceled, 499},
	}
	for _, tt := range tests {
		if got := errors.HTTPStatus(tt.err); got != tt.want {
			t.Errorf("HTTPStatus(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}
}

func TestCodeStringIsSnakeCaseForTheEnvelope(t *testing.T) {
	if got := errors.CodeNotFound.String(); got != "not_found" {
		t.Fatalf("String = %q", got)
	}
	if got := errors.CodeFailedPrecondition.String(); got != "failed_precondition" {
		t.Fatalf("String = %q", got)
	}
}

func TestDetailsAreCopiedNotShared(t *testing.T) {
	base := errors.InvalidArgument("validation failed")
	withField := base.WithDetail("field", "email")
	if _, ok := base.Details()["field"]; ok {
		t.Fatal("WithDetail mutated the original")
	}
	if withField.Details()["field"] != "email" {
		t.Fatalf("Details = %v", withField.Details())
	}
	if withField.Code() != base.Code() || withField.Message() != base.Message() {
		t.Fatal("WithDetail changed code or message")
	}
}

func TestEnvelopeRendersPublicSafeJSON(t *testing.T) {
	t.Run("gox error with details and request id", func(t *testing.T) {
		err := errors.InvalidArgument("email is invalid").WithDetail("field", "email")
		status, env := errors.ToEnvelope(err, "req-1")
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d", status)
		}
		b, _ := json.Marshal(env)
		want := `{"code":"invalid_argument","message":"email is invalid","request_id":"req-1","details":{"field":"email"}}`
		if string(b) != want {
			t.Fatalf("json = %s\nwant  %s", b, want)
		}
	})
	t.Run("foreign error never leaks its text", func(t *testing.T) {
		status, env := errors.ToEnvelope(stderrors.New("pq: password authentication failed for user admin"), "")
		if status != http.StatusInternalServerError {
			t.Fatalf("status = %d", status)
		}
		b, _ := json.Marshal(env)
		want := `{"code":"internal","message":"internal error"}`
		if string(b) != want {
			t.Fatalf("json = %s\nwant  %s", b, want)
		}
	})
	t.Run("wrapped gox error keeps only the public message", func(t *testing.T) {
		err := errors.Wrap(stderrors.New("secret detail"), errors.CodeUnavailable, "try again later")
		_, env := errors.ToEnvelope(fmt.Errorf("outer: %w", err), "")
		if env.Message != "try again later" {
			t.Fatalf("Message = %q", env.Message)
		}
	})
	t.Run("context errors get their own codes", func(t *testing.T) {
		status, env := errors.ToEnvelope(context.DeadlineExceeded, "")
		if status != http.StatusGatewayTimeout || env.Code != "deadline_exceeded" {
			t.Fatalf("status=%d code=%s", status, env.Code)
		}
	})
}

func TestMapAppliesMappersUntilOneProducesAGoxError(t *testing.T) {
	errNoRows := stderrors.New("no rows")
	errConflict := stderrors.New("unique violation")

	first := func(err error) error {
		if stderrors.Is(err, errNoRows) {
			return errors.Wrap(err, errors.CodeNotFound, "not found")
		}
		return err
	}
	second := func(err error) error {
		if stderrors.Is(err, errConflict) {
			return errors.Wrap(err, errors.CodeAlreadyExists, "already exists")
		}
		return err
	}

	if got := errors.CodeOf(errors.Map(fmt.Errorf("repo: %w", errNoRows), first, second)); got != errors.CodeNotFound {
		t.Fatalf("first mapper: code = %v", got)
	}
	if got := errors.CodeOf(errors.Map(errConflict, first, second)); got != errors.CodeAlreadyExists {
		t.Fatalf("second mapper: code = %v", got)
	}
	unmapped := stderrors.New("something else")
	if got := errors.Map(unmapped, first, second); got != unmapped {
		t.Fatal("unmapped errors must pass through unchanged")
	}
	already := errors.NotFound("x")
	if got := errors.Map(already, first, second); got != already {
		t.Fatal("an existing *Error must pass through untouched")
	}
	if errors.Map(nil, first) != nil {
		t.Fatal("Map(nil) must be nil")
	}
}

func TestWithHTTPStatusOverridesOnlyTheStatus(t *testing.T) {
	err := errors.InvalidArgument("too big").WithHTTPStatus(http.StatusRequestEntityTooLarge)
	if errors.HTTPStatus(err) != http.StatusRequestEntityTooLarge {
		t.Fatalf("HTTPStatus = %d", errors.HTTPStatus(err))
	}
	if errors.HTTPStatus(fmt.Errorf("wrapped: %w", err)) != http.StatusRequestEntityTooLarge {
		t.Fatal("override must survive wrapping")
	}
	status, env := errors.ToEnvelope(err, "")
	if status != http.StatusRequestEntityTooLarge || env.Code != "invalid_argument" {
		t.Fatalf("status=%d code=%s", status, env.Code)
	}
	if errors.HTTPStatus(errors.InvalidArgument("plain")) != http.StatusBadRequest {
		t.Fatal("errors without an override keep the code's status")
	}
}

func TestParseCodeRoundTripsEveryName(t *testing.T) {
	for c := errors.CodeOK; c <= errors.CodeUnauthenticated; c++ {
		got, ok := errors.ParseCode(c.String())
		if !ok || got != c {
			t.Fatalf("ParseCode(%q) = %v, %v", c.String(), got, ok)
		}
	}
	if _, ok := errors.ParseCode("nope"); ok {
		t.Fatal("unknown name must not parse")
	}
}
