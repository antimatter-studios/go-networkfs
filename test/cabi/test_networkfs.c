// A consumer's-eye test of libnetworkfs.a.
//
// Every test elsewhere in this repository calls the Go code from Go. Nothing
// exercised the artifact that actually ships: the archive, its generated
// header, and the C entry points a consumer links against. This does, by being
// an ordinary C program that includes the header and links the archive.
//
// It reaches three functions no Go test can. networkfs_openfile,
// networkfs_writefile and setOutBytes take a ByteSlice or a size_t, and a Go
// test file may not `import "C"`, so it cannot name either type. In C they are
// ordinary arguments.
//
// It also checks things a Go test structurally cannot see: that the header
// matches the implementation, that every symbol was exported, and that the
// archive links at all.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "libnetworkfs.h"
#include "http_fixture.h"
#include "revision.h"

static int failures = 0;
static int checks = 0;

// THE FLOORS, AND WHY A HARNESS THAT RAN NOTHING HAS TO FAIL.
//
// main returned `failures == 0 ? 0 : 1`, so `checks` was printed and never
// asserted on: a harness that executed forty checks and a harness that
// executed none exited identically, and so did one that stopped a third of
// the way through. Only a count can tell those apart. Issue #6.
//
// TWO NUMBERS, because a skip is right on a laptop with no servers up and
// wrong in the job whose whole reason for existing is that the servers are
// there. Without CABI_CONFIG the mounted round trip does not run and 49
// checks is the honest total; with it, all 81 must.
//
// EQUALITY, NOT "AT LEAST". A check added without moving the number is an
// unreviewed change to what this file claims to prove, and it is exactly as
// interesting as one removed. Raise it in the same commit that adds the check.
#define EXPECTED_CHECKS_OFFLINE 56
#define EXPECTED_CHECKS_MOUNTED 88

static void check(int cond, const char *what) {
    checks++;
    if (!cond) {
        failures++;
        fprintf(stderr, "FAIL: %s\n", what);
    }
}

// ---------------------------------------------------------------------------

static void test_version(void) {
    char *v = networkfs_version();
    check(v != NULL, "version returns a string");
    if (v) {
        check(strlen(v) > 0, "version is not empty");
        networkfs_free(v);
    }
}

static void test_free_tolerates_null(void) {
    networkfs_free(NULL);
    check(1, "free(NULL) does not crash");
}

// The archive links every driver, so the registry must report them all. This
// is the failure the unified archive exists to prevent and one no per-driver
// test can see.
static void test_drivers_are_all_linked(void) {
    char *out = NULL;
    int rc = networkfs_drivers(&out);

    check(rc == 0, "drivers returns 0");
    check(out != NULL, "drivers writes a result");
    if (!out) return;

    // The payload is a JSON array of ints; count the commas rather than
    // parsing, since a JSON parser is not the thing under test.
    int commas = 0;
    for (const char *p = out; *p; p++) {
        if (*p == ',') commas++;
    }
    check(commas + 1 >= 8, "all eight drivers are linked in");
    networkfs_free(out);
}

// The three mount failures are told apart by return code, and a consumer
// branches on them.
static void test_mount_failure_codes(void) {
    char *err = NULL;

    check(networkfs_mount(1, 3, "{not json", &err) == -1, "malformed JSON returns -1");
    check(err != NULL, "malformed JSON writes an error message");
    if (err) { networkfs_free(err); err = NULL; }

    check(networkfs_mount(1, 999, "{}", &err) == 1, "unknown driver type returns 1");
    check(err != NULL, "unknown driver type writes an error message");
    if (err) {
        check(strstr(err, "999") != NULL, "the message names the driver type");
        networkfs_free(err);
        err = NULL;
    }

    check(networkfs_mount(1, 3, "{\"host\":\"\"}", &err) == 2, "a refused config returns 2");
    check(err != NULL, "a refused config writes an error message");
    if (err) { networkfs_free(err); err = NULL; }
}

// Reachable only from C: ByteSlice cannot be named in a Go test file.
static void test_openfile_on_unknown_mount(void) {
    ByteSlice out;
    out.data = NULL;
    out.len = 0;

    int rc = networkfs_openfile(4242, "/nope", &out);

    check(rc != 0, "openfile on an unknown mount fails");
    check(out.data == NULL, "a failed openfile leaves no buffer to free");
    check(out.len == 0, "a failed openfile reports zero length");
    if (out.data) networkfs_free(out.data);
}

// Also reachable only from C.
static void test_writefile_on_unknown_mount(void) {
    const char *payload = "some bytes";
    ByteSlice data;
    data.data = (char *)payload;
    data.len = strlen(payload);

    check(networkfs_writefile(4242, "/nope", data) != 0,
          "writefile on an unknown mount fails");

    // A zero-length slice must be handled rather than dereferenced.
    ByteSlice empty;
    empty.data = NULL;
    empty.len = 0;
    check(networkfs_writefile(4242, "/nope", empty) != 0,
          "writefile with an empty slice fails cleanly");
}

static void test_operations_on_unknown_mount(void) {
    char *out = NULL;

    check(networkfs_stat(4242, "/p", &out) != 0, "stat on an unknown mount fails");
    if (out) {
        check(strstr(out, "mount") != NULL, "stat says the mount is missing");
        networkfs_free(out);
        out = NULL;
    }

    check(networkfs_listdir(4242, "/p", &out) != 0, "listdir on an unknown mount fails");
    if (out) { networkfs_free(out); out = NULL; }

    check(networkfs_mkdir(4242, "/p") != 0, "mkdir on an unknown mount fails");
    check(networkfs_remove(4242, "/p") != 0, "remove on an unknown mount fails");
    check(networkfs_rename(4242, "/a", "/b") != 0, "rename on an unknown mount fails");

    ByteSlice data;
    data.data = "x";
    data.len = 1;
    check(networkfs_writefile_if(4242, "/p", data, "\"r\"") == 1, "writefile_if on an unknown mount returns 1");
    check(networkfs_remove_if(4242, "/p", "\"r\"") == 1, "remove_if on an unknown mount returns 1");
    check(networkfs_rename_if(4242, "/a", "/b", "\"r\"", NULL) == 1, "rename_if on an unknown mount returns 1");
    char *new_revision = (char *)"sentinel";
    check(networkfs_writefile_if_revision(4242, "/p", data, "\"r\"", &new_revision) == 1 && new_revision == NULL,
          "writefile_if_revision on an unknown mount returns 1 and no revision");
    check(networkfs_writefile_if_revision(4242, "/p", data, "\"r\"", NULL) == 1,
          "writefile_if_revision takes a NULL revision out-pointer");

    check(networkfs_unmount(4242) != 0, "unmount of an unknown mount fails");
}

// Mounts the in-process HTTP fixture as a WebDAV mount (driver type 5).
static int mount_fixture(int mount_id, const char *config) {
    char *err = NULL;
    int rc = networkfs_mount(mount_id, 5 /* WebDAV */, (char *)config, &err);
    if (err) networkfs_free(err);
    return rc;
}

// A download is all of the file or a failure, never a successful prefix (#64).
//
// The fixture promises ten bytes and sends three before closing the
// connection. The export must report the failure, and a failed openfile hands
// back no buffer: out.data is NULL and out.len is zero whatever they held
// before, so the caller has nothing to free. A whole file and an empty file
// still read back.
static void test_download_fidelity(void) {
    static char sentinel;
    ByteSlice out;

    out.data = &sentinel;
    out.len = 99;
    check(networkfs_openfile(9998, "/nope", &out) != 0, "openfile on an unknown mount fails");
    check(out.data == NULL && out.len == 0, "a failed openfile clears the caller's buffer and length");

    http_fixture fx;
    if (fixture_start(&fx) != 0) {
        check(0, "the HTTP fixture starts");
        return;
    }
    fixture_put(&fx, "/truncated", "0123456789", 10, 1, 3);
    fixture_put(&fx, "/whole", "hello", 5, 0, 0);
    fixture_put(&fx, "/empty", "", 0, 0, 0);

    const int id = 7;
    char cfg[256];
    fixture_config(&fx, cfg, sizeof cfg);
    if (mount_fixture(id, cfg) != 0) {
        check(0, "mount the HTTP fixture");
        return;
    }
    check(1, "mount the HTTP fixture");

    out.data = &sentinel;
    out.len = 99;
    check(networkfs_openfile(id, "/truncated", &out) != 0, "a download cut short of its Content-Length fails");
    check(out.data == NULL && out.len == 0, "a truncated download hands back no buffer");
    if (out.data && out.data != &sentinel) networkfs_free(out.data);

    out.data = NULL;
    out.len = 0;
    check(networkfs_openfile(id, "/whole", &out) == 0, "a whole download succeeds");
    check(out.len == 5 && out.data != NULL && memcmp(out.data, "hello", 5) == 0,
          "a whole download returns every byte");
    if (out.data) networkfs_free(out.data);

    out.data = &sentinel;
    out.len = 99;
    check(networkfs_openfile(id, "/empty", &out) == 0, "an empty download succeeds");
    check(out.data == NULL && out.len == 0, "an empty download hands back no buffer and zero length");
    if (out.data && out.data != &sentinel) networkfs_free(out.data);

    networkfs_unmount(id);
}

// A write's length is checked before any memory is read or any request is
// sent (#65).
//
// ByteSlice.len is a size_t. The export narrowed it to a C int before reading,
// so 1 << 32 became zero and an empty upload replaced the file. The buffer
// here is one byte long and is never read: an oversized length, or a NULL
// pointer with a nonzero length, must fail with no PUT or MOVE reaching the
// server and the target unchanged. A valid empty write still empties a file.
static void test_write_length_bounds(void) {
    http_fixture fx;
    if (fixture_start(&fx) != 0) {
        check(0, "the HTTP fixture starts");
        return;
    }
    fixture_put(&fx, "/target", "original", 8, 0, 0);

    const int id = 8;
    char cfg[256];
    fixture_config(&fx, cfg, sizeof cfg);
    if (mount_fixture(id, cfg) != 0) {
        check(0, "mount the HTTP fixture");
        return;
    }
    check(1, "mount the HTTP fixture");

    char body[64];
    static char one_byte = 'x';
    ByteSlice data;

    data.data = &one_byte;
    data.len = (size_t)1 << 32;
    check(networkfs_writefile(id, "/target", data) != 0, "a 4 GiB length over a one-byte buffer fails");
    check(fixture_get(&fx, "/target", body, sizeof body) == 8 && memcmp(body, "original", 8) == 0 &&
              fixture_count(&fx, &fx.puts) == 0 && fixture_count(&fx, &fx.moves) == 0,
          "an oversized write sends nothing and leaves the target unchanged");

    data.data = NULL;
    data.len = 5;
    check(networkfs_writefile(id, "/target", data) != 0, "a NULL buffer with a nonzero length fails");
    check(fixture_get(&fx, "/target", body, sizeof body) == 8 && memcmp(body, "original", 8) == 0 &&
              fixture_count(&fx, &fx.puts) == 0 && fixture_count(&fx, &fx.moves) == 0,
          "a NULL buffer sends nothing and leaves the target unchanged");

    data.data = "replaced";
    data.len = 8;
    check(networkfs_writefile(id, "/target", data) == 0, "an ordinary write succeeds");
    check(fixture_get(&fx, "/target", body, sizeof body) == 8 && memcmp(body, "replaced", 8) == 0,
          "an ordinary write replaces the target");

    data.data = NULL;
    data.len = 0;
    check(networkfs_writefile(id, "/target", data) == 0, "an empty write succeeds");
    check(fixture_get(&fx, "/target", body, sizeof body) == 0, "an empty write empties the target");

    networkfs_unmount(id);
}

// ---------------------------------------------------------------------------
// Mounted tests.
//
// Everything above runs without a server and therefore only ever reaches the
// failure paths. The success paths are where the interesting marshalling lives
// — setOutBytes hands a buffer and a length back across the boundary, and it
// is only reached when a read actually succeeds.
//
// These run when SMB_HOST is set, which `chore test:cabi` does after starting
// the Samba container.

static int have_server(void) { return getenv("SMB_HOST") != NULL; }

static int mount_smb(int mount_id) {
    const char *host = getenv("SMB_HOST");
    const char *port = getenv("SMB_PORT");
    const char *share = getenv("SMB_SHARE");
    const char *user = getenv("SMB_USER");
    const char *pass = getenv("SMB_PASS");

    char config[1024];
    snprintf(config, sizeof config,
             "{\"host\":\"%s\",\"port\":\"%s\",\"share\":\"%s\","
             "\"user\":\"%s\",\"pass\":\"%s\"}",
             host ? host : "", port ? port : "445", share ? share : "",
             user ? user : "", pass ? pass : "");

    char *err = NULL;
    int rc = networkfs_mount(mount_id, 3 /* SMB */, config, &err);
    if (rc != 0) {
        fprintf(stderr, "mount failed (%d): %s\n", rc, err ? err : "no message");
    }
    if (err) networkfs_free(err);
    return rc;
}

// A write followed by a read back, entirely through the C ABI. This is the
// path a consumer takes and the only one that reaches setOutBytes.
static void test_write_then_read_back(void) {
    const int id = 1;
    if (mount_smb(id) != 0) {
        check(0, "mount against the test server");
        return;
    }
    check(1, "mount against the test server");

    const char *path = "/cabi-roundtrip.txt";
    const char *payload = "written through the C ABI";

    ByteSlice data;
    data.data = (char *)payload;
    data.len = strlen(payload);
    check(networkfs_writefile(id, (char *)path, data) == 0, "writefile succeeds");

    ByteSlice out;
    out.data = NULL;
    out.len = 0;
    int rc = networkfs_openfile(id, (char *)path, &out);

    check(rc == 0, "openfile succeeds");
    check(out.data != NULL, "openfile hands back a buffer");
    check(out.len == strlen(payload), "openfile reports the right length");
    if (out.data && out.len == strlen(payload)) {
        check(memcmp(out.data, payload, out.len) == 0, "the bytes survive the round trip");
    }
    if (out.data) networkfs_free(out.data);

    char *info = NULL;
    check(networkfs_stat(id, (char *)path, &info) == 0, "stat succeeds");
    if (info) {
        check(strstr(info, "cabi-roundtrip.txt") != NULL, "stat names the file");
        networkfs_free(info);
    }

    char *listing = NULL;
    check(networkfs_listdir(id, "/", &listing) == 0, "listdir succeeds");
    if (listing) {
        check(strstr(listing, "cabi-roundtrip.txt") != NULL, "the file appears in the listing");
        networkfs_free(listing);
    }

    check(networkfs_remove(id, (char *)path) == 0, "remove succeeds");
    check(networkfs_unmount(id) == 0, "unmount succeeds");
}

// An empty file is the edge of the ByteSlice contract: no bytes, and nothing
// for the caller to free.
static void test_empty_file_round_trip(void) {
    const int id = 2;
    if (mount_smb(id) != 0) return;

    const char *path = "/cabi-empty.txt";
    ByteSlice data;
    data.data = NULL;
    data.len = 0;
    check(networkfs_writefile(id, (char *)path, data) == 0, "writefile of an empty file succeeds");

    ByteSlice out;
    out.data = NULL;
    out.len = 1;
    check(networkfs_openfile(id, (char *)path, &out) == 0, "openfile of an empty file succeeds");
    check(out.len == 0, "an empty file reports zero length");
    if (out.data) networkfs_free(out.data);

    networkfs_remove(id, (char *)path);
    networkfs_unmount(id);
}

static void test_mkdir_and_rename(void) {
    const int id = 3;
    if (mount_smb(id) != 0) return;

    check(networkfs_mkdir(id, "/cabi-dir") == 0, "mkdir succeeds");
    check(networkfs_rename(id, "/cabi-dir", "/cabi-dir-moved") == 0, "rename succeeds");
    check(networkfs_remove(id, "/cabi-dir-moved") == 0, "remove of the renamed directory succeeds");
    networkfs_unmount(id);
}


// A driver whose server cannot enforce a revision has every conditional
// export refused with 3, and none of them falls back to the unconditional
// operation (#70). SMB is such a driver.
static void test_conditional_unsupported(void) {
    const int id = 4;
    if (mount_smb(id) != 0) {
        check(0, "mount SMB for the conditional exports");
        return;
    }
    const char *path = "/cabi-conditional.txt";
    const char *moved = "/cabi-conditional-moved.txt";
    char *info = NULL;
    ByteSlice data;
    data.data = "conditional";
    data.len = 11;

    check(networkfs_writefile_if(id, (char *)path, data, NULL) == 3, "writefile_if on SMB returns 3");
    check(networkfs_stat(id, (char *)path, &info) != 0, "the refused writefile_if wrote nothing");
    if (info) { networkfs_free(info); info = NULL; }

    networkfs_writefile(id, (char *)path, data);
    check(networkfs_remove_if(id, (char *)path, "\"r\"") == 3, "remove_if on SMB returns 3");
    check(networkfs_rename_if(id, (char *)path, (char *)moved, "\"r\"", NULL) == 3, "rename_if on SMB returns 3");
    check(networkfs_stat(id, (char *)path, &info) == 0, "the refused remove_if and rename_if left the file");
    if (info) { networkfs_free(info); info = NULL; }

    networkfs_remove(id, (char *)path);
    networkfs_unmount(id);
}

// A WebDAV mount's revision travels out through networkfs_stat and back in
// through networkfs_writefile_if, and a stale one returns 2 (#70). Two mount
// IDs are two clients of the same server.
static void test_conditional_webdav(void) {
    const char *url = getenv("WEBDAV_URL");
    const char *user = getenv("WEBDAV_USER");
    const char *pass = getenv("WEBDAV_PASS");
    if (!url || !user || !pass) {
        check(0, "WEBDAV_URL, WEBDAV_USER and WEBDAV_PASS are set (scripts/cabi.sh cabi_unified)");
        return;
    }
    char config[1024];
    snprintf(config, sizeof config, "{\"url\":\"%s\",\"user\":\"%s\",\"pass\":\"%s\"}", url, user, pass);
    const int a = 5, b = 6;
    if (mount_fixture(a, config) != 0 || mount_fixture(b, config) != 0) {
        check(0, "mount two WebDAV clients");
        return;
    }
    const char *path = "/cabi-networkfs-conditional.txt";
    ByteSlice data;
    data.data = "first";
    data.len = 5;
    check(networkfs_writefile(a, (char *)path, data) == 0, "client A writes through networkfs_writefile");

    char rev[256];
    char *info = NULL;
    rev[0] = '\0';
    if (networkfs_stat(a, (char *)path, &info) == 0 && info) {
        revision_from_json(info, rev, sizeof rev);
    }
    if (info) { networkfs_free(info); info = NULL; }
    check(rev[0] != '\0', "networkfs_stat reports a revision");

    data.data = "other";
    check(networkfs_writefile(b, (char *)path, data) == 0, "client B overwrites it");
    data.data = "stale";
    check(networkfs_writefile_if(a, (char *)path, data, rev) == 2, "a stale networkfs_writefile_if returns 2");

    ByteSlice out;
    out.data = NULL;
    out.len = 0;
    check(networkfs_openfile(a, (char *)path, &out) == 0 && out.len == 5 && memcmp(out.data, "other", 5) == 0,
          "the refused write left client B's contents");
    if (out.data) networkfs_free(out.data);

    rev[0] = '\0';
    if (networkfs_stat(a, (char *)path, &info) == 0 && info) {
        revision_from_json(info, rev, sizeof rev);
    }
    if (info) { networkfs_free(info); info = NULL; }
    check(networkfs_remove_if(a, (char *)path, rev) == 0, "networkfs_remove_if at the current revision returns 0");

    // networkfs_writefile_if_revision hands back the revision it created
    // (#84); a write at it after client B's save returns 2 and no revision.
    const char *carried = "/cabi-networkfs-carried.txt";
    char *created = NULL, *refused = (char *)"sentinel";
    data.data = "fresh";
    check(networkfs_writefile_if_revision(a, (char *)carried, data, NULL, &created) == 0 && created && created[0],
          "networkfs_writefile_if_revision returns the revision it created");
    data.data = "other";
    check(networkfs_writefile(b, (char *)carried, data) == 0, "client B overwrites the carried document");
    data.data = "stale";
    check(networkfs_writefile_if_revision(a, (char *)carried, data, created, &refused) == 2 && refused == NULL,
          "a write at the returned revision after client B's save returns 2 and no revision");
    if (created) networkfs_free(created);
    networkfs_remove(a, (char *)carried);

    networkfs_unmount(a);
    networkfs_unmount(b);
}

// ---------------------------------------------------------------------------

// The ABI's contract is that the caller frees what it is given. Doing that a
// few thousand times is what turns a leak or a double free into a crash or an
// ASan report rather than something a consumer meets in production.
static void test_free_contract_under_repetition(void) {
    for (int i = 0; i < 2000; i++) {
        char *v = networkfs_version();
        if (!v) { check(0, "version returned NULL under repetition"); return; }
        networkfs_free(v);

        char *err = NULL;
        networkfs_mount(1, 999, "{}", &err);
        if (err) networkfs_free(err);
    }
    check(1, "2000 allocate/free cycles across the boundary");
}

// networkfs_same_revision is the comparison a consumer is told to use instead
// of strcmp (#94): a WebDAV server reports a state changed in the last second
// as W/"x" and the same state later as "x". It needs no mount.
static void test_same_revision(void) {
    check(networkfs_same_revision("\"7-a\"", "\"7-a\"") == 1, "same_revision: equal tags are the same");
    check(networkfs_same_revision("W/\"7-a\"", "\"7-a\"") == 1, "same_revision: a weak and a strong tag with one value are the same");
    check(networkfs_same_revision("\"7-a\"", "W/\"7-a\"") == 1, "same_revision: the comparison is symmetric");
    check(networkfs_same_revision("W/\"7-a\"", "\"7-b\"") == 0, "same_revision: different values differ");
    check(networkfs_same_revision("", "") == 0, "same_revision: an empty revision is never the same");
    check(networkfs_same_revision(NULL, NULL) == 0, "same_revision: NULL is an empty revision");
    check(networkfs_same_revision(NULL, "\"7-a\"") == 0, "same_revision: NULL is not the same as a tag");
}

// Built with -cover, the archive exposes a flush hook. A c-archive has no Go
// main, so the runtime never writes its counters on the way out and the
// profile would otherwise be empty.
#ifdef NETWORKFS_COVERAGE
extern int networkfs_flush_coverage(char *dir);
#endif

int main(void) {
    test_version();
    test_free_tolerates_null();
    test_drivers_are_all_linked();
    test_mount_failure_codes();
    test_openfile_on_unknown_mount();
    test_writefile_on_unknown_mount();
    test_operations_on_unknown_mount();
    test_same_revision();
    test_free_contract_under_repetition();
    test_download_fidelity();
    test_write_length_bounds();

    if (have_server()) {
        test_write_then_read_back();
        test_empty_file_round_trip();
        test_mkdir_and_rename();
        test_conditional_unsupported();
        test_conditional_webdav();
    } else {
        // A SKIP IS RIGHT ON A LAPTOP AND WRONG IN CI — see the per-driver
        // harnesses. In the job that exists because Samba is up, no SMB_HOST
        // means scripts/cabi.sh did not export one, and every success path of
        // the unified archive went untested while the job reported green.
        if (getenv("CI") != NULL || getenv("GITHUB_ACTIONS") != NULL) {
            fprintf(stderr,
                    "FAIL: SMB_HOST is unset under CI — scripts/cabi.sh's cabi_unified()\n"
                    "      owes this harness one, and every mounted test was skipped.\n");
            failures++;
        }
        printf("no SMB_HOST set; skipping the mounted tests\n");
    }

    printf("%d checks, %d failures\n", checks, failures);

    {
        const int mounted = have_server();
        const int expected = mounted ? EXPECTED_CHECKS_MOUNTED : EXPECTED_CHECKS_OFFLINE;
        if (checks != expected) {
            fprintf(stderr,
                    "FAIL: %d checks ran, %d expected %s — this harness did not finish.\n",
                    checks, expected,
                    mounted ? "with a mount config" : "without a mount config");
            failures++;
        }
    }

#ifdef NETWORKFS_COVERAGE
    {
        const char *dir = getenv("GOCOVERDIR");
        if (dir && networkfs_flush_coverage((char *)dir) != 0) {
            fprintf(stderr, "FAIL: could not write coverage counters\n");
            failures++;
        }
    }
#endif

    return failures == 0 ? 0 : 1;
}
