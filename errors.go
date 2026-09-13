package gox

import "github.com/guilhermebr/gox/pkg/errors"

// Re-exports of the error model so services import only gox.

// Error is a coded error with a public-safe message.
type Error = errors.Error

// Code classifies an Error.
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

// Constructors, one per code, plus wrapping and inspection helpers.
var (
	NewError           = errors.New
	WrapError          = errors.Wrap
	Canceled           = errors.Canceled
	Unknown            = errors.Unknown
	InvalidArgument    = errors.InvalidArgument
	DeadlineExceeded   = errors.DeadlineExceeded
	NotFound           = errors.NotFound
	AlreadyExists      = errors.AlreadyExists
	PermissionDenied   = errors.PermissionDenied
	ResourceExhausted  = errors.ResourceExhausted
	FailedPrecondition = errors.FailedPrecondition
	Aborted            = errors.Aborted
	OutOfRange         = errors.OutOfRange
	Unimplemented      = errors.Unimplemented
	Internal           = errors.Internal
	Unavailable        = errors.Unavailable
	DataLoss           = errors.DataLoss
	Unauthenticated    = errors.Unauthenticated
	CodeOf             = errors.CodeOf
	HTTPStatus         = errors.HTTPStatus
)

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
