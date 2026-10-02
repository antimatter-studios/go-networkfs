// Fuzz targets for the path/key mapping.
//
// WHY THESE FUNCTIONS. An S3 key is an opaque byte string, so the server puts
// no constraint on what comes back from a listing: a key may hold a newline, a
// NUL, a run of separators, a leading "..", or nothing at all. fsutil.NormPath
// and toKey are what stand between that and the rest of the driver, and they are
// pure functions of a string, which is what makes them the cheapest real fuzz
// surface this package has.
//
// WHAT IS ASSERTED. Idempotence, because NormPath's result is fed back through
// the driver's own helpers and a normalisation whose answer depended on how
// many times it had run would be a bug nobody reads twice; and CONTAINMENT for
// toKey, because a configured prefix is the only thing keeping one mount's
// keys out of another's, and a path that escaped it would read and write
// somewhere the mount was never given.

package s3

import (
	"github.com/christhomas/go-networkfs/pkg/fsutil"
	"strings"
	"testing"
)

// normalizePrefix must produce "" or something ending in exactly one
// separator, and must be idempotent — it is applied to configuration once at
// mount and its result is concatenated on every request afterwards.
func FuzzNormalizePrefix(f *testing.F) {
	f.Add("")
	f.Add("/")
	f.Add("team")
	f.Add("/team/")
	f.Add("///team///")
	f.Add("a/b")

	f.Fuzz(func(t *testing.T, prefix string) {
		once := normalizePrefix(prefix)
		twice := normalizePrefix(once)

		if once != twice {
			t.Fatalf("normalizePrefix is not idempotent: %q -> %q -> %q", prefix, once, twice)
		}
		if once == "" {
			return
		}
		if !strings.HasSuffix(once, "/") {
			t.Fatalf("normalizePrefix(%q) = %q, which does not end in a separator", prefix, once)
		}
		if strings.HasPrefix(once, "/") {
			t.Fatalf("normalizePrefix(%q) = %q, which starts with a separator and would "+
				"address an empty first key segment", prefix, once)
		}
	})
}

// The mapping the driver performs in both directions must be the identity on
// keys, and must never leave the configured prefix.
//
// ListDir turns an object key into a path by putting a separator in front of
// the part after the prefix; every other method turns a path back into a key
// with toKey(fsutil.NormPath(path)). Those two have to agree, or a listing names
// entries that cannot then be opened — and the key is chosen by the SERVER,
// not by any caller here, so "nobody would name an object that" is not an
// argument available.
//
// AN S3 KEY MAY BEGIN WITH A SEPARATOR. "/0" is a legal key with an empty
// first segment, and the driver represents it as the path "//0" — which looks
// wrong and is not: it round-trips exactly. That is why this asserts the round
// trip rather than "a key never starts with a separator", which would have
// been an invariant about taste rather than about behaviour.
//
// A key with a TRAILING separator is excluded: those are the driver's
// directory markers and ListDir strips the marker deliberately.
func FuzzKeyPathRoundTrip(f *testing.F) {
	f.Add("team", "a/b.txt")
	f.Add("team", "")
	f.Add("", "a/b.txt")
	f.Add("", "/0")
	f.Add("", "..")
	f.Add("", "a//b")
	f.Add("/team/", "x")

	f.Fuzz(func(t *testing.T, prefix, key string) {
		if strings.HasSuffix(key, "/") {
			return // a directory marker, which ListDir strips on purpose
		}
		d := &S3Driver{prefix: normalizePrefix(prefix)}

		// What ListDir would build for an object at d.prefix + key.
		path := "/" + key
		got := d.toKey(fsutil.NormPath(path))

		if !strings.HasPrefix(got, d.prefix) {
			t.Fatalf("toKey(fsutil.NormPath(%q)) = %q with prefix %q, which is outside the mount",
				path, got, d.prefix)
		}
		want := d.prefix + key
		if got != want {
			t.Fatalf("key %q became path %q and came back as %q, want %q",
				want, path, got, want)
		}
	})
}
