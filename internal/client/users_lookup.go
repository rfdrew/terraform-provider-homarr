package client

import (
	"context"
	"net/url"
)

// AuthProviders are the auth providers a Homarr user can sign in through,
// supportedAuthProviders in packages/definitions/src/auth.ts.
var AuthProviders = []string{"credentials", "oidc", "ldap"}

// ListSelectableUsers returns every user through GET /api/users/selectable, or
// only those signing in through provider when it is not empty.
//
// Unlike GET /api/users, which needs an admin, this endpoint is open to any
// authenticated API key: Homarr uses it for the user pickers of its access
// forms. It reports the same fields except emailVerified, and filters by auth
// provider on the server, which neither list can otherwise tell apart. The
// filter is an array in Homarr's schema; trpc-to-openapi takes a single
// `providers` query parameter as a one-element array.
func (c *Client) ListSelectableUsers(ctx context.Context, provider string) ([]UserSummary, error) {
	path := "/api/users/selectable"
	if provider != "" {
		path += "?providers=" + url.QueryEscape(provider)
	}
	var users []UserSummary
	if err := c.do(ctx, "GET", path, nil, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// ListUsersWithAnyKey returns every user, like ListUsers, but for any API key.
//
// GET /api/users is reserved for admins, so another key's 403 falls back to
// GET /api/users/selectable, which lists the same users with the same fields
// except emailVerified. As with ListApps, the fallback is best-effort: if it
// fails too, the original 403 is reported.
func (c *Client) ListUsersWithAnyKey(ctx context.Context) ([]UserSummary, error) {
	users, err := c.ListUsers(ctx)
	if err == nil || !IsForbidden(err) {
		return users, err
	}
	selectable, fallbackErr := c.ListSelectableUsers(ctx, "")
	if fallbackErr != nil {
		return nil, err
	}
	return selectable, nil
}

// ProviderUser is a user as GET /api/users/selectable lists it, along with the
// auth provider it was listed under.
type ProviderUser struct {
	UserSummary
	Provider string
}

// ListUsersByProvider lists the users of every auth provider in AuthProviders,
// one GET /api/users/selectable per provider. Neither list of users reports the
// provider, so this is the only way to learn it without reading every user,
// and it works with any API key.
//
// A user of a provider that a newer Homarr might add is not listed at all.
func (c *Client) ListUsersByProvider(ctx context.Context) ([]ProviderUser, error) {
	var all []ProviderUser
	for _, provider := range AuthProviders {
		users, err := c.ListSelectableUsers(ctx, provider)
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			all = append(all, ProviderUser{UserSummary: u, Provider: provider})
		}
	}
	return all, nil
}

// FindUsersByName returns the users whose name matches name the way Homarr
// matches a sign-in, optionally only those of one auth provider.
//
// Homarr trims and lowercases a username before storing it, so the comparison
// trims name and ignores case. Names are unique only per auth provider, so
// without a provider several users can match; and releases before the
// lowercasing stored names as typed, so even within a provider "Alice" and
// "alice" may both exist until Homarr's fix-usernames command has run. Callers
// decide what more than one match means.
func (c *Client) FindUsersByName(ctx context.Context, name, provider string) ([]UserSummary, error) {
	users, err := c.ListSelectableUsers(ctx, provider)
	if err != nil {
		return nil, err
	}
	var matches []UserSummary
	for _, u := range users {
		if u.Name != nil && sameUsername(*u.Name, name) {
			matches = append(matches, u)
		}
	}
	return matches, nil
}
