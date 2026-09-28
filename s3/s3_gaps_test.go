//go:build s3_integration

// What the test server does NOT implement, asserted.
//
// stupid-simple-s3 covers everything s3.go calls — ranged reads, delimiter
// listings, multipart upload, HEAD metadata, copy, pagination past one page —
// which is why it is a fair replacement for MinIO (#10). Three things it does
// not implement, and none of them is reached by the driver today. That is
// exactly the problem issue #11 describes: a change that started using one
// would pass `chore test:s3` and `integration (containerised)` and still be
// broken against AWS or any other server, because the suite would be testing
// a stub of it.
//
// SO THE GAPS THEMSELVES ARE THE ASSERTION. Each case below states what the
// server does today and names what would reach it. The day sss3 implements
// one, THIS FILE GOES RED — which is the moment the decision in #11 needs
// revisiting, and the only moment at which anybody would otherwise notice.
// A test that passed either way would be documentation, and documentation is
// what this repository already had.
//
// Nothing here is weakened to make it green: the gap is in the server, not in
// the tests. The other half of #11 — a second container with a fuller
// implementation, run only for these cases — is deliberately not built. It is
// the worse trade while none of the three is reached, and scripts/tests/
// s3-server-gaps.sh is what notices when one is.
//
// Measured against sss3 1.0.7 on 2026-09-28:
//
//	ListBuckets -> [], err=The specified bucket does not exist.
//	ComposeObject -> err=EOF
//	ListObjects MaxKeys=1 over 5 keys -> 5 events

package s3

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// gapsClient is the same minio-go v7 client the driver builds, pointed at the
// same server, so what it sees is what the driver would see.
func gapsClient(t *testing.T) (*minio.Client, string) {
	t.Helper()
	cfg := requireEnv(t)
	ensureBucket(t, cfg)
	c, err := minio.New(cfg["endpoint"], &minio.Options{
		Creds:        credentials.NewStaticV4(cfg["access_key_id"], cfg["secret_access_key"], ""),
		Secure:       strings.EqualFold(cfg["secure"], "true"),
		Region:       "us-east-1",
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		t.Fatalf("minio client: %v", err)
	}
	return c, cfg["bucket"]
}

// ListBuckets (GET /) is not implemented: the server answers NoSuchBucket.
//
// What would reach it: any "which buckets can I see" feature, and a Mount that
// discovered a bucket instead of being told one. s3.go asks BucketExists for a
// named bucket, which is a different request and works.
func TestServerGap_ListBuckets(t *testing.T) {
	c, _ := gapsClient(t)

	buckets, err := c.ListBuckets(context.Background())
	if err == nil {
		t.Fatalf("ListBuckets succeeded and returned %d buckets — the test server has "+
			"grown the operation issue #11 records as missing. Re-read that issue: the "+
			"driver can now be given a bucket-discovery path that this suite would "+
			"actually exercise.", len(buckets))
	}
	t.Logf("ListBuckets is still unimplemented: %v", err)
}

// UploadPartCopy is not implemented: the server closes the connection, which
// surfaces as EOF.
//
// What would reach it: minio-go's ComposeObject, and CopyObject on an object
// over 5 GiB — above that size minio-go switches to the multipart copy path
// inside the library rather than in any code here. Rename (s3.go's CopyObject
// call) is therefore one large file away from this, and nothing in the suite
// would catch it.
//
// ComposeObject with two sources is the cheap way to reach the same server
// operation: minio-go requires every part but the last to be at least 5 MiB,
// so this writes 12 MiB rather than the 5 GiB a size-triggered CopyObject
// would need.
func TestServerGap_UploadPartCopy(t *testing.T) {
	c, bucket := gapsClient(t)
	ctx := context.Background()

	prefix := "gonfs-gap-uploadpartcopy/"
	part := bytes.Repeat([]byte("x"), 6<<20)
	for _, name := range []string{"a", "b"} {
		key := prefix + name
		if _, err := c.PutObject(ctx, bucket, key, bytes.NewReader(part),
			int64(len(part)), minio.PutObjectOptions{}); err != nil {
			t.Fatalf("PutObject %s: %v", key, err)
		}
		t.Cleanup(func() { _ = c.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}) })
	}

	dst := minio.CopyDestOptions{Bucket: bucket, Object: prefix + "joined"}
	_, err := c.ComposeObject(ctx, dst,
		minio.CopySrcOptions{Bucket: bucket, Object: prefix + "a"},
		minio.CopySrcOptions{Bucket: bucket, Object: prefix + "b"},
	)
	if err == nil {
		_ = c.RemoveObject(ctx, bucket, prefix+"joined", minio.RemoveObjectOptions{})
		t.Fatal("ComposeObject succeeded — the test server has grown UploadPartCopy. " +
			"That is the one gap in issue #11 with teeth: Rename on an object over " +
			"5 GiB takes this path inside minio-go. It can now be tested; write that " +
			"case and take this one out.")
	}
	t.Logf("UploadPartCopy is still unimplemented: %v", err)
}

// max-keys is ignored: the server returns the full listing whatever is asked
// for.
//
// What reaches it TODAY: Stat's directory probe passes MaxKeys: 1 (s3.go) and
// gets every key back. That is correct — it reads one event and cancels the
// context — but a listing of a million-key prefix is built server-side to
// answer "is this a directory", and nothing here would show that.
func TestServerGap_MaxKeysIgnored(t *testing.T) {
	c, bucket := gapsClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prefix := "gonfs-gap-maxkeys/"
	const written = 5
	for i := 0; i < written; i++ {
		key := fmt.Sprintf("%so%d", prefix, i)
		if _, err := c.PutObject(ctx, bucket, key, bytes.NewReader([]byte("x")), 1,
			minio.PutObjectOptions{}); err != nil {
			t.Fatalf("PutObject %s: %v", key, err)
		}
		t.Cleanup(func() {
			_ = c.RemoveObject(context.Background(), bucket, key, minio.RemoveObjectOptions{})
		})
	}

	events := 0
	for obj := range c.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix, MaxKeys: 1}) {
		if obj.Err != nil {
			t.Fatalf("ListObjects: %v", obj.Err)
		}
		events++
	}

	if events <= 1 {
		t.Fatalf("ListObjects with MaxKeys=1 yielded %d event(s) over %d keys — the test "+
			"server now honours max-keys. Issue #11 records it as ignored; the "+
			"directory probe in Stat can be trusted to cost one key again.", events, written)
	}
	t.Logf("max-keys is still ignored: MaxKeys=1 yielded %d events over %d keys", events, written)
}
