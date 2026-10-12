package provider

import (
	"context"
	"fmt"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// homarrRelease is the Homarr release that introduced an endpoint.
//
// Homarr's REST handler answers 404 for a route it does not have, so on an
// older Homarr a newer endpoint looks exactly like a missing object. A 404
// from such an endpoint is therefore checked against the version Homarr
// reports, so the user is told to upgrade instead of being told that
// something does not exist.
type homarrRelease struct {
	major, minor, patch int
}

func (r homarrRelease) String() string {
	return fmt.Sprintf("%d.%d.%d", r.major, r.minor, r.patch)
}

// homarrPredates reports whether the connected Homarr is older than release,
// together with the version it reports. known is false when that cannot be
// established — GET /api/info failing, or a version that does not start with a
// major.minor.patch release — and version is "" when Homarr did not report one.
func homarrPredates(ctx context.Context, c *client.Client, release homarrRelease) (older, known bool, version string) {
	info, err := c.GetInfo(ctx)
	if err != nil {
		return false, false, ""
	}
	atLeast, known := info.AtLeast(release.major, release.minor, release.patch)
	return known && !atLeast, known, info.Version
}

// requiresHomarrSummary is the diagnostic summary for an endpoint the
// connected Homarr is too old to have.
func requiresHomarrSummary(release homarrRelease) string {
	return "Requires Homarr " + release.String() + " or newer"
}

// requiresHomarrDetail explains which endpoint is missing and why.
func requiresHomarrDetail(subject, endpoint string, release homarrRelease, version string) string {
	reported := "This Homarr did not report its version"
	if version != "" {
		reported = "This Homarr reports version " + version
	}
	return fmt.Sprintf("%s uses %s, which Homarr added in %s. %s and answered 404 for it, so the endpoint "+
		"does not exist there. Upgrade Homarr to %s or newer to use %s.",
		subject, endpoint, release, reported, release, subject)
}
