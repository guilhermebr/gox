package web

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	gerrors "github.com/guilhermebr/gox/pkg/errors"
)

// FieldErrors maps a form field to its validation message, ready to be
// rendered next to the field.
type FieldErrors map[string]string

// Error implements error so FieldErrors can travel as one.
func (e FieldErrors) Error() string {
	parts := make([]string, 0, len(e))
	for k, v := range e {
		parts = append(parts, k+": "+v)
	}
	return "web: invalid form: " + strings.Join(parts, "; ")
}

// Form decodes an application/x-www-form-urlencoded or multipart body into
// T and validates it from struct tags:
//
//	Email string        `form:"email,required"`
//	Age   int           `form:"age,min=18,max=120"`
//	Note  string        `form:"note,max=280"`      // max on strings is a length
//	Tags  []string      `form:"tags"`
//	Wait  time.Duration `form:"wait"`
//	Agree bool          `form:"agree,required"`    // required means checked
//
// Validation failures come back as FieldErrors keyed by the form tag name
// ("email", not "Email") with nil error; messages are "required", "must be
// at least N characters", "must be at most N characters", "must be at least
// N", "must be at most N", "must be a whole number", "must be a number" and
// "must be a duration such as 30s or 5m". A body that is not a form, or a T
// that is not a struct, is an error.
func Form[T any](r *http.Request) (T, FieldErrors, error) {
	var out T
	rv := reflect.ValueOf(&out).Elem()
	if rv.Kind() != reflect.Struct {
		return out, nil, fmt.Errorf("web: Form[%T]: target must be a struct", out)
	}
	if !isForm(r) {
		return out, nil, gerrors.InvalidArgument("expected a form body, got %q", r.Header.Get("Content-Type"))
	}
	if err := r.ParseMultipartForm(10 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return out, nil, gerrors.Wrap(err, gerrors.CodeInvalidArgument, "malformed form body")
	}
	ferrs := FieldErrors{}
	rt := rv.Type()
	for i := range rt.NumField() {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		tag, ok := sf.Tag.Lookup("form")
		if !ok {
			continue
		}
		name, rules := parseTag(tag)
		if name == "" || name == "-" {
			continue
		}
		values, present := r.PostForm[name]
		if !present {
			values = r.Form[name]
		}
		if msg := setField(rv.Field(i), values, rules); msg != "" {
			ferrs[name] = msg
		}
	}
	if len(ferrs) == 0 {
		return out, nil, nil
	}
	return out, ferrs, nil
}

type rules struct {
	required bool
	min, max *float64
}

func parseTag(tag string) (string, rules) {
	parts := strings.Split(tag, ",")
	var r rules
	for _, p := range parts[1:] {
		switch {
		case p == "required":
			r.required = true
		case strings.HasPrefix(p, "min="):
			if v, err := strconv.ParseFloat(p[4:], 64); err == nil {
				r.min = &v
			}
		case strings.HasPrefix(p, "max="):
			if v, err := strconv.ParseFloat(p[4:], 64); err == nil {
				r.max = &v
			}
		}
	}
	return strings.TrimSpace(parts[0]), r
}

// setField parses values into field and returns a validation message, or "".
func setField(field reflect.Value, values []string, r rules) string {
	first := ""
	if len(values) > 0 {
		first = strings.TrimSpace(values[0])
	}
	switch field.Kind() {
	case reflect.String:
		if first == "" {
			if r.required {
				return "required"
			}
			return ""
		}
		if r.min != nil && float64(len(first)) < *r.min {
			return fmt.Sprintf("must be at least %d characters", int(*r.min))
		}
		if r.max != nil && float64(len(first)) > *r.max {
			return fmt.Sprintf("must be at most %d characters", int(*r.max))
		}
		field.SetString(first)
	case reflect.Bool:
		v := isTruthy(first)
		if r.required && !v {
			return "required"
		}
		field.SetBool(v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if field.Type() == reflect.TypeFor[time.Duration]() {
			if first == "" {
				if r.required {
					return "required"
				}
				return ""
			}
			d, err := time.ParseDuration(first)
			if err != nil {
				return "must be a duration such as 30s or 5m"
			}
			field.SetInt(int64(d))
			return ""
		}
		if first == "" {
			if r.required {
				return "required"
			}
			return ""
		}
		n, err := strconv.ParseInt(first, 10, 64)
		if err != nil {
			return "must be a whole number"
		}
		if msg := checkRange(float64(n), r); msg != "" {
			return msg
		}
		field.SetInt(n)
	case reflect.Float32, reflect.Float64:
		if first == "" {
			if r.required {
				return "required"
			}
			return ""
		}
		x, err := strconv.ParseFloat(first, 64)
		if err != nil {
			return "must be a number"
		}
		if msg := checkRange(x, r); msg != "" {
			return msg
		}
		field.SetFloat(x)
	case reflect.Slice:
		if field.Type().Elem().Kind() != reflect.String {
			return ""
		}
		var out []string
		for _, v := range values {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
		if r.required && len(out) == 0 {
			return "required"
		}
		field.Set(reflect.ValueOf(out))
	}
	return ""
}

func checkRange(x float64, r rules) string {
	if r.min != nil && x < *r.min {
		return "must be at least " + strconv.FormatFloat(*r.min, 'f', -1, 64)
	}
	if r.max != nil && x > *r.max {
		return "must be at most " + strconv.FormatFloat(*r.max, 'f', -1, 64)
	}
	return ""
}

func isTruthy(s string) bool {
	switch strings.ToLower(s) {
	case "on", "true", "1", "yes":
		return true
	}
	return false
}
