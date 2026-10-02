package gdrive

import "testing"

// A path of nothing but separators is the root. normPath used to turn "//"
// into "", and splitParent then sliced past the end of it and panicked --
// inside a cgo library, which takes the host process down (#41).
func TestSplitParentOfSeparatorsIsTheRoot(t *testing.T) {
	for _, in := range []string{"//", "///", "/"} {
		if parent, name := splitParent(in); parent != "/" || name != "" {
			t.Errorf("splitParent(%q) = (%q, %q), want (\"/\", \"\")", in, parent, name)
		}
	}
}
