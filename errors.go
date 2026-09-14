package gox

import "github.com/guilhermebr/gox/pkg/errors"

// The error model, re-exported so services import only gox. The concrete
// type stays in pkg/errors; services work with codes and constructors.

// Code classifies a coded error.
type Code = errors.Code

// Codes.
const (
	CodeOK                 = errors.CodeOK
	CodeCanceled           = errors.CodeCanceled
	CodeUnknown            = errors.CodeUnknown
	CodeInvalidArgument    = errors.CodeInvalidArgument
	CodeDeadlineExceeded   = errors.CodeDeadlineExceeded
	CodeNotFound           = errors.CodeNotFound
	CodeAlreadyExists      = errors.CodeAlreadyExists
	CodePermissionDenied   = errors.CodePermissionDenied
	CodeResourceExhausted  = errors.CodeResourceExhausted
	CodeFailedPrecondition = errors.CodeFailedPrecondition
	CodeAborted            = errors.CodeAborted
	CodeOutOfRange         = errors.CodeOutOfRange
	CodeUnimplemented      = errors.CodeUnimplemented
	CodeInternal           = errors.CodeInternal
	CodeUnavailable        = errors.CodeUnavailable
	CodeDataLoss           = errors.CodeDataLoss
	CodeUnauthenticated    = errors.CodeUnauthenticated
)

// Envelope is the JSON error body every gox response uses.
type Envelope = errors.Envelope

// NewError creates a coded error with a public-safe message.
func NewError(code Code, msg string) *errors.Error { return errors.New(code, msg) }

// WrapError keeps cause for errors.Is and logs while msg is what clients see.
func WrapError(cause error, code Code, msg string) *errors.Error {
	return errors.Wrap(cause, code, msg)
}

// CodeOf returns the code of err through wrapping: OK for nil, Unknown for foreign errors.
func CodeOf(err error) Code { return errors.CodeOf(err) }

// HTTPStatus returns the HTTP status err renders as.
func HTTPStatus(err error) int { return errors.HTTPStatus(err) }

// Canceled creates a 499 error: the client went away.
func Canceled(format string, args ...any) *errors.Error { return errors.Canceled(format, args...) }

// Unknown creates a 500 error of unknown cause.
func Unknown(format string, args ...any) *errors.Error { return errors.Unknown(format, args...) }

// InvalidArgument creates a 400 error: the request is malformed or fails validation.
func InvalidArgument(format string, args ...any) *errors.Error {
	return errors.InvalidArgument(format, args...)
}

// DeadlineExceeded creates a 504 error.
func DeadlineExceeded(format string, args ...any) *errors.Error {
	return errors.DeadlineExceeded(format, args...)
}

// NotFound creates a 404 error.
func NotFound(format string, args ...any) *errors.Error { return errors.NotFound(format, args...) }

// AlreadyExists creates a 409 error: the resource already exists.
func AlreadyExists(format string, args ...any) *errors.Error {
	return errors.AlreadyExists(format, args...)
}

// PermissionDenied creates a 403 error.
func PermissionDenied(format string, args ...any) *errors.Error {
	return errors.PermissionDenied(format, args...)
}

// ResourceExhausted creates a 429 error.
func ResourceExhausted(format string, args ...any) *errors.Error {
	return errors.ResourceExhausted(format, args...)
}

// FailedPrecondition creates a 400 error: the system is not in the required state.
func FailedPrecondition(format string, args ...any) *errors.Error {
	return errors.FailedPrecondition(format, args...)
}

// Aborted creates a 409 error: a concurrency conflict; the client may retry.
func Aborted(format string, args ...any) *errors.Error { return errors.Aborted(format, args...) }

// OutOfRange creates a 400 error.
func OutOfRange(format string, args ...any) *errors.Error { return errors.OutOfRange(format, args...) }

// Unimplemented creates a 501 error.
func Unimplemented(format string, args ...any) *errors.Error {
	return errors.Unimplemented(format, args...)
}

// Internal creates a 500 error with a public-safe message.
func Internal(format string, args ...any) *errors.Error { return errors.Internal(format, args...) }

// Unavailable creates a 503 error: retry later.
func Unavailable(format string, args ...any) *errors.Error {
	return errors.Unavailable(format, args...)
}

// DataLoss creates a 500 error.
func DataLoss(format string, args ...any) *errors.Error { return errors.DataLoss(format, args...) }

// Unauthenticated creates a 401 error.
func Unauthenticated(format string, args ...any) *errors.Error {
	return errors.Unauthenticated(format, args...)
}

// ErrorMapper translates a service's own errors into coded errors at the
// boundary. See WithErrorMapper.
type ErrorMapper = errors.Mapper

// WithErrorMapper registers a mapper applied by gox.Error before rendering,
// so a service keeps its domain sentinels and maps them once.
func WithErrorMapper(m ErrorMapper) Option {
	return func(b *Builder) error {
		b.mappers = append(b.mappers, m)
		return nil
	}
}

// MapError applies the registered mappers to err. gox.Error calls it; a
// service can call it too when rendering errors itself.
func (a *App) MapError(err error) error {
	return errors.Map(err, a.mappers...)
}
