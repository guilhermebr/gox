# Add file uploads (direct to the bucket, then attach)

The browser uploads straight to object storage; the service never proxies
the bytes. The service chooses the key, the store enforces type, size and
checksum, and a signed token proves later which object the caller was
allowed to write.

```go path=main.go
package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"time"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/storage"
	"github.com/guilhermebr/gox/providers/s3"
)

// Config adds the token secret next to the framework's settings.
type Config struct {
	gox.BaseConfig
	UploadSecret string `conf:"required,mask,help:signs upload tokens; at least 32 bytes"`
}

func main() {
	// SHOP_S3_BUCKET, and for MinIO or R2: SHOP_S3_ENDPOINT, SHOP_S3_ACCESS_KEY_ID,
	// SHOP_S3_SECRET_ACCESS_KEY. The bucket is checked at boot.
	var cfg Config
	a := gox.MustNew("shop", gox.WithConfig(&cfg), gox.HTTP(), s3.Enable())
	signer, err := storage.NewSigner([]byte(cfg.UploadSecret))
	if err != nil {
		a.Log().Error("upload secret", "error", err)
		os.Exit(1)
	}
	userID := func(*http.Request) string { return "user_01" } // from your session

	// 1. Ask where to upload. The client sends what it is about to upload.
	a.HandleFunc("POST /uploads", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Filename    string `json:"filename"`
			ContentType string `json:"contentType"`
			ByteSize    int64  `json:"byteSize"`
			Checksum    string `json:"checksum"` // base64 MD5
		}
		if err := gox.Decode(r, &in); err != nil {
			gox.Error(w, r, err)
			return
		}
		if in.ByteSize <= 0 || in.ByteSize > 25<<20 {
			gox.Error(w, r, gox.InvalidArgument("files may be up to 25 MB"))
			return
		}
		random := make([]byte, 16)
		_, _ = rand.Read(random)
		key := "uploads/" + hex.EncodeToString(random) // never the client's filename
		url, headers, err := s3.From(a).PresignPut(r.Context(), key, storage.PresignPutOptions{
			ContentType: in.ContentType, ContentMD5: in.Checksum, ContentLength: in.ByteSize,
		})
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		token := signer.Sign(storage.Upload{
			Key: key, Subject: userID(r), Expires: time.Now().Add(time.Hour),
			Meta: map[string]string{"filename": in.Filename, "contentType": in.ContentType},
		})
		_ = gox.JSON(w, http.StatusCreated, map[string]any{"url": url, "headers": headers, "token": token})
	})

	// 2. The client PUTs the file to url with those headers, then submits the
	// token with the form it belongs to.
	a.HandleFunc("POST /invoices/{id}/attachments", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Token string `json:"token"`
		}
		if err := gox.Decode(r, &in); err != nil {
			gox.Error(w, r, err)
			return
		}
		up, err := signer.Verify(in.Token)
		if err != nil || up.Subject != userID(r) {
			gox.Error(w, r, gox.InvalidArgument("the upload is not valid for this user"))
			return
		}
		info, err := s3.From(a).Stat(r.Context(), up.Key) // it must really be there
		if err != nil {
			gox.Error(w, r, gox.InvalidArgument("the file was not uploaded"))
			return
		}
		// Store up.Key, up.Meta["filename"] and info.Size with the invoice here.
		_ = gox.JSON(w, http.StatusCreated, map[string]any{"filename": up.Meta["filename"], "size": info.Size})
	})

	// 3. Downloads are short-lived links, not proxied bytes.
	a.HandleFunc("GET /attachments/{key...}", func(w http.ResponseWriter, r *http.Request) {
		link, err := s3.From(a).PresignGet(r.Context(), r.PathValue("key"), storage.PresignGetOptions{Filename: "attachment.pdf"})
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		http.Redirect(w, r, link, http.StatusSeeOther)
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

Browsers need a CORS rule on the bucket allowing `PUT` from the site's
origin with the signed headers. Server-side writes (exports, generated
files) use `Put`; `Get`, `Stat` and `Delete` return `storage.ErrNotFound`
for a missing key.
