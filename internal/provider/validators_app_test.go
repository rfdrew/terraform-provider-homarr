package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestAppHrefValidatorRefusesJavaScriptURIsLikeHomarr(t *testing.T) {
	for value, want := range map[string]bool{
		"https://grafana.example.com": true,
		"mailto:ops@example.com":      true,
		"vscode://file/etc/hosts":     true,
		"/relative/path":              true,
		"javascript":                  true, // no colon, so not the scheme
		"xjavascript:alert(1)":        true, // anchored at the start
		"javascript:alert(1)":         false,
		"JavaScript:alert(1)":         false,
		"JAVASCRIPT:void(0)":          false,
		// Homarr trims before matching, so surrounding whitespace does not hide
		// the scheme; trimmedValidator reports the whitespace on its own.
		" javascript:alert(1)": false,
		// Without the u flag, /i ignores case for ASCII letters only: a long s
		// is not an s to Homarr, though strings.EqualFold would treat it as one.
		"javaſcript:alert(1)": true,
	} {
		if got := validate(appHrefValidator(), value); got != want {
			t.Errorf("appHrefValidator(%q) passes = %v, want %v", value, got, want)
		}
	}
}

func TestAppPingURLValidatorRequiresWhatHomarrRequires(t *testing.T) {
	for value, want := range map[string]bool{
		"https://grafana.example.com/api/health": true,
		"http://192.168.1.10:8080":               true,
		"http://[::1]:8080/health":               true,
		"https://user:secret@example.com/ping":   true,
		"http://localhost":                       true,
		// The WHATWG parser skips extra slashes before an http(s) host.
		"http:///example.com": true,
		// Homarr's /^https?:\/\// is case-sensitive.
		"HTTPS://example.com": false,
		"Http://example.com":  false,
		// Other schemes, and http without the two slashes.
		"ftp://example.com":    false,
		"https:example.com":    false,
		"http:/example.com":    false,
		"//example.com":        false,
		"example.com":          false,
		"javascript:alert(1)":  false,
		"https://":             false,
		"http://:8080/health":  false,
		"http://user@/health":  false,
		"http://?query":        false,
		"http://exa mple.com/": false,
		// "" is Homarr's way to clear the ping URL; other validators refuse it.
		"": true,
	} {
		if got := validate(appPingURLValidator(), value); got != want {
			t.Errorf("appPingURLValidator(%q) passes = %v, want %v", value, got, want)
		}
	}
}

// The validators must be hooked up to the homarr_app schema, and report the
// value at plan time rather than leave it to a 400 at apply.
func TestAppSchemaRejectsJavaScriptHrefAndNonHTTPPingURL(t *testing.T) {
	requireErrorContaining(t, validateAppAttribute(t, "href", types.StringValue("javascript:alert(1)")),
		"must not be a javascript: URI")
	requireErrorContaining(t, validateAppAttribute(t, "ping_url", types.StringValue("HTTPS://example.com")),
		"must be an http:// or https:// URL")
	requireErrorContaining(t, validateAppAttribute(t, "ping_url", types.StringValue("ftp://example.com")),
		"must be an http:// or https:// URL")
}

// Whitespace stays trimmedValidator's to report: the new checks look at the
// value Homarr would see after trimming, so they add no second error for it.
func TestAppURLValidatorsLeaveWhitespaceToTrimmedValidator(t *testing.T) {
	for attr, value := range map[string]string{
		"href":     " https://grafana.example.com",
		"ping_url": "https://grafana.example.com/api/health\n",
	} {
		diags := validateAppAttribute(t, attr, types.StringValue(value))
		if diags.ErrorsCount() != 1 {
			t.Errorf("%s = %q: want exactly the whitespace error, got %v", attr, value, diags)
		}
	}
}
