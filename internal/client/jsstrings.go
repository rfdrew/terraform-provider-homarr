package client

import (
	"strings"
	"unicode"
)

// Homarr normalises some inputs in JavaScript before storing them, so code that
// predicts what Homarr stores — the provider's validators, and CreateUser
// matching a new user's name — has to trim the way JavaScript does.

// JSTrim is String.prototype.trim, which zod's .trim() applies.
func JSTrim(s string) string {
	return strings.TrimFunc(s, isJSWhitespace)
}

// isJSWhitespace reports whether String.prototype.trim removes r: the
// ECMAScript WhiteSpace and LineTerminator code points. It differs from
// unicode.IsSpace, which also counts U+0085 and does not count U+FEFF.
func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', '\u00a0', '\u2028', '\u2029', '\ufeff':
		return true
	}
	return unicode.Is(unicode.Zs, r)
}
