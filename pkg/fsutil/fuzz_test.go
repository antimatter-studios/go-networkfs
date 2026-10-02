// Fuzz targets for the path helpers.
//
// WHY THESE FUNCTIONS. The constellation's fuzzing standard targets the code
// that takes bytes off the wire from a peer it does not control. In the Rust
// drivers that is an on-disk format parser; here there is no format, and the
// equivalent is a NAME CHOSEN BY A REMOTE SERVER. Every driver turns entries a
// server hands it into paths through this package: a listing from Dropbox,
// Google Drive, OneDrive, WebDAV, FTP or S3 can contain a name with embedded
// slashes, a NUL, a lone surrogate, nothing but dots, or four kilobytes of
// nothing — and none of that is refused anywhere upstream.
//
// WHAT IS ASSERTED. Not "it does not panic" alone, which a target gets for
// free and which would make these tests satisfied by any total function. Each
// one states a property the callers actually depend on — that a name never
// contains a separator, that normalisation is idempotent, that climbing to a
// parent terminates. Those are the statements a hostile listing would have to
// break to get somewhere.
//
// HOW THEY RUN. Go's fuzzing needs no third-party tooling and no separate
// deterministic tier: `go test ./...` replays every f.Add seed and every file
// in testdata/fuzz/<Target>/ as an ordinary unit test, so the gate the Rust
// repositories had to build by hand comes free here and runs on every pull
// request. `chore fuzz` is the explorer, on a bounded budget; anything it
// finds is written back into testdata/ as a regression seed.

package fsutil

import (
	"strings"
	"testing"
)

// NameFromPath must return a NAME: one segment, never a path.
//
// Every driver fills api.FileInfo.Name with this. A result that still held a
// separator would be joined back onto a directory by fsutil.Walk and address
// somewhere else entirely.
func FuzzNameFromPath(f *testing.F) {
	f.Add("/a/b/c.txt")
	f.Add("/a/b/")
	f.Add("/")
	f.Add("")
	f.Add("///")
	f.Add("..")
	f.Add("/a//b")
	f.Add("\x00")
	f.Add(strings.Repeat("/", 4096))

	f.Fuzz(func(t *testing.T, path string) {
		name := NameFromPath(path)

		if strings.Contains(name, "/") {
			t.Fatalf("NameFromPath(%q) = %q, which is a path and not a name", path, name)
		}
		if name != "" && !strings.Contains(path, name) {
			t.Fatalf("NameFromPath(%q) = %q, which is not a segment of the input", path, name)
		}
		// The root has no name, and a path made only of separators is the
		// root however many of them there are.
		if name == "" && strings.Trim(path, "/") != "" {
			t.Fatalf("NameFromPath(%q) = \"\", but the path has a segment to name", path)
		}
	})
}

// joinPath must always produce an absolute path, and the name must survive it.
func FuzzJoinPath(f *testing.F) {
	f.Add("/a", "b")
	f.Add("", "b")
	f.Add("/", "b")
	f.Add("/a///", "b")
	f.Add("/a", "")
	f.Add("/a", "b/c")
	f.Add("/a", "..")

	f.Fuzz(func(t *testing.T, dir, name string) {
		joined := joinPath(dir, name)

		if !strings.HasPrefix(joined, "/") && !strings.HasPrefix(dir, "/") && dir != "" {
			// A relative dir gives a relative join, which is the caller's
			// doing; Walk only ever passes absolute ones.
			return
		}
		if !strings.HasPrefix(joined, "/") {
			t.Fatalf("joinPath(%q, %q) = %q, which is not absolute", dir, name, joined)
		}
		if !strings.HasSuffix(joined, name) {
			t.Fatalf("joinPath(%q, %q) = %q, which lost the name", dir, name, joined)
		}
		// Exactly one separator between the two, however many the directory
		// arrived with: a doubled one is a different path to some servers.
		if name != "" && strings.HasSuffix(joined, "//"+name) {
			t.Fatalf("joinPath(%q, %q) = %q, which doubled the separator", dir, name, joined)
		}
	})
}

// parentPath must climb, never descend, and must terminate at the root.
func FuzzParentPath(f *testing.F) {
	f.Add("/a/b/c")
	f.Add("/a")
	f.Add("/")
	f.Add("")
	f.Add("//////")
	f.Add("a/b")
	f.Add(strings.Repeat("/a", 512))

	f.Fuzz(func(t *testing.T, p string) {
		parent := parentPath(p)

		// The root is the fixed point and the only answer allowed to be
		// longer than its input: parentPath("") is "/", which is correct and
		// not a climb. Everything else must be a prefix of its child.
		if parent != "/" && !strings.HasPrefix(trimRightSlash(p), parent) {
			t.Fatalf("parentPath(%q) = %q, which is not an ancestor of it", p, parent)
		}
		if parent == "" {
			t.Fatalf("parentPath(%q) = %q, which names nothing", p, parent)
		}
		// Climbing has to reach the root. An input whose parent is itself
		// would make Glob's ascent loop forever.
		if parent == p && p != "/" {
			t.Fatalf("parentPath(%q) is itself, so the climb never terminates", p)
		}
	})
}

// normaliseRoot must be idempotent and must never hand back an empty root.
//
// Glob calls it once and then treats the result as a prefix. A second
// normalisation changing the answer would mean the prefix depended on how many
// times it had been through, which is the shape of a bug nobody reads twice.
func FuzzNormaliseRoot(f *testing.F) {
	f.Add("")
	f.Add("/")
	f.Add("/a/")
	f.Add("/a///")
	f.Add("a")
	f.Add(strings.Repeat("/", 1024))

	f.Fuzz(func(t *testing.T, r string) {
		once := normaliseRoot(r)
		twice := normaliseRoot(once)

		if once == "" {
			t.Fatalf("normaliseRoot(%q) = \"\", which is not a root", r)
		}
		if once != twice {
			t.Fatalf("normaliseRoot is not idempotent: %q -> %q -> %q", r, once, twice)
		}
		if len(once) > 1 && strings.HasSuffix(once, "/") {
			t.Fatalf("normaliseRoot(%q) = %q, which keeps a trailing separator", r, once)
		}
	})
}

// NormPath must be idempotent and must always produce an absolute path with no
// trailing separator.
func FuzzNormPath(f *testing.F) {
	f.Add("")
	f.Add("/")
	f.Add("a")
	f.Add("/a/b/")
	f.Add("/a/b///")
	f.Add("//")
	f.Add("../../etc")
	f.Add("\x00")
	f.Add(strings.Repeat("/", 2048))

	f.Fuzz(func(t *testing.T, path string) {
		once := NormPath(path)
		twice := NormPath(once)

		if once != twice {
			t.Fatalf("NormPath is not idempotent: %q -> %q -> %q", path, once, twice)
		}
		if !strings.HasPrefix(once, "/") {
			t.Fatalf("NormPath(%q) = %q, which is not absolute", path, once)
		}
		if len(once) > 1 && strings.HasSuffix(once, "/") {
			t.Fatalf("NormPath(%q) = %q, which keeps a trailing separator", path, once)
		}
	})
}
