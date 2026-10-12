package client

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

const forbiddenApps = `{"message":"Permission denied for /api/apps","code":"FORBIDDEN"}`

func TestListAppsReturnsTheFullListWhenAllowed(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/apps", http.StatusOK, `[{"id":"a1","name":"Sonarr","description":null,"iconUrl":"https://example.com/s.png","href":"http://sonarr.lan","pingUrl":null}]`)

	apps, err := f.client(t).ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	if len(apps) != 1 || apps[0].ID != "a1" {
		t.Errorf("ListApps() = %+v, want the one app from GET /api/apps", apps)
	}
	if f.called("GET /api/apps/selectable") {
		t.Error("GET /api/apps/selectable was called although GET /api/apps succeeded")
	}
}

// Homarr 2.0.0 restricted GET /api/apps to app-modify-all; a key without it
// still gets the same fields from /api/apps/selectable.
func TestListAppsFallsBackToSelectableOnForbidden(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/apps", http.StatusForbidden, forbiddenApps)
	f.reply("GET /api/apps/selectable", http.StatusOK, `[`+
		`{"id":"a1","name":"Sonarr","description":null,"iconUrl":"https://example.com/s.png","href":"http://sonarr.lan","pingUrl":null},`+
		`{"id":"a2","name":"Radarr","description":"Movies","iconUrl":"https://example.com/r.png","href":null,"pingUrl":"http://radarr.lan"}`+
		`]`)

	apps, err := f.client(t).ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	if len(apps) != 2 || apps[0].ID != "a1" || apps[1].ID != "a2" {
		t.Fatalf("ListApps() = %+v, want a1 and a2 from GET /api/apps/selectable", apps)
	}
	if apps[1].Description == nil || *apps[1].Description != "Movies" || apps[1].PingURL == nil {
		t.Errorf("ListApps()[1] = %+v, want the selectable fields decoded", apps[1])
	}
}

// When the fallback fails too, the 403 from GET /api/apps is what the caller
// can act on (grant the permission), not whatever the fallback ran into.
func TestListAppsReportsTheOriginalForbiddenWhenTheFallbackFails(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"server error", http.StatusInternalServerError, `{"message":"selectable broke","code":"INTERNAL_SERVER_ERROR"}`},
		{"also forbidden", http.StatusForbidden, `{"message":"Permission denied for /api/apps/selectable","code":"FORBIDDEN"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHomarr()
			f.reply("GET /api/apps", http.StatusForbidden, forbiddenApps)
			f.reply("GET /api/apps/selectable", tc.status, tc.body)

			apps, err := f.client(t).ListApps(context.Background())
			if err == nil {
				t.Fatalf("ListApps() = %+v, want an error", apps)
			}
			if !f.called("GET /api/apps/selectable") {
				t.Fatal("GET /api/apps/selectable was not tried")
			}
			if !IsForbidden(err) {
				t.Errorf("ListApps() error = %v, want a 403", err)
			}
			var apiErr *Error
			if !errors.As(err, &apiErr) || apiErr.Body != forbiddenApps {
				t.Errorf("ListApps() error = %v, want the error from GET /api/apps", err)
			}
		})
	}
}

func TestListAppsDoesNotFallBackOnOtherErrors(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/apps", http.StatusInternalServerError, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)
	f.reply("GET /api/apps/selectable", http.StatusOK, `[]`)

	_, err := f.client(t).ListApps(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("ListApps() error = %v, want the 500 from GET /api/apps", err)
	}
	if f.called("GET /api/apps/selectable") {
		t.Error("GET /api/apps/selectable was called for an error other than 403")
	}
}
