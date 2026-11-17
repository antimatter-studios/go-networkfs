package api

import "testing"

// SameRevision is RFC 9110's weak comparison for entity tags (section 8.8.3.2):
// two tags whose opaque values match name the same state, whether or not
// either is marked weak. Apache mod_dav reports W/"x" for a file changed in
// the last second and "x" for the same state once that second has passed
// (#94), so equality of the strings would report a change that never
// happened.
func TestSameRevision(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{`"7-66055e976a900"`, `"7-66055e976a900"`, true},
		{`W/"7-66055e976a900"`, `"7-66055e976a900"`, true},
		{`"7-66055e976a900"`, `W/"7-66055e976a900"`, true},
		{`W/"7-66055e976a900"`, `W/"7-66055e976a900"`, true},
		{`"7-66055e976a900"`, `"7-66055e976a901"`, false},
		{`W/"7-66055e976a900"`, `"8-66055e976a900"`, false},
		{`W/"7-66055e976a900"`, `W/"7-66055e976a901"`, false},
		// Not entity tags: compared exactly, no prefix removed.
		{`W/abc`, `abc`, false},
		{`rev-1`, `rev-1`, true},
		{`rev-1`, `rev-2`, false},
		// The prefix is case-sensitive (RFC 9110: %s"W/"), and only one.
		{`w/"x"`, `"x"`, false},
		{`W/W/"x"`, `"x"`, false},
		// An empty revision is no revision: it names no state, so it is the
		// same as nothing, not even another empty revision.
		{``, ``, false},
		{``, `"x"`, false},
		{`W/"x"`, ``, false},
	} {
		if got := SameRevision(c.a, c.b); got != c.same {
			t.Errorf("SameRevision(%q, %q) = %v; want %v", c.a, c.b, got, c.same)
		}
	}
}
