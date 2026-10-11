package client

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// UserSummary is the shape returned by GET /api/users.
type UserSummary struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Email         *string `json:"email"`
	EmailVerified *string `json:"emailVerified"`
	Image         *string `json:"image"`
}

// UserPreferenceFields are the preferences that both GET /api/users/{userId}
// and /api/users/preferences report, under the same names.
type UserPreferenceFields struct {
	ColorScheme               string  `json:"colorScheme"`
	ByteUnitSystem            string  `json:"byteUnitSystem"`
	HomeBoardID               *string `json:"homeBoardId"`
	MobileHomeBoardID         *string `json:"mobileHomeBoardId"`
	FirstDayOfWeek            int64   `json:"firstDayOfWeek"`
	PingIconsEnabled          bool    `json:"pingIconsEnabled"`
	EnableRightClickOnWidgets bool    `json:"enableRightClickOnWidgets"`
	DefaultSearchEngineID     *string `json:"defaultSearchEngineId"`
	OpenSearchInNewTab        bool    `json:"openSearchInNewTab"`
	DdgBangs                  bool    `json:"ddgBangs"`
}

// UserDetail is the shape returned by GET /api/users/{userId}.
type UserDetail struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Email         *string `json:"email"`
	EmailVerified *string `json:"emailVerified"`
	Image         *string `json:"image"`
	Provider      string  `json:"provider"`
	UserPreferenceFields
}

// UserCreateRequest is the body for POST /api/users.
//
// Email is omitted rather than sent as null when unset: optionalEmailSchema
// accepts a valid address, an empty string, or absence — an explicit null is
// rejected with a 400.
type UserCreateRequest struct {
	Username        string   `json:"username"`
	Password        string   `json:"password"`
	ConfirmPassword string   `json:"confirmPassword"`
	Email           *string  `json:"email,omitempty"`
	GroupIDs        []string `json:"groupIds"`
}

// UserPreferences is the shape of GET and PATCH /api/users/preferences, added
// in Homarr 2.3.0. Both verbs return it in full.
//
// The response also carries headerPreferences, which is deliberately not
// decoded: it is a versioned, deeply nested object that Homarr rewrites on
// write (it re-appends required items), so it cannot round-trip through a flat
// Terraform attribute and the provider does not manage it.
type UserPreferences struct {
	UserID string `json:"userId"`
	UserPreferenceFields
}

// UserPreferencesPatch is a partial update to a user's preferences.
//
// It is a map rather than a struct on purpose: homeBoardId, mobileHomeBoardId
// and defaultSearchEngineId are nullable references where an explicit null
// clears the value and an omitted key leaves it alone. A struct with
// `omitempty` cannot express that difference, and one without it would clear
// every field the caller did not set.
type UserPreferencesPatch map[string]any

// GetUserPreferences reads a user's preferences. An empty userID means the user
// behind the API key; naming another user requires admin.
//
// Requires Homarr 2.3.0 or newer.
func (c *Client) GetUserPreferences(ctx context.Context, userID string) (*UserPreferences, error) {
	path := "/api/users/preferences"
	if userID != "" {
		path += "?userId=" + url.QueryEscape(userID)
	}
	var prefs UserPreferences
	if err := c.do(ctx, "GET", path, nil, &prefs); err != nil {
		return nil, err
	}
	return &prefs, nil
}

// UpdateUserPreferences applies a partial patch and returns the preferences as
// stored afterwards. An empty userID targets the user behind the API key.
//
// Homarr rejects an empty patch with a 400 ("Supply at least one preference"),
// so callers must skip the call when nothing changed. Every supplied value is
// validated before a single write, making the call atomic.
//
// Requires Homarr 2.3.0 or newer.
func (c *Client) UpdateUserPreferences(ctx context.Context, userID string, patch UserPreferencesPatch) (*UserPreferences, error) {
	if len(patch) == 0 {
		return nil, fmt.Errorf("refusing to send an empty preferences patch: Homarr requires at least one field")
	}
	body := make(map[string]any, len(patch)+1)
	for k, v := range patch {
		body[k] = v
	}
	if userID != "" {
		body["userId"] = userID
	}

	var prefs UserPreferences
	if err := c.do(ctx, "PATCH", "/api/users/preferences", body, &prefs); err != nil {
		return nil, err
	}
	return &prefs, nil
}

// ListUsers returns every user. Requires an admin API key.
func (c *Client) ListUsers(ctx context.Context) ([]UserSummary, error) {
	var users []UserSummary
	if err := c.do(ctx, "GET", "/api/users", nil, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// GetUser fetches a single user by id.
func (c *Client) GetUser(ctx context.Context, id string) (*UserDetail, error) {
	var user UserDetail
	if err := c.do(ctx, "GET", "/api/users/"+url.PathEscape(id), nil, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// CreateUser creates a credentials user and returns its id.
//
// POST /api/users declares `.output(z.void())` and returns an empty body, so
// the id has to be recovered from GET /api/users afterwards. The name alone
// does not identify the new account: Homarr keeps usernames unique only per
// auth provider, so an LDAP or OIDC user may already have the same name, and
// GET /api/users does not say which provider a user belongs to. Picking that
// user would hand the caller someone else's account to manage — and, on
// destroy, to delete. The users are therefore listed before the POST too, and
// only a user that was not there before qualifies.
//
// Anything other than exactly one such user is an error, even though the
// account now exists: guessing between several is the very mistake this
// avoids, and with none there is no id that is safe to return.
func (c *Client) CreateUser(ctx context.Context, req UserCreateRequest) (string, error) {
	before, err := c.ListUsers(ctx)
	if err != nil {
		return "", fmt.Errorf("listing users before creating %q, to tell the new user apart from existing ones: %w", req.Username, err)
	}
	existing := make(map[string]bool, len(before))
	for _, u := range before {
		existing[u.ID] = true
	}

	if err := c.do(ctx, "POST", "/api/users", req, nil); err != nil {
		return "", err
	}

	after, err := c.ListUsers(ctx)
	if err != nil {
		return "", fmt.Errorf("user %q was created but its id could not be recovered: %w", req.Username, err)
	}
	var candidates []string
	for _, u := range after {
		if !existing[u.ID] && sameUsername(u.Name, req.Username) {
			candidates = append(candidates, u.ID)
		}
	}

	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return "", fmt.Errorf("user %q was created but could not be found afterwards: "+
			"GET /api/users lists no user of that name that was not there before the create", req.Username)
	default:
		return "", fmt.Errorf("user %q was created but its id is ambiguous: %d users of that name appeared in "+
			"GET /api/users during the create (ids %s), most likely because another account was added at the "+
			"same time; check them in Homarr and remove or adopt the right one by hand",
			req.Username, len(candidates), strings.Join(candidates, ", "))
	}
}

// sameUsername reports whether stored, a name as GET /api/users returns it, is
// what Homarr made of configured when creating the user.
//
// usernameSchema runs z.string().trim().toLowerCase(), so configured is trimmed
// the way JavaScript trims and then compared case-insensitively. Folding case
// instead of lowercasing with strings.ToLower is deliberate: JavaScript's
// toLowerCase applies full Unicode mappings that Go's does not (a final capital
// sigma becomes "ς", not "σ"), and a spurious mismatch would leave the account
// just created unaccounted for. The looser comparison cannot pick the wrong
// user, because CreateUser only considers users that did not exist before.
func sameUsername(stored, configured string) bool {
	return strings.EqualFold(stored, JSTrim(configured))
}

// ChangeUserPassword sets a new password for a user.
//
// previousPassword must be a non-empty string even for an admin acting on
// another account: the field is validated as min(1) by the schema, but its
// value is only actually verified when the caller is changing their own
// password.
func (c *Client) ChangeUserPassword(ctx context.Context, userID, previousPassword, newPassword string) error {
	if previousPassword == "" {
		previousPassword = "unused-by-admin"
	}
	body := struct {
		PreviousPassword string `json:"previousPassword"`
		Password         string `json:"password"`
		ConfirmPassword  string `json:"confirmPassword"`
	}{
		PreviousPassword: previousPassword,
		Password:         newPassword,
		ConfirmPassword:  newPassword,
	}
	return c.do(ctx, "PATCH", "/api/users/"+url.PathEscape(userID)+"/changePassword", body, nil)
}

// SetUserHomeBoards sets a user's desktop and mobile home boards. Both fields
// are required by the schema and may be null.
func (c *Client) SetUserHomeBoards(ctx context.Context, userID string, homeBoardID, mobileHomeBoardID *string) error {
	body := struct {
		UserID            string  `json:"userId"`
		HomeBoardID       *string `json:"homeBoardId"`
		MobileHomeBoardID *string `json:"mobileHomeBoardId"`
	}{
		UserID:            userID,
		HomeBoardID:       homeBoardID,
		MobileHomeBoardID: mobileHomeBoardID,
	}
	return c.do(ctx, "PATCH", "/api/users/changeHome", body, nil)
}

// DeleteUser removes a user.
func (c *Client) DeleteUser(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/users/"+url.PathEscape(id), nil, nil)
}
