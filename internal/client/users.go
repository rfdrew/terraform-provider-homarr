package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// UserSummary is the shape returned by GET /api/users.
//
// Name is a pointer because users.name is nullable in Homarr's schema and in
// the output schemas of both user reads; decoding it into a string would turn a
// null into "".
type UserSummary struct {
	ID            string  `json:"id"`
	Name          *string `json:"name"`
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

// UserDetail is the shape returned by GET /api/users/{userId}. Name is nullable
// for the same reason as on UserSummary.
//
// The response also carries headerPreferences, but as the raw JSON text Homarr
// stores, possibly still in a legacy format; UserPreferences reports it parsed.
type UserDetail struct {
	ID            string  `json:"id"`
	Name          *string `json:"name"`
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
// HeaderPreferences is only decoded here. Homarr stores the header layout as
// JSON text, which GET /api/users/{userId} hands back verbatim — possibly in one
// of the two legacy formats — while this endpoint parses it and migrates it to
// the current version first.
type UserPreferences struct {
	UserID            string             `json:"userId"`
	HeaderPreferences *HeaderPreferences `json:"headerPreferences"`
	UserPreferenceFields
}

// HeaderPreferencesVersion is the header layout format Homarr 2.x reads and
// writes, headerPreferencesVersion in packages/validation/src/header-preferences.ts.
// Older formats are migrated on read, so a response always carries this one.
const HeaderPreferencesVersion = 3

// HeaderPreferences is the layout of the header bar above a board: whether it
// is shown, how the search field and the logo are displayed, and which items
// sit in each of its three zones.
//
// Homarr validates the merged header before every write: items must be unique
// across the zones, a zone holds at most 100, and the builtin "user" item must
// be present. A header that passes is stored as sent and reads back unchanged.
// Homarr does re-append "user" when it is missing, but only to legacy formats
// it migrates; the current format rejects such a header outright.
//
// PATCH /api/users/preferences merges a header patch field by field and zone by
// zone, keeping any zone the patch leaves out. Sending all three zones, which
// HeaderZones always does, therefore replaces the layout as a whole.
type HeaderPreferences struct {
	Version       int         `json:"version"`
	Visible       bool        `json:"visible"`
	SearchDisplay string      `json:"searchDisplay"`
	LogoDisplay   string      `json:"logoDisplay"`
	Zones         HeaderZones `json:"zones"`
}

// HeaderZones are the three zones of the header, from left to right.
type HeaderZones struct {
	Left   []HeaderItem `json:"left"`
	Center []HeaderItem `json:"center"`
	Right  []HeaderItem `json:"right"`
}

// MarshalJSON encodes an empty zone as [] rather than null, which Homarr's
// array schema rejects.
func (z HeaderZones) MarshalJSON() ([]byte, error) {
	type zones HeaderZones
	nonNil := func(items []HeaderItem) []HeaderItem {
		if items == nil {
			return []HeaderItem{}
		}
		return items
	}
	return json.Marshal(zones{Left: nonNil(z.Left), Center: nonNil(z.Center), Right: nonNil(z.Right)})
}

// HeaderItem is one entry in a header zone: either a builtin control,
// {"type":"builtin","id":...}, or a shortcut to a board,
// {"type":"board","boardId":...}. Homarr's item schemas are strict, so only
// the field that belongs to the type may be sent; the other is omitted.
type HeaderItem struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	BoardID string `json:"boardId,omitempty"`
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
// Requires Homarr 2.3.0 or newer. An older release answers with a 404, which
// callers take to mean exactly that — but only an admin key gets one there. Those
// releases have no such route, so trpc-to-openapi matches
// GET /api/users/{userId} instead, with "preferences" as the user id; the path
// parameter wins over a userId in the query. An admin is told that no such user
// exists, while any other key is refused a look at another user's details with
// a 403. That refusal is turned into the 404 an admin gets, since GET
// /api/users/preferences itself never answers it: from 2.3.0 on, another user's
// preferences are a 404 too.
func (c *Client) GetUserPreferences(ctx context.Context, userID string) (*UserPreferences, error) {
	path := "/api/users/preferences"
	if userID != "" {
		path += "?userId=" + url.QueryEscape(userID)
	}
	var prefs UserPreferences
	if err := c.do(ctx, "GET", path, nil, &prefs); err != nil {
		var apiErr *Error
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden && apiErr.Message == otherUsersDetailsRefused {
			return nil, &Error{
				StatusCode: http.StatusNotFound,
				Code:       "NOT_FOUND",
				Message: "GET /api/users/preferences does not exist on this Homarr, which predates 2.3.0; it " +
					"routed the request to GET /api/users/{userId}, which answered: " + apiErr.Message,
			}
		}
		return nil, err
	}
	return &prefs, nil
}

// otherUsersDetailsRefused is the message of the 403 that GET /api/users/{userId}
// answers when a key that is not an admin's asks for another user.
const otherUsersDetailsRefused = "You are not allowed to view other users details"

// CurrentUserID returns the id of the user the API key belongs to.
//
// Homarr has no "who am I" endpoint, but GET /api/users/preferences without a
// userId resolves to the caller and echoes its id, and any key may call it.
//
// Requires Homarr 2.3.0 or newer; older releases answer 404.
func (c *Client) CurrentUserID(ctx context.Context) (string, error) {
	prefs, err := c.GetUserPreferences(ctx, "")
	if err != nil {
		return "", err
	}
	if prefs.UserID == "" {
		return "", fmt.Errorf("GET /api/users/preferences did not report the id of the API key's user")
	}
	return prefs.UserID, nil
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

// ListUsers returns every user. Requires an admin API key; ListUsersWithAnyKey
// does not.
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
//
// A failed POST can leave the account behind as well. Homarr inserts the user
// row first and the group memberships afterwards, outside a transaction, so a
// duplicate or unknown group id fails the request with a 500 once the account
// already exists — and that account then holds the name, so a retry answers
// 409. For any failure but a 409 the users are listed again, and if exactly one
// new user of that name appeared, its id is returned together with the error.
// A caller must therefore treat a non-empty id as an existing account even when
// the error is not nil.
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
		if IsConflict(err) {
			return "", err
		}
		return c.recoverFailedCreate(ctx, req.Username, existing, err)
	}

	after, err := c.ListUsers(ctx)
	if err != nil {
		return "", fmt.Errorf("user %q was created but its id could not be recovered: %w", req.Username, err)
	}
	candidates := newUsersNamed(after, existing, req.Username)

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

// recoverFailedCreate looks for an account that a failed POST /api/users
// created anyway, and returns its id along with postErr when there is exactly
// one. Otherwise it returns postErr alone: most failures, a 400 for instance,
// create nothing, and several candidates cannot be told apart safely.
func (c *Client) recoverFailedCreate(ctx context.Context, username string, existing map[string]bool, postErr error) (string, error) {
	after, err := c.ListUsers(ctx)
	if err != nil {
		return "", postErr
	}
	candidates := newUsersNamed(after, existing, username)
	if len(candidates) != 1 {
		return "", postErr
	}
	return candidates[0], fmt.Errorf("user %q was created (id %s) but Homarr then failed the request, most likely "+
		"while adding the user to its groups — a duplicate or unknown group id fails it after the account exists: %w",
		username, candidates[0], postErr)
}

// newUsersNamed returns the ids of the users in after that are not in existing
// and carry the name Homarr made of username.
func newUsersNamed(after []UserSummary, existing map[string]bool, username string) []string {
	var candidates []string
	for _, u := range after {
		if !existing[u.ID] && u.Name != nil && sameUsername(*u.Name, username) {
			candidates = append(candidates, u.ID)
		}
	}
	return candidates
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

// UpdateUserProfile renames a credentials user and sets or changes its email,
// through PUT /api/users/profile. The endpoint returns no body.
//
// name is required even when only the email changes. A nil email is omitted and
// leaves the stored one alone, and so would an empty string: Homarr writes the
// email only when the new value is non-empty and differs, so an email can be
// changed but never cleared. Changing it also resets emailVerified.
//
// An admin may edit any user and anyone may edit themselves, but only
// credentials accounts: LDAP and OIDC users answer 403. A name another
// credentials user already has answers 409.
func (c *Client) UpdateUserProfile(ctx context.Context, id, name string, email *string) error {
	body := struct {
		ID    string  `json:"id"`
		Name  string  `json:"name"`
		Email *string `json:"email,omitempty"`
	}{
		ID:    id,
		Name:  name,
		Email: email,
	}
	return c.do(ctx, "PUT", "/api/users/profile", body, nil)
}

// SetUserProfileImage sets a user's profile image through
// PUT /api/users/profileImage, or removes it when image is nil. The endpoint
// returns no body.
//
// Homarr takes only a base64 data URL of a PNG, JPEG, GIF or WebP picture, at
// most 350,000 characters long, and stores it verbatim, so GET /api/users/{id}
// reports it back unchanged. An admin may set any user's image and anyone their
// own; unlike a rename, this works for LDAP and OIDC accounts too.
func (c *Client) SetUserProfileImage(ctx context.Context, userID string, image *string) error {
	body := struct {
		UserID string  `json:"userId"`
		Image  *string `json:"image"`
	}{
		UserID: userID,
		Image:  image,
	}
	return c.do(ctx, "PUT", "/api/users/profileImage", body, nil)
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

// SetUserSearchPreferences sets a user's default search engine, whether search
// results open in a new tab, and whether DuckDuckGo bangs are enabled, through
// PATCH /api/users/search-preferences, which every Homarr 2.x release has. All
// three fields are required by the schema and written together; the search
// engine may be null to clear it.
func (c *Client) SetUserSearchPreferences(ctx context.Context, userID string, defaultSearchEngineID *string, openInNewTab, ddgBangsEnabled bool) error {
	body := struct {
		UserID                string  `json:"userId"`
		DefaultSearchEngineID *string `json:"defaultSearchEngineId"`
		OpenInNewTab          bool    `json:"openInNewTab"`
		DdgBangsEnabled       bool    `json:"ddgBangsEnabled"`
	}{
		UserID:                userID,
		DefaultSearchEngineID: defaultSearchEngineID,
		OpenInNewTab:          openInNewTab,
		DdgBangsEnabled:       ddgBangsEnabled,
	}
	return c.do(ctx, "PATCH", "/api/users/search-preferences", body, nil)
}

// DeleteUser removes a user.
func (c *Client) DeleteUser(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/users/"+url.PathEscape(id), nil, nil)
}
