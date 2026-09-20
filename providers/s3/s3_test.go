package s3_test

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/providers/s3"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func setArgs(t *testing.T) {
	t.Helper()
	old := os.Args
	os.Args = []string{"svc"}
	t.Cleanup(func() { os.Args = old })
}

func TestFromPanicsWithoutEnable(t *testing.T) {
	setArgs(t)
	a, _ := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()))
	defer func() {
		if r := recover(); r != "gox: s3.From called but s3.Enable() was not passed to gox.New" {
			t.Fatalf("panic = %v", r)
		}
	}()
	s3.From(a)
}

func TestConfigNamesWhatIsWrong(t *testing.T) {
	cases := map[string]map[string]string{
		"SHOP_S3_BUCKET":       {},
		"S3_ENDPOINT":          {"SHOP_S3_BUCKET": "b", "SHOP_S3_ENDPOINT": "minio:9000"},
		"S3_SECRET_ACCESS_KEY": {"SHOP_S3_BUCKET": "b", "SHOP_S3_ACCESS_KEY_ID": "AKIA"},
		"S3_PATH_STYLE":        {"SHOP_S3_BUCKET": "b", "SHOP_S3_PATH_STYLE": "maybe"},
		"S3_PRESIGN_EXPIRY":    {"SHOP_S3_BUCKET": "b", "SHOP_S3_PRESIGN_EXPIRY": "200h"},
	}
	for want, env := range cases {
		t.Run(want, func(t *testing.T) {
			setArgs(t)
			for k, v := range env {
				t.Setenv(k, v)
			}
			_, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), s3.Enable())
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
