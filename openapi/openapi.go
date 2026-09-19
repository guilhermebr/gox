package openapi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	"github.com/pb33f/libopenapi-validator/config"
	verrors "github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/paths"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/guilhermebr/gox"
)

// Violation is one way a request breaks the contract. A rejected request
// carries them under the error's "errors" detail.
type Violation struct {
	Code    string `json:"code"`    // what kind of rule failed: "body", "query", "path", "header", "cookie"
	Message string `json:"message"` // what is wrong, safe to show to the caller
	Pointer string `json:"pointer"` // JSON pointer into the body ("/amountCents"), or "/<name>" for a parameter
}

// Enable validates every request that one of the documents describes: path,
// query, header and cookie parameters and the request body. A request that
// breaks the contract is a 400 invalid_argument error whose "errors" detail
// lists the violations; the body stays readable for the handler. Requests no
// document describes go on to the mux untouched, so routes outside the
// contract (health, webhooks, pages) keep working.
//
// Documents are usually embedded: openapi.Enable(api.Spec). Validation runs
// with the other feature middleware, in the order the options are passed:
// put an authenticating feature first so anonymous callers get a 401, not a
// description of the contract.
func Enable(documents ...[]byte) gox.Option {
	return func(b *gox.Builder) error {
		if len(documents) == 0 {
			return errors.New("openapi: Enable needs at least one document")
		}
		var contracts []*contract
		for i, raw := range documents {
			c, err := load(raw)
			if err != nil {
				return fmt.Errorf("openapi: document %d: %w", i+1, err)
			}
			contracts = append(contracts, c)
		}
		b.Middleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for _, c := range contracts {
					violations, described := c.check(r)
					if !described {
						continue
					}
					if len(violations) > 0 {
						gox.Error(w, r, gox.InvalidArgument("the request does not match the API contract").WithDetail("errors", violations))
						return
					}
					break
				}
				next.ServeHTTP(w, r)
			})
		})
		return nil
	}
}

type contract struct {
	model     *v3.Document
	validator validator.Validator
}

func load(raw []byte) (*contract, error) {
	doc, err := libopenapi.NewDocument(raw)
	if err != nil {
		return nil, err
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		return nil, err
	}
	// Formats (uuid, date, email) are part of the contract. Security schemes
	// are not checked here: authentication is the service's own middleware.
	v, errs := validator.NewValidator(doc, config.WithFormatAssertions(), config.WithoutSecurityValidation())
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &contract{model: &model.Model, validator: v}, nil
}

// check reports the violations of r, and whether the document describes the
// request's path and method at all.
func (c *contract) check(r *http.Request) ([]Violation, bool) {
	item, _, pathValue := paths.FindPath(r, c.model, nil)
	if item == nil || item.GetOperations().GetOrZero(strings.ToLower(r.Method)) == nil {
		return nil, false
	}
	// The validator consumes the body; give the handler a fresh one.
	if r.Body != nil && r.Body != http.NoBody {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			return []Violation{{Code: "body", Message: "the request body could not be read"}}, true
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		defer func() { r.Body = io.NopCloser(bytes.NewReader(raw)) }()
	}
	ok, failures := c.validator.ValidateHttpRequestSyncWithPathItem(r, item, pathValue)
	if ok {
		return nil, true
	}
	return violationsOf(failures), true
}

func violationsOf(failures []*verrors.ValidationError) []Violation {
	var out []Violation
	for _, f := range failures {
		code := kind(f)
		if len(f.SchemaValidationErrors) == 0 || code != "body" {
			v := Violation{Code: code, Message: strings.TrimSpace(f.Message + ": " + f.Reason)}
			if f.ParameterName != "" {
				v.Pointer = "/" + f.ParameterName
			}
			out = append(out, v)
			continue
		}
		for _, s := range f.SchemaValidationErrors {
			pointer := s.FieldPath
			if len(s.InstancePath) > 0 {
				pointer = "/" + strings.Join(s.InstancePath, "/")
			}
			if !strings.HasPrefix(pointer, "/") {
				pointer = ""
			}
			out = append(out, Violation{Code: code, Message: s.Reason, Pointer: pointer})
		}
	}
	return out
}

func kind(f *verrors.ValidationError) string {
	switch f.ValidationType {
	case "parameter", "path", "query", "header", "cookie":
		switch f.ValidationSubType {
		case "path", "query", "header", "cookie":
			return f.ValidationSubType
		}
		if f.ValidationType != "parameter" {
			return f.ValidationType
		}
		return "parameter"
	}
	return "body"
}
