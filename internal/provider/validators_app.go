package provider

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// Homarr's appManageSchema trims href and ping_url before it checks them, and
// these validators check the trimmed value the same way, so that whitespace is
// left to trimmedValidator to report. A value that trims to "" is left to the
// other validators as well: Homarr would store it as null.

// appHrefValidator refuses a javascript: URI, the one href appHrefSchema
// rejects. Its /^javascript:/i has no u flag, so it ignores case for ASCII
// letters only: "JavaScript:" is refused, but a look-alike such as the long s
// in "javaſcript:" is not, which strings.EqualFold would get wrong.
func appHrefValidator() validator.String {
	return stringCheck{
		description: "must not be a javascript: URI, which Homarr refuses",
		check: func(s string) bool {
			return !hasPrefixFoldASCII(client.JSTrim(s), "javascript:")
		},
	}
}

// appPingURLValidator requires what appManageSchema requires of pingUrl: a URL
// that matches /^https?:\/\// — case-sensitively, so HTTPS:// is refused — and
// that the WHATWG URL parser behind zod's .url() accepts.
//
// The parse check is deliberately lenient. Go's net/url and the WHATWG parser
// disagree on many edge cases, and Homarr remains the authority, so only a URL
// that is certain to fail is refused: one with no host, such as "https://" or
// "http://:8080/health", or with a space in the host.
func appPingURLValidator() validator.String {
	return stringCheck{
		description: "must be an http:// or https:// URL with a host, the scheme written in lower case, as " +
			"Homarr requires",
		check: func(s string) bool {
			trimmed := client.JSTrim(s)
			return trimmed == "" || isHomarrPingURL(trimmed)
		},
	}
}

// hasPrefixFoldASCII is strings.HasPrefix ignoring the case of ASCII letters
// only. prefix must be ASCII.
func hasPrefixFoldASCII(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if lowerASCII(s[i]) != lowerASCII(prefix[i]) {
			return false
		}
	}
	return true
}

func lowerASCII(b byte) byte {
	if 'A' <= b && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// isHomarrPingURL reports whether s could be a URL Homarr accepts as pingUrl.
func isHomarrPingURL(s string) bool {
	rest, ok := strings.CutPrefix(s, "http://")
	if !ok {
		if rest, ok = strings.CutPrefix(s, "https://"); !ok {
			return false
		}
	}

	// The WHATWG parser drops tabs and newlines anywhere in the input, and for
	// http and https it skips any further slashes or backslashes before the
	// host, so "http:///example.com" names the host example.com.
	rest = strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(rest)
	rest = strings.TrimLeft(rest, `/\`)

	host := rest
	if i := strings.IndexAny(host, `/\?#`); i >= 0 {
		host = host[:i]
	}
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	if !strings.HasPrefix(host, "[") {
		// Not an IPv6 literal, so a colon starts the port.
		if i := strings.Index(host, ":"); i >= 0 {
			host = host[:i]
		}
	}
	return host != "" && !strings.Contains(host, " ")
}
