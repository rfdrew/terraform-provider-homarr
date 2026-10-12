package client

import "testing"

func TestJSTrimMatchesJavaScript(t *testing.T) {
	for in, want := range map[string]string{
		"  padded  ":          "padded",
		"\t\n\v\f\r x \r\n":   "x",
		"\u00a0\u3000x\u2028": "x",
		"\ufeffx\ufeff":       "x",             // trimmed by JavaScript, not by strings.TrimSpace
		"\u0085x\u0085":       "\u0085x\u0085", // trimmed by strings.TrimSpace, not by JavaScript
		"inner  space":        "inner  space",
	} {
		if got := JSTrim(in); got != want {
			t.Errorf("JSTrim(%q) = %q, want %q", in, got, want)
		}
	}
}
