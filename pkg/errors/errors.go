// Package errors is the canonical error model for gox services: an error
// carries a Code (mirroring gRPC status codes), a public-safe message, and
// optional details, and maps to an HTTP status and a JSON envelope in one
// documented way.
//
// Services do not have to replace their own sentinel errors: Map applies
// caller-supplied Mapper functions at the boundary so existing domain errors
// can be translated to codes without rewriting them.
package errors

import (
	"context"
	stderrors "errors"
	"fmt"
	"maps"
	"net/http"
)

// Code classifies an error. The numbering mirrors gRPC status codes.
type Code int

// Codes. Zero is OK so that CodeOf(nil) reads naturally.
const (
	CodeOK                 Code = 0
	CodeCanceled           Code = 1
	CodeUnknown            Code = 2
	CodeInvalidArgument    Code = 3
	CodeDeadlineExceeded   Code = 4
	CodeNotFound           Code = 5
	CodeAlreadyExists      Code = 6
	CodePermissionDenied   Code = 7
	CodeResourceExhausted  Code = 8
	CodeFailedPrecondition Code = 9
	CodeAborted            Code = 10
	CodeOutOfRange         Code = 11
	CodeUnimplemented      Code = 12
	CodeInternal           Code = 13
	CodeUnavailable        Code = 14
	CodeDataLoss           Code = 15
	CodeUnauthenticated    Code = 16
)

var codeNames = map[Code]string{
	CodeOK:                 "ok",
	CodeCanceled:           "canceled",
	CodeUnknown:            "unknown",
	CodeInvalidArgument:    "invalid_argument",
	CodeDeadlineExceeded:   "deadline_exceeded",
	CodeNotFound:           "not_found",
	CodeAlreadyExists:      "already_exists",
	CodePermissionDenied:   "permission_denied",
	CodeResourceExhausted:  "resource_exhausted",
	CodeFailedPrecondition: "failed_precondition",
	CodeAborted:            "aborted",
	CodeOutOfRange:         "out_of_range",
	CodeUnimplemented:      "unimplemented",
	CodeInternal:           "internal",
	CodeUnavailable:        "unavailable",
	CodeDataLoss:           "data_loss",
	CodeUnauthenticated:    "unauthenticated",
}

// StatusClientClosedRequest is the non-standard status nginx uses when the
// client went away; it is what a canceled context maps to.
const StatusClientClosedRequest = 499

var httpStatus = map[Code]int{
	CodeOK:                 http.StatusOK,
	CodeCanceled:           StatusClientClosedRequest,
	CodeUnknown:            http.StatusInternalServerError,
	CodeInvalidArgument:    http.StatusBadRequest,
	CodeDeadlineExceeded:   http.StatusGatewayTimeout,
	CodeNotFound:           http.StatusNotFound,
	CodeAlreadyExists:      http.StatusConflict,
	CodePermissionDenied:   http.StatusForbidden,
	CodeResourceExhausted:  http.StatusTooManyRequests,
	CodeFailedPrecondition: http.StatusBadRequest,
	CodeAborted:            http.StatusConflict,
	CodeOutOfRange:         http.StatusBadRequest,
	CodeUnimplemented:      http.StatusNotImplemented,
	CodeInternal:           http.StatusInternalServerError,
	CodeUnavailable:        http.StatusServiceUnavailable,
	CodeDataLoss:           http.StatusInternalServerError,
	CodeUnauthenticated:    http.StatusUnauthorized,
}

// String returns the snake_case name used in the JSON envelope.
func (c Code) String() string {
	if s, ok := codeNames[c]; ok {
		return s
	}
	return "unknown"
}

// ParseCode returns the Code for a name from an envelope ("not_found"). ok is
// false for names it does not know.
func ParseCode(name string) (code Code, ok bool) {
	for c, n := range codeNames {
		if n == name {
			return c, true
		}
	}
	return CodeUnknown, false
}

// HTTPStatus returns the HTTP status the code renders as.
func (c Code) HTTPStatus() int {
	if s, ok := httpStatus[c]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// Error is a coded error with a public-safe message and an optional cause.
type Error struct {
	code    Code
	msg     string
	details map[string]any
	cause   error
	status  int // optional HTTP status override
}

// New creates an Error with a code and a public-safe message.
func New(code Code, msg string) *Error {
	return &Error{code: code, msg: msg}
}

// Newf creates an Error with a code and a formatted public-safe message.
func Newf(code Code, format string, args ...any) *Error {
	return &Error{code: code, msg: fmt.Sprintf(format, args...)}
}

// Wrap creates an Error that keeps cause for errors.Is/As and logs, while
// msg is what clients see.
func Wrap(cause error, code Code, msg string) *Error {
	return &Error{code: code, msg: msg, cause: cause}
}

// Code returns the error's code.
func (e *Error) Code() Code { return e.code }

// Message returns the public-safe message.
func (e *Error) Message() string { return e.msg }

// Details returns a copy of the details map (nil when there are none).
func (e *Error) Details() map[string]any {
	if e.details == nil {
		return nil
	}
	return maps.Clone(e.details)
}

// WithDetail returns a copy of the error with one more detail attached.
// Details are public: they end up in the JSON envelope.
func (e *Error) WithDetail(key string, value any) *Error {
	c := *e
	c.details = maps.Clone(e.details)
	if c.details == nil {
		c.details = make(map[string]any, 1)
	}
	c.details[key] = value
	return &c
}

// WithHTTPStatus returns a copy that renders with a specific HTTP status
// instead of the code's default, for the few HTTP-only cases such as 413.
// The code and envelope are unchanged.
func (e *Error) WithHTTPStatus(status int) *Error {
	c := *e
	c.status = status
	return &c
}

// Error renders the message and, when present, the cause. This form is for
// logs; use Message for anything client-facing.
func (e *Error) Error() string {
	if e.cause != nil {
		return e.msg + ": " + e.cause.Error()
	}
	return e.msg
}

// Unwrap returns the cause, if any.
func (e *Error) Unwrap() error { return e.cause }

// CodeOf returns the code of err, looking through wrapping. nil is OK,
// context errors get their own codes, and anything else is Unknown.
func CodeOf(err error) Code {
	if err == nil {
		return CodeOK
	}
	if e, ok := stderrors.AsType[*Error](err); ok {
		return e.code
	}
	switch {
	case stderrors.Is(err, context.DeadlineExceeded):
		return CodeDeadlineExceeded
	case stderrors.Is(err, context.Canceled):
		return CodeCanceled
	}
	return CodeUnknown
}

// HTTPStatus returns the HTTP status err renders as.
func HTTPStatus(err error) int {
	if e, ok := stderrors.AsType[*Error](err); ok && e.status != 0 {
		return e.status
	}
	return CodeOf(err).HTTPStatus()
}

// Envelope is the JSON body every gox error response carries.
type Envelope struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"request_id,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

// ToEnvelope renders err as an HTTP status and a public-safe envelope. Errors
// that are not *Error never leak their text: they become a generic message
// for their code.
func ToEnvelope(err error, requestID string) (int, Envelope) {
	code := CodeOf(err)
	env := Envelope{Code: code.String(), RequestID: requestID}
	if e, ok := stderrors.AsType[*Error](err); ok {
		env.Message = e.msg
		env.Details = e.Details()
		return HTTPStatus(err), env
	}
	switch code {
	case CodeOK:
		env.Message = "ok"
	case CodeDeadlineExceeded:
		env.Message = "deadline exceeded"
	case CodeCanceled:
		env.Message = "request canceled"
	default:
		env.Code = CodeInternal.String()
		env.Message = "internal error"
		return CodeInternal.HTTPStatus(), env
	}
	return code.HTTPStatus(), env
}

// Mapper translates a service's own errors into *Error at the boundary. A
// mapper returns its input unchanged when it does not recognize it.
type Mapper func(error) error

// Map applies mappers in order and returns as soon as one yields an *Error.
// nil and errors that already are *Error pass through untouched.
func Map(err error, mappers ...Mapper) error {
	if err == nil {
		return nil
	}
	if _, ok := stderrors.AsType[*Error](err); ok {
		return err
	}
	for _, m := range mappers {
		mapped := m(err)
		if mapped == nil {
			return nil
		}
		if _, ok := stderrors.AsType[*Error](mapped); ok {
			return mapped
		}
		err = mapped
	}
	return err
}

// Constructors, one per code. Each takes a format string and arguments for
// the public-safe message.

// Canceled creates an error with CodeCanceled.
func Canceled(format string, args ...any) *Error {
	return Newf(CodeCanceled, format, args...)
}

// Unknown creates an error with CodeUnknown.
func Unknown(format string, args ...any) *Error {
	return Newf(CodeUnknown, format, args...)
}

// InvalidArgument creates an error with CodeInvalidArgument.
func InvalidArgument(format string, args ...any) *Error {
	return Newf(CodeInvalidArgument, format, args...)
}

// DeadlineExceeded creates an error with CodeDeadlineExceeded.
func DeadlineExceeded(format string, args ...any) *Error {
	return Newf(CodeDeadlineExceeded, format, args...)
}

// NotFound creates an error with CodeNotFound.
func NotFound(format string, args ...any) *Error {
	return Newf(CodeNotFound, format, args...)
}

// AlreadyExists creates an error with CodeAlreadyExists.
func AlreadyExists(format string, args ...any) *Error {
	return Newf(CodeAlreadyExists, format, args...)
}

// PermissionDenied creates an error with CodePermissionDenied.
func PermissionDenied(format string, args ...any) *Error {
	return Newf(CodePermissionDenied, format, args...)
}

// ResourceExhausted creates an error with CodeResourceExhausted.
func ResourceExhausted(format string, args ...any) *Error {
	return Newf(CodeResourceExhausted, format, args...)
}

// FailedPrecondition creates an error with CodeFailedPrecondition.
func FailedPrecondition(format string, args ...any) *Error {
	return Newf(CodeFailedPrecondition, format, args...)
}

// Aborted creates an error with CodeAborted.
func Aborted(format string, args ...any) *Error {
	return Newf(CodeAborted, format, args...)
}

// OutOfRange creates an error with CodeOutOfRange.
func OutOfRange(format string, args ...any) *Error {
	return Newf(CodeOutOfRange, format, args...)
}

// Unimplemented creates an error with CodeUnimplemented.
func Unimplemented(format string, args ...any) *Error {
	return Newf(CodeUnimplemented, format, args...)
}

// Internal creates an error with CodeInternal.
func Internal(format string, args ...any) *Error {
	return Newf(CodeInternal, format, args...)
}

// Unavailable creates an error with CodeUnavailable.
func Unavailable(format string, args ...any) *Error {
	return Newf(CodeUnavailable, format, args...)
}

// DataLoss creates an error with CodeDataLoss.
func DataLoss(format string, args ...any) *Error {
	return Newf(CodeDataLoss, format, args...)
}

// Unauthenticated creates an error with CodeUnauthenticated.
func Unauthenticated(format string, args ...any) *Error {
	return Newf(CodeUnauthenticated, format, args...)
}
