package storagetest_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/guilhermebr/gox/pkg/storage"
	"github.com/guilhermebr/gox/pkg/storage/storagetest"
)

var ctx = context.Background()

func TestPutGetStatAndDeleteBehaveLikeAStore(t *testing.T) {
	b := storagetest.New()
	if _, _, err := b.Get(ctx, "a/1"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get of a missing key: %v", err)
	}
	if _, err := b.Stat(ctx, "a/1"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Stat of a missing key: %v", err)
	}
	if err := b.Put(ctx, "a/1", strings.NewReader("hello"), 5, storage.PutOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	rc, info, err := b.Get(ctx, "a/1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(rc)
	if string(body) != "hello" || info.Key != "a/1" || info.Size != 5 || info.ContentType != "text/plain" || info.LastModified.IsZero() {
		t.Fatalf("body=%q info=%+v", body, info)
	}
	if st, err := b.Stat(ctx, "a/1"); err != nil || st.Size != 5 {
		t.Fatalf("Stat: %+v %v", st, err)
	}
	if !b.Has("a/1") {
		t.Fatal("Has must report a stored key")
	}
	if err := b.Delete(ctx, "a/1"); err != nil {
		t.Fatal(err)
	}
	if err := b.Delete(ctx, "never-there"); err != nil {
		t.Fatalf("deleting a missing key is not an error on S3: %v", err)
	}
	if b.Has("a/1") {
		t.Fatal("the object survived Delete")
	}
	if got := b.Deleted(); len(got) != 2 || got[0] != "a/1" || got[1] != "never-there" {
		t.Fatalf("Deleted = %v", got)
	}
}

func TestUploadStoresABodyThatMatchesThePresignedConstraints(t *testing.T) {
	b := storagetest.New()
	body := []byte("%PDF-1.7")
	url, h, err := b.PresignPut(ctx, "docs/1.pdf", storage.PresignPutOptions{ContentType: "application/pdf", ContentMD5: storagetest.Checksum(body), ContentLength: int64(len(body))})
	if err != nil {
		t.Fatal(err)
	}
	if url != storagetest.BaseURL+"docs/1.pdf" || h.Get("Content-Type") != "application/pdf" || h.Get("Content-Md5") != storagetest.Checksum(body) || h.Get("Content-Length") != "8" {
		t.Fatalf("url=%q headers=%v", url, h)
	}
	if err := b.Upload(url, h, body); err != nil {
		t.Fatal(err)
	}
	if st, err := b.Stat(ctx, "docs/1.pdf"); err != nil || st.Size != 8 || st.ContentType != "application/pdf" {
		t.Fatalf("Stat: %+v %v", st, err)
	}
}

func TestUploadIsRefusedWhenItBreaksWhatTheURLWasSignedFor(t *testing.T) {
	body := []byte("12345")
	signed := storage.PresignPutOptions{ContentType: "image/png", ContentMD5: storagetest.Checksum(body), ContentLength: 5}
	good := http.Header{"Content-Type": {"image/png"}}
	cases := map[string]struct {
		url     string
		headers http.Header
		body    []byte
	}{
		"no presigned upload": {storagetest.BaseURL + "other", good, body},
		"another type":        {storagetest.BaseURL + "k", http.Header{"Content-Type": {"image/jpeg"}}, body},
		"another size":        {storagetest.BaseURL + "k", good, []byte("123456")},
		"another digest":      {storagetest.BaseURL + "k", good, []byte("54321")},
	}
	for name, c := range cases {
		b := storagetest.New()
		if _, _, err := b.PresignPut(ctx, "k", signed); err != nil {
			t.Fatal(err)
		}
		if err := b.Upload(c.url, c.headers, c.body); !errors.Is(err, storagetest.ErrUploadRefused) {
			t.Errorf("%s: err = %v, want ErrUploadRefused", name, err)
		}
		if b.Has("k") || b.Has("other") {
			t.Errorf("%s: a refused upload was stored", name)
		}
	}
}

func TestUploadDoesNotEnforceConstraintsLeftAtZero(t *testing.T) {
	b := storagetest.New()
	url, h, err := b.PresignPut(ctx, "k", storage.PresignPutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 0 {
		t.Fatalf("headers = %v, want none", h)
	}
	if err := b.Upload(url, http.Header{"Content-Type": {"text/csv"}}, []byte("a,b")); err != nil {
		t.Fatal(err)
	}
}

func TestPresignGetReturnsALinkAndRecordsItsOptions(t *testing.T) {
	b := storagetest.New()
	url, err := b.PresignGet(ctx, "exports/1.zip", storage.PresignGetOptions{Filename: "export.zip"})
	if err != nil || !strings.HasPrefix(url, storagetest.BaseURL+"exports/1.zip") {
		t.Fatalf("url=%q err=%v", url, err)
	}
	if got := b.Links(); len(got) != 1 || got[0].Filename != "export.zip" {
		t.Fatalf("Links = %+v", got)
	}
}
