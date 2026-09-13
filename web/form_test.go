package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox/web"
)

type signup struct {
	Email   string        `form:"email,required"`
	Age     int           `form:"age,min=18,max=120"`
	Agree   bool          `form:"agree,required"`
	Tags    []string      `form:"tags"`
	Timeout time.Duration `form:"timeout"`
	Score   float64       `form:"score"`
	Note    string        `form:"note,max=5"`
	secret  string        //nolint:unused // unexported fields are ignored
	NoTag   string
}

func post(values url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestFormDecodesTypesAndValidates(t *testing.T) {
	v := url.Values{
		"email": {"ana@example.com"}, "age": {"30"}, "agree": {"on"},
		"tags": {"a", "b"}, "timeout": {"1m30s"}, "score": {"9.5"}, "note": {"hi"},
	}
	got, ferrs, err := web.Form[signup](post(v))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ferrs != nil {
		t.Fatalf("field errors = %v", ferrs)
	}
	if got.Email != "ana@example.com" || got.Age != 30 || !got.Agree || len(got.Tags) != 2 ||
		got.Timeout != 90*time.Second || got.Score != 9.5 || got.Note != "hi" {
		t.Fatalf("decoded = %+v", got)
	}
}

func TestFormReportsEveryFieldError(t *testing.T) {
	v := url.Values{"age": {"12"}, "note": {"too long note"}}
	_, ferrs, err := web.Form[signup](post(v))
	if err != nil {
		t.Fatalf("err = %v; validation failures are field errors, not errors", err)
	}
	want := map[string]string{
		"email": "required",
		"age":   "must be at least 18",
		"agree": "required",
		"note":  "must be at most 5 characters",
	}
	for field, msg := range want {
		if ferrs[field] != msg {
			t.Errorf("%s = %q, want %q (all: %v)", field, ferrs[field], msg, ferrs)
		}
	}
	if ferrs.Error() == "" {
		t.Fatal("FieldErrors must implement error")
	}
}

func TestFormRejectsMalformedValues(t *testing.T) {
	v := url.Values{"email": {"x"}, "age": {"thirty"}, "agree": {"yes"}, "timeout": {"soon"}}
	_, ferrs, err := web.Form[signup](post(v))
	if err != nil {
		t.Fatal(err)
	}
	if ferrs["age"] != "must be a whole number" || ferrs["timeout"] != "must be a duration such as 30s or 5m" {
		t.Fatalf("field errors = %v", ferrs)
	}
}

func TestFormBoolSpellings(t *testing.T) {
	for _, s := range []string{"on", "true", "1", "yes"} {
		got, ferrs, _ := web.Form[signup](post(url.Values{"email": {"x"}, "age": {"20"}, "agree": {s}}))
		if !got.Agree || ferrs != nil {
			t.Fatalf("%q: agree=%v errs=%v", s, got.Agree, ferrs)
		}
	}
	got, ferrs, _ := web.Form[signup](post(url.Values{"email": {"x"}, "age": {"20"}, "agree": {"false"}}))
	if got.Agree || ferrs["agree"] != "required" {
		t.Fatalf("false: agree=%v errs=%v", got.Agree, ferrs)
	}
}

func TestFormNeedsAFormBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(`{"email":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	if _, _, err := web.Form[signup](req); err == nil {
		t.Fatal("a JSON body is not a form")
	}
}

func TestFormRejectsNonStructTargets(t *testing.T) {
	if _, _, err := web.Form[string](post(url.Values{})); err == nil {
		t.Fatal("Form[string] must be an error")
	}
}
