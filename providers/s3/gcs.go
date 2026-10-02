package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// Google Cloud Storage speaks the S3 API, with two differences the SDK's
// defaults trip over: its front end appends ",gzip(gfe)" to Accept-Encoding
// before it checks the signature, so a signed Accept-Encoding never
// matches (every call fails with SignatureDoesNotMatch, or a bare 403 on
// HEAD); and it refuses the x-amz-checksum-* headers the SDK adds to every
// upload. gcsOptions adapts a client to both.
func gcsOptions(o *awss3.Options) {
	o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	o.APIOptions = append(o.APIOptions, unsignedAcceptEncoding)
}

// unsignedAcceptEncoding moves the SDK's "Accept-Encoding: identity" after
// signing. The header stays, so the HTTP client never gunzips an object on
// its own; it just leaves the signature.
func unsignedAcceptEncoding(stack *middleware.Stack) error {
	if _, ok := stack.Finalize.Get(disableGzipID); ok {
		if _, err := stack.Finalize.Remove(disableGzipID); err != nil {
			return err
		}
	}
	if _, ok := stack.Finalize.Get(signingID); !ok {
		return nil // a presigned URL: the browser sends its own headers
	}
	return stack.Finalize.Insert(acceptIdentity{}, signingID, middleware.After)
}

const (
	disableGzipID = "DisableAcceptEncodingGzip" // the SDK's own step, which runs before signing
	signingID     = "Signing"
)

type acceptIdentity struct{}

func (acceptIdentity) ID() string { return "AcceptEncodingIdentityUnsigned" }

func (acceptIdentity) HandleFinalize(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
	if req, ok := in.Request.(*smithyhttp.Request); ok {
		req.Header.Set("Accept-Encoding", "identity")
	}
	return next.HandleFinalize(ctx, in)
}
