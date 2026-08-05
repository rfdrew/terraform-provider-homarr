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

// UserDetail is the shape returned by GET /api/users/{userId}.
type UserDetail struct {
	ID                        string  `json:"id"`
	Name                      string  `json:"name"`
	Email                     *string `json:"email"`
	EmailVerified             *string `json:"emailVerified"`
	Image                     *string `json:"image"`
	Provider                  string  `json:"provider"`
	HomeBoardID               *string `json:"homeBoardId"`
	MobileHomeBoardID         *string `json:"mobileHomeBoardId"`
	FirstDayOfWeek            int64   `json:"firstDayOfWeek"`
	PingIconsEnabled          bool    `json:"pingIconsEnabled"`
	EnableRightClickOnWidgets bool    `json:"enableRightClickOnWidgets"`
	DefaultSearchEngineID     *string `json:"defaultSearchEngineId"`
	OpenSearchInNewTab        bool    `json:"openSearchInNewTab"`
	DdgBangs                  bool    `json:"ddgBangs"`
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

// FindUserByUsername locates a user by name, case-insensitively.
//
// Homarr lowercases usernames on the way in (usernameSchema applies
// .toLowerCase()), so the stored name may differ in case from what was
// configured. Absence yields a synthetic 404.
func (c *Client) FindUserByUsername(ctx context.Context, username string) (*UserSummary, error) {
	users, err := c.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	target := strings.ToLower(strings.TrimSpace(username))
	for i := range users {
		if strings.ToLower(users[i].Name) == target {
			return &users[i], nil
		}
	}
	return nil, &Error{
		StatusCode: 404,
		Code:       "NOT_FOUND",
		Message:    fmt.Sprintf("user named %q is not present in GET /api/users", username),
	}
}

// CreateUser creates a user and returns its id.
//
// POST /api/users declares `.output(z.void())` and returns an empty body, so
// the id has to be recovered by looking the username up afterwards.
func (c *Client) CreateUser(ctx context.Context, req UserCreateRequest) (string, error) {
	if err := c.do(ctx, "POST", "/api/users", req, nil); err != nil {
		return "", err
	}
	created, err := c.FindUserByUsername(ctx, req.Username)
	if err != nil {
		return "", fmt.Errorf("user %q was created but could not be found afterwards: %w", req.Username, err)
	}
	return created.ID, nil
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
