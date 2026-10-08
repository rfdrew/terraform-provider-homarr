// Package client is a thin HTTP client for the Homarr OpenAPI surface.
//
// Homarr exposes two transports: tRPC under /api/trpc/... and an
// OpenAPI-compatible REST surface under /api/... . Only the latter is stable
// enough to build a provider on, so this client speaks REST exclusively.
// Authentication is a single `ApiKey: <id>.<token>` header.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// apiKeyHeader matches API_KEY_HEADER_NAME in packages/auth/api-key/constants.ts.
const apiKeyHeader = "ApiKey"

// Client talks to a single Homarr instance.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	userAgent  string
}

// Options configures a Client.
type Options struct {
	// BaseURL is the Homarr origin, e.g. https://homarr.example.com. Any
	// trailing slash and any trailing /api are trimmed so that both forms work.
	BaseURL string
	// APIKey is the `<id>.<token>` pair issued in Management > Tools > API.
	APIKey string
	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool
	// Timeout bounds a single HTTP request. Defaults to 30s.
	Timeout time.Duration
	// UserAgent is sent with every request.
	UserAgent string
}

// New builds a Client from Options.
func New(opts Options) (*Client, error) {
	base := strings.TrimRight(opts.BaseURL, "/")
	base = strings.TrimSuffix(base, "/api")
	if base == "" {
		return nil, errors.New("base URL must not be empty")
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return nil, fmt.Errorf("base URL must start with http:// or https://, got %q", opts.BaseURL)
	}
	if opts.APIKey == "" {
		return nil, errors.New("API key must not be empty")
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if opts.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in
	}

	ua := opts.UserAgent
	if ua == "" {
		ua = "terraform-provider-homarr"
	}

	return &Client{
		baseURL:    base,
		apiKey:     opts.APIKey,
		httpClient: &http.Client{Timeout: timeout, Transport: transport},
		userAgent:  ua,
	}, nil
}

// BaseURL returns the normalised Homarr origin.
func (c *Client) BaseURL() string { return c.baseURL }

// Error is a non-2xx response from Homarr.
//
// Homarr funnels REST calls through trpc-to-openapi, so failures arrive as a
// tRPC error envelope: {"message":..., "code":..., "data":{"httpStatus":...},
// "issues":[...]}. Validation failures additionally carry a zodError with
// per-field messages, which we flatten into FieldErrors for actionable
// diagnostics.
type Error struct {
	StatusCode  int
	Code        string
	Message     string
	FieldErrors map[string][]string
	Body        string
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "homarr API error: HTTP %d", e.StatusCode)
	if e.Code != "" {
		fmt.Fprintf(&b, " (%s)", e.Code)
	}
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	if len(e.FieldErrors) > 0 {
		fields := make([]string, 0, len(e.FieldErrors))
		for field, msgs := range e.FieldErrors {
			fields = append(fields, fmt.Sprintf("%s: %s", field, strings.Join(msgs, "; ")))
		}
		fmt.Fprintf(&b, " [%s]", strings.Join(fields, ", "))
	}
	if e.Message == "" && len(e.FieldErrors) == 0 && e.Body != "" {
		fmt.Fprintf(&b, ": %s", truncate(e.Body, 512))
	}
	return b.String()
}

// IsNotFound reports whether err is a 404 from Homarr. Callers use this to
// drop a resource from state instead of failing the plan.
func IsNotFound(err error) bool {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// IsConflict reports whether err is a 409 from Homarr, e.g. a duplicate
// username or board name.
func IsConflict(err error) bool {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusConflict
	}
	return false
}

// IsForbidden reports whether err is a 403 from Homarr. Homarr 2.0.0 narrowed
// GET /api/apps to the app-modify-all permission, so a scoped key that used to
// list apps now gets this.
func IsForbidden(err error) bool {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusForbidden
	}
	return false
}

type trpcErrorEnvelope struct {
	Message string `json:"message"`
	Code    string `json:"code"`
	Data    struct {
		Code       string `json:"code"`
		HTTPStatus int    `json:"httpStatus"`
		ZodError   *struct {
			FormErrors  []string            `json:"formErrors"`
			FieldErrors map[string][]string `json:"fieldErrors"`
		} `json:"zodError"`
	} `json:"data"`
}

// do issues a request against path (which must start with /api/) and decodes a
// JSON response body into out when out is non-nil. Several Homarr mutations
// declare `.output(z.void())` and return an empty body; passing out == nil
// handles those.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body for %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("building request for %s %s: %w", method, path, err)
	}

	req.Header.Set(apiKeyHeader, c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	// The route handler defaults a missing Content-Type to JSON, but setting it
	// explicitly keeps behaviour identical for bodyless requests too.
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body for %s %s: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newError(resp.StatusCode, raw)
	}

	if out == nil {
		return nil
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return fmt.Errorf("%s %s: expected a JSON response body but got none", method, path)
	}
	if err := json.Unmarshal(trimmed, out); err != nil {
		return fmt.Errorf("decoding response body for %s %s: %w (body: %s)", method, path, err, truncate(string(trimmed), 512))
	}
	return nil
}

func newError(status int, raw []byte) *Error {
	apiErr := &Error{StatusCode: status, Body: string(raw)}

	var envelope trpcErrorEnvelope
	if err := json.Unmarshal(bytes.TrimSpace(raw), &envelope); err == nil {
		apiErr.Message = envelope.Message
		apiErr.Code = envelope.Code
		if envelope.Data.Code != "" {
			apiErr.Code = envelope.Data.Code
		}
		if envelope.Data.ZodError != nil {
			if len(envelope.Data.ZodError.FieldErrors) > 0 {
				apiErr.FieldErrors = envelope.Data.ZodError.FieldErrors
			}
			if len(envelope.Data.ZodError.FormErrors) > 0 {
				if apiErr.FieldErrors == nil {
					apiErr.FieldErrors = map[string][]string{}
				}
				apiErr.FieldErrors["(form)"] = envelope.Data.ZodError.FormErrors
			}
		}
	}
	return apiErr
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…(truncated)"
}
