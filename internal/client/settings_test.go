package client

import "testing"

func TestInfoAtLeastComparesReleases(t *testing.T) {
	for _, tc := range []struct {
		version         string
		atLeast, known  bool
		major, minor, p int
	}{
		{"2.4.1", true, true, 2, 2, 0},
		{"2.2.0", true, true, 2, 2, 0},
		{"2.1.9", false, true, 2, 2, 0},
		{"2.10.0", true, true, 2, 9, 0},
		{"1.73.0", false, true, 2, 1, 0},
		{"3.0.0", true, true, 2, 4, 1},
		// A pre-release suffix is ignored rather than ordered.
		{"2.2.0-beta.1", true, true, 2, 2, 0},
		{"v2.0.4", false, true, 2, 1, 0},
		{"dev", false, false, 2, 1, 0},
		{"", false, false, 2, 1, 0},
	} {
		atLeast, known := (&Info{Version: tc.version}).AtLeast(tc.major, tc.minor, tc.p)
		if atLeast != tc.atLeast || known != tc.known {
			t.Errorf("Info{%q}.AtLeast(%d, %d, %d) = %v, %v; want %v, %v",
				tc.version, tc.major, tc.minor, tc.p, atLeast, known, tc.atLeast, tc.known)
		}
	}
}
