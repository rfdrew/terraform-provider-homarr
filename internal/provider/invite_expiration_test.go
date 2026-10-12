package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// validateInviteExpiration runs the validators the schema declares on
// expiration_date, as Terraform does at plan time, so the test also fails if
// the validator is ever unhooked from the attribute.
func validateInviteExpiration(t *testing.T, value string) diag.Diagnostics {
	t.Helper()
	attr, ok := resourceSchema(t, &inviteResource{}).Attributes["expiration_date"].(schema.StringAttribute)
	if !ok {
		t.Fatal("expiration_date is not a string attribute")
	}
	var diags diag.Diagnostics
	for _, v := range attr.Validators {
		var resp validator.StringResponse
		v.ValidateString(context.Background(), validator.StringRequest{
			Path:        path.Root("expiration_date"),
			ConfigValue: types.StringValue(value),
		}, &resp)
		diags.Append(resp.Diagnostics...)
	}
	return diags
}

func TestInviteExpirationAcceptsWholeSecondsInAnyOffset(t *testing.T) {
	for _, value := range []string{
		"2027-01-01T00:00:00Z",
		"2027-01-01T02:00:00+02:00",
		"2026-12-31T19:00:00-05:00",
		"2027-01-01T00:00:00-00:00",
		"2028-02-29T23:59:59+23:59",
		// A zero fraction is the same instant as none, so Homarr stores exactly
		// what was planned, and fromAPI, which compares instants, keeps the
		// spelling just as it keeps an offset. It is also how Homarr itself
		// prints dates, so a value copied from its API is not refused.
		"2027-01-01T00:00:00.000Z",
		"2027-01-01T02:00:00.0+02:00",
	} {
		if diags := validateInviteExpiration(t, value); diags.HasError() {
			t.Errorf("%q was rejected: %v", value, diags)
		}
	}
}

func TestInviteExpirationRejectsWhatHomarrWouldRefuseOrRewrite(t *testing.T) {
	for value, why := range map[string]string{
		"2027-01-01T00:00:00.5Z":      "SQLite keeps whole seconds, so it would read back as 00:00:00Z",
		"2027-01-01T00:00:00.001Z":    "SQLite keeps whole seconds, so it would read back as 00:00:00Z",
		"2027-01-01T02:00:00.5+02:00": "SQLite keeps whole seconds, so it would read back as 00:00:00Z",
		"2027-01-01T00:00:00,0Z":      "a comma before the fraction: time.Parse takes it, RFC 3339 does not",
		"2027-01-01T0:00:00Z":         "a one-digit hour: time.Parse takes it, RFC 3339 does not",
		"2027-01-01T00:00:00+24:00":   "an offset beyond 23:59: time.Parse takes it, RFC 3339 does not",
		"2027-01-01T00:00:00+23:60":   "an offset beyond 23:59: time.Parse takes it, RFC 3339 does not",
		"2027-01-01T00:00:00+0200":    "an offset without a colon",
		"2027-01-01t00:00:00Z":        "a lower-case t, which time.Parse refuses",
		"2027-01-01T00:00:00z":        "a lower-case z, which time.Parse refuses",
		"2016-12-31T23:59:60Z":        "a leap second, which time.Parse refuses",
		"2027-02-29T00:00:00Z":        "a day 2027 does not have",
		"2027-01-01T24:00:00Z":        "hour 24",
		"2027-01-01T00:00Z":           "no seconds, which RFC 3339 requires",
		"2027-01-01 00:00:00Z":        "a space instead of the T",
		" 2027-01-01T00:00:00Z":       "surrounding whitespace",
		"2027-01-01":                  "a date without a time",
		"not a date":                  "not a timestamp at all",
		"":                            "empty",
	} {
		if diags := validateInviteExpiration(t, value); !diags.HasError() {
			t.Errorf("%q was accepted despite %s", value, why)
		}
	}

	requireErrorContaining(t, validateInviteExpiration(t, "2027-01-01T00:00:00.5Z"), "whole seconds")
}

// sqliteInviteHomarr fakes Homarr's invite endpoints on its default SQLite
// database: the expiration is stored as whole Unix seconds (drizzle floors the
// milliseconds away) and read back the way JavaScript prints a Date.
func sqliteInviteHomarr(t *testing.T) *fakeHomarr {
	t.Helper()
	var stored time.Time
	fake := newFakeHomarr()
	fake.handle("POST /api/invites", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ExpirationDate string `json:"expirationDate"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		parsed, err := time.Parse(time.RFC3339Nano, body.ExpirationDate)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"message":"Invalid ISO datetime","code":"BAD_REQUEST"}`)
			return
		}
		stored = parsed.Truncate(time.Second)
		_, _ = io.WriteString(w, `{"id":"i1","token":"secret"}`)
	})
	fake.handle("GET /api/invites", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id":             "i1",
			"expirationDate": stored.UTC().Format("2006-01-02T15:04:05.000Z"),
			"creator":        map[string]any{"id": "u1", "name": "admin"},
		}})
	})
	return fake
}

func createInvite(t *testing.T, fake *fakeHomarr, expiration string) (inviteModel, diag.Diagnostics) {
	t.Helper()
	ctx := context.Background()
	r := &inviteResource{client: fake.client(t)}
	s := resourceSchema(t, r)
	plan := planOf(t, s, &inviteModel{
		ID:             types.StringUnknown(),
		ExpirationDate: types.StringValue(expiration),
		Token:          types.StringUnknown(),
		CreatorID:      types.StringUnknown(),
		CreatorName:    types.StringUnknown(),
	})

	resp := resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, &resp)

	var state inviteModel
	if !resp.Diagnostics.HasError() {
		requireNoErrors(t, resp.State.Get(ctx, &state))
	}
	return state, resp.Diagnostics
}

// Terraform requires the applied expiration_date to equal the planned one; a
// whole-second value meets that whichever way it is spelled.
func TestInviteCreateKeepsAWholeSecondExpirationAsWritten(t *testing.T) {
	for _, expiration := range []string{
		"2027-01-01T00:00:00Z",
		"2027-01-01T02:00:00+02:00",
		"2027-01-01T00:00:00.000Z",
	} {
		t.Run(expiration, func(t *testing.T) {
			fake := sqliteInviteHomarr(t)
			state, diags := createInvite(t, fake, expiration)
			requireNoErrors(t, diags)

			if got := state.ExpirationDate.ValueString(); got != expiration {
				t.Fatalf("expiration_date = %q after apply, planned %q: an inconsistent result", got, expiration)
			}
			if sent := fake.body(t, "POST /api/invites")["expirationDate"]; sent != "2027-01-01T00:00:00Z" {
				t.Fatalf("sent expirationDate %v, want the same instant in UTC", sent)
			}
			if state.ID.ValueString() != "i1" || state.Token.ValueString() != "secret" {
				t.Fatalf("id = %v, token = %v; want the values Homarr returned", state.ID, state.Token)
			}
		})
	}
}

// Create checks the value again, so a sub-second expiration never reaches
// Homarr, which would store a different instant than the one planned.
func TestInviteCreateRefusesASubSecondExpiration(t *testing.T) {
	fake := sqliteInviteHomarr(t)
	_, diags := createInvite(t, fake, "2027-01-01T00:00:00.5Z")

	requireErrorContaining(t, diags, "whole seconds")
	if fake.called("POST /api/invites") {
		t.Fatal("sent a sub-second expiration that Homarr would store as a different instant")
	}
}
