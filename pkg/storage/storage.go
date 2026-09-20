// Package storage is the backend-neutral side of object storage: the Bucket
// interface every storage provider implements (providers/s3), and signed
// upload tokens for the direct-upload handshake, where a browser uploads
// straight to the bucket and then tells the service which object it sent.
package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

// ErrNotFound is returned by Get, Stat and Delete for a key that does not exist.
var ErrNotFound = errors.New("storage: object not found")

// Bucket is one object store. Keys are slash-separated paths chosen by the
// service; never build them from user input without a prefix the service owns.
type Bucket interface {
	// Put stores r under key. size is the byte length, or -1 when unknown.
	Put(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) error
	// Get opens the object; the caller closes it.
	Get(ctx context.Context, key string) (io.ReadCloser, Info, error)
	// Stat describes the object without reading it.
	Stat(ctx context.Context, key string) (Info, error)
	// Delete removes the object.
	Delete(ctx context.Context, key string) error
	// PresignPut returns a URL a client can PUT the object to, and the headers
	// it must send with it. The constraints in opts are part of the signature:
	// an upload with another type, checksum or length is refused by the store.
	PresignPut(ctx context.Context, key string, opts PresignPutOptions) (url string, headers http.Header, err error)
	// PresignGet returns a URL a client can GET the object from until it expires.
	PresignGet(ctx context.Context, key string, opts PresignGetOptions) (url string, err error)
}

// Info describes a stored object.
type Info struct {
	Key          string
	Size         int64
	ContentType  string
	ETag         string
	LastModified time.Time
}

// PutOptions describes an object being written by the service.
type PutOptions struct {
	ContentType string
}

// PresignPutOptions constrains a direct upload. Zero values are not enforced.
type PresignPutOptions struct {
	ContentType   string
	ContentMD5    string        // base64 of the MD5 digest, as browsers and SDKs send it
	ContentLength int64         // exact size in bytes
	Expires       time.Duration // default: the provider's presign expiry
}

// PresignGetOptions shapes a download link.
type PresignGetOptions struct {
	Filename    string        // download under this name (Content-Disposition: attachment)
	Inline      bool          // with Filename: display in the browser instead of downloading
	ContentType string        // override the stored content type
	Expires     time.Duration // default: the provider's presign expiry
}
