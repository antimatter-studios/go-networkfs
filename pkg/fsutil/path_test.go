package fsutil

import "testing"

func TestNameFromPath(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"/a/b/c.txt", "c.txt"},
		{"/a/b/", "b"},
		{"/single", "single"},
		{"single", "single"},
		{"a/b/c", "c"},

		// The root has no name, whichever way it is spelled.
		{"/", ""},
		{"", ""},
		{"//", ""},
		{"///", ""},
	} {
		if got := NameFromPath(tt.in); got != tt.want {
			t.Errorf("NameFromPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// NormPath canonicalises: "" and "/" -> "/"; otherwise ensure leading
// slash and trim trailing slashes.
func TestNormPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "/"},
		{"/", "/"},
		{"/foo", "/foo"},
		{"/foo/", "/foo"},
		{"/foo/bar", "/foo/bar"},
		{"/foo/bar/", "/foo/bar"},
		{"/foo/bar///", "/foo/bar"},
		{"//", "/"},
		{"///", "/"},
		{"foo", "/foo"},
		{"foo/", "/foo"},
		{"foo/bar", "/foo/bar"},
		{"foo/bar/", "/foo/bar"},
		{"/a/b/c.txt", "/a/b/c.txt"},
		{"/日本語", "/日本語"},
		{"日本語/café", "/日本語/café"},
	}
	for _, c := range cases {
		if got := NormPath(c.in); got != c.want {
			t.Errorf("NormPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
