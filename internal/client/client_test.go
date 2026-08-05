package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewNormalisesBaseURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain origin", "https://homarr.example.com", "https://homarr.example.com"},
		{"trailing slash", "https://homarr.example.com/", "https://homarr.example.com"},
		{"trailing api path", "https://homarr.example.com/api", "https://homarr.example.com"},
		{"trailing api path with slash", "https://homarr.example.com/api/", "https://homarr.example.com"},
		{"http with port", "http://localhost:7575", "http://localhost:7575"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New(Options{BaseURL: tc.input, APIKey: "id.token"})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if c.BaseURL() != tc.want {
				t.Errorf("BaseURL() = %q, want %q", c.BaseURL(), tc.want)
			}
		})
	}
}

func TestNewRejectsBadOptions(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{"empty url", Options{BaseURL: "", APIKey: "id.token"}},
		{"url without scheme", Options{BaseURL: "homarr.example.com", APIKey: "id.token"}},
		{"empty api key", Options{BaseURL: "https://homarr.example.com", APIKey: ""}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.opts); err == nil {
				t.Fatal("New() expected an error, got nil")
			}
		})
	}
}

func TestSendsAPIKeyHeader(t *testing.T) {
	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("ApiKey")
		_ = json.NewEncoder(w).Encode(Info{Version: "1.73.0"})
	}))
	defer server.Close()

	c, err := New(Options{BaseURL: server.URL, APIKey: "abc123.secrettoken"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	info, err := c.GetInfo(context.Background())
	if err != nil {
		t.Fatalf("GetInfo() error = %v", err)
	}
	if gotKey != "abc123.secrettoken" {
		t.Errorf("ApiKey header = %q, want %q", gotKey, "abc123.secrettoken")
	}
	if info.Version != "1.73.0" {
		t.Errorf("Version = %q, want %q", info.Version, "1.73.0")
	}
}

func TestParsesTRPCValidationError(t *testing.T) {
	// A real 400 body from Homarr, produced by PATCH /api/apps/{id} with only a
	// name supplied.
	body := `{"message":"Input validation failed","code":"BAD_REQUEST","data":{"code":"BAD_REQUEST",` +
		`"httpStatus":400,"path":"appRouter.update","zodError":{"formErrors":[],"fieldErrors":{` +
		`"iconUrl":["Invalid input: expected string, received undefined"]}},"error":null}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	c, _ := New(Options{BaseURL: server.URL, APIKey: "id.token"})
	err := c.UpdateApp(context.Background(), "someid", AppRequest{Name: "x"})
	if err == nil {
		t.Fatal("UpdateApp() expected an error, got nil")
	}

	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error type = %T, want *client.Error", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
	if apiErr.Code != "BAD_REQUEST" {
		t.Errorf("Code = %q, want BAD_REQUEST", apiErr.Code)
	}
	if len(apiErr.FieldErrors["iconUrl"]) != 1 {
		t.Errorf("FieldErrors[iconUrl] = %v, want one message", apiErr.FieldErrors["iconUrl"])
	}
	// The rendered message must name the offending field so users can act on it.
	if got := apiErr.Error(); !contains(got, "iconUrl") {
		t.Errorf("Error() = %q, want it to mention iconUrl", got)
	}
}

func TestIsNotFoundAndIsConflict(t *testing.T) {
	notFoundBody := `{"message":"App not found","code":"NOT_FOUND","data":{"code":"NOT_FOUND","httpStatus":404}}`
	conflictBody := `{"message":"Username already taken","code":"CONFLICT","data":{"code":"CONFLICT","httpStatus":409}}`

	tests := []struct {
		name         string
		status       int
		body         string
		wantNotFound bool
		wantConflict bool
	}{
		{"404", http.StatusNotFound, notFoundBody, true, false},
		{"409", http.StatusConflict, conflictBody, false, true},
		{"500", http.StatusInternalServerError, `{"message":"boom"}`, false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			c, _ := New(Options{BaseURL: server.URL, APIKey: "id.token"})
			_, err := c.GetApp(context.Background(), "someid")
			if err == nil {
				t.Fatal("GetApp() expected an error, got nil")
			}
			if IsNotFound(err) != tc.wantNotFound {
				t.Errorf("IsNotFound() = %v, want %v", IsNotFound(err), tc.wantNotFound)
			}
			if IsConflict(err) != tc.wantConflict {
				t.Errorf("IsConflict() = %v, want %v", IsConflict(err), tc.wantConflict)
			}
		})
	}
}

func TestGetBoardSynthesises404WhenAbsent(t *testing.T) {
	// Homarr has no GET /api/boards/{id}, so the client filters the list. A
	// missing board must still look like a 404 to callers.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"other","name":"other","logoImageUrl":null,"isPublic":false,` +
			`"creator":null,"isHome":false,"isMobileHome":false,"userPermissions":[],"groupPermissions":[]}]`))
	}))
	defer server.Close()

	c, _ := New(Options{BaseURL: server.URL, APIKey: "id.token"})
	_, err := c.GetBoard(context.Background(), "missing")
	if err == nil {
		t.Fatal("GetBoard() expected an error, got nil")
	}
	if !IsNotFound(err) {
		t.Errorf("IsNotFound() = false, want true (err = %v)", err)
	}
}

func TestUserCreateOmitsNullEmail(t *testing.T) {
	// Homarr rejects `"email": null` with a 400, so an unset email has to be
	// omitted from the payload entirely.
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte(`[{"id":"newid","name":"someone","email":null,"emailVerified":null,"image":null}]`))
	}))
	defer server.Close()

	c, _ := New(Options{BaseURL: server.URL, APIKey: "id.token"})
	id, err := c.CreateUser(context.Background(), UserCreateRequest{
		Username:        "someone",
		Password:        "hunter2hunter2",
		ConfirmPassword: "hunter2hunter2",
		Email:           nil,
		GroupIDs:        []string{},
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if id != "newid" {
		t.Errorf("CreateUser() id = %q, want newid", id)
	}
	if _, present := gotBody["email"]; present {
		t.Errorf("request body contains an email key, want it omitted: %v", gotBody)
	}
	if _, present := gotBody["groupIds"]; !present {
		t.Errorf("request body is missing groupIds, which Homarr requires: %v", gotBody)
	}
}

func TestAppRequestSendsExplicitNulls(t *testing.T) {
	// PATCH /api/apps/{id} validates every field as present-but-nullable, so
	// unset attributes must serialise as null rather than be omitted.
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c, _ := New(Options{BaseURL: server.URL, APIKey: "id.token"})
	if err := c.UpdateApp(context.Background(), "someid", AppRequest{Name: "x", IconURL: "y"}); err != nil {
		t.Fatalf("UpdateApp() error = %v", err)
	}

	for _, field := range []string{"description", "href", "pingUrl"} {
		value, present := gotBody[field]
		if !present {
			t.Errorf("request body omits %q, want an explicit null", field)
			continue
		}
		if value != nil {
			t.Errorf("request body %q = %v, want null", field, value)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
