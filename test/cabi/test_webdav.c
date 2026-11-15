// A consumer's-eye test of libwebdav.a.
//
// This links the archive that actually ships and calls its C entry points the
// way a consumer does. It reaches three functions no Go test can:
// webdav_openfile, webdav_writefile and setOutBytes take a ByteSlice or a
// size_t, and a Go test file may not `import "C"`, so it cannot name either
// type.
//
// It also checks what a Go test structurally cannot see: that the generated
// header matches the implementation, that every symbol was exported, and that
// the archive links at all.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "libwebdav.h"
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
// there. Without CABI_CONFIG the mounted round trip does not run and 50
// checks is the honest total; with it, all 89 must.
//
// EQUALITY, NOT "AT LEAST". A check added without moving the number is an
// unreviewed change to what this file claims to prove, and it is exactly as
// interesting as one removed. Raise it in the same commit that adds the check.
#define EXPECTED_CHECKS_OFFLINE 50
#define EXPECTED_CHECKS_MOUNTED 89

static void check(int cond, const char *what) {
    checks++;
    if (!cond) {
        failures++;
        fprintf(stderr, "FAIL: %s\n", what);
    }
}

static void test_version(void) {
    char *v = webdav_version();
    check(v != NULL, "version returns a string");
    if (v) {
        check(strlen(v) > 0, "version is not empty");
        webdav_free(v);
    }
}

static void test_free_tolerates_null(void) {
    webdav_free(NULL);
    check(1, "free(NULL) does not crash");
}

static void test_mount_rejects_bad_input(void) {
    check(webdav_mount(1, "{not json") == -1, "malformed JSON returns -1");
    check(webdav_mount(1, "{}") != 0, "an empty config is refused");
}

// Reachable only from C: ByteSlice cannot be named in a Go test file.
static void test_openfile_unmounted(void) {
    ByteSlice out;
    out.data = NULL;
    out.len = 0;

    check(webdav_openfile(99, "/nope", &out) != 0, "openfile while unmounted fails");
    check(out.data == NULL, "a failed openfile leaves no buffer to free");
    check(out.len == 0, "a failed openfile reports zero length");
    if (out.data) webdav_free(out.data);
}

// Also reachable only from C.
static void test_writefile_unmounted(void) {
    const char *payload = "some bytes";
    ByteSlice data;
    data.data = (char *)payload;
    data.len = strlen(payload);
    check(webdav_writefile(99, "/nope", data) != 0, "writefile while unmounted fails");

    ByteSlice empty;
    empty.data = NULL;
    empty.len = 0;
    check(webdav_writefile(99, "/nope", empty) != 0,
          "writefile with an empty slice fails cleanly rather than dereferencing it");
}

static void test_operations_unmounted(void) {
    char *out = NULL;

    check(webdav_stat(99, "/p", &out) != 0, "stat while unmounted fails");
    if (out) { webdav_free(out); out = NULL; }

    check(webdav_listdir(99, "/p", &out) != 0, "listdir while unmounted fails");
    if (out) { webdav_free(out); out = NULL; }

    check(webdav_mkdir(99, "/p") != 0, "mkdir while unmounted fails");
    check(webdav_remove(99, "/p") != 0, "remove while unmounted fails");
    check(webdav_rename(99, "/a", "/b") != 0, "rename while unmounted fails");

    ByteSlice data;
    data.data = "x";
    data.len = 1;
    check(webdav_writefile_if(99, "/p", data, "\"r\"") == 1, "writefile_if while unmounted returns 1");
    check(webdav_remove_if(99, "/p", "\"r\"") == 1, "remove_if while unmounted returns 1");
    check(webdav_rename_if(99, "/a", "/b", "\"r\"", NULL) == 1, "rename_if while unmounted returns 1");

    // Every mount ID names its own driver, so an ID that was never mounted
    // names nothing, and unmounting it is an error (#66).
    check(webdav_unmount(99) != 0, "unmount of a never-mounted ID fails");
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
    check(webdav_openfile(9998, "/nope", &out) != 0, "openfile on an unknown mount fails");
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
    if (webdav_mount(id, cfg) != 0) {
        check(0, "mount the HTTP fixture");
        return;
    }
    check(1, "mount the HTTP fixture");

    out.data = &sentinel;
    out.len = 99;
    check(webdav_openfile(id, "/truncated", &out) != 0, "a download cut short of its Content-Length fails");
    check(out.data == NULL && out.len == 0, "a truncated download hands back no buffer");
    if (out.data && out.data != &sentinel) webdav_free(out.data);

    out.data = NULL;
    out.len = 0;
    check(webdav_openfile(id, "/whole", &out) == 0, "a whole download succeeds");
    check(out.len == 5 && out.data != NULL && memcmp(out.data, "hello", 5) == 0,
          "a whole download returns every byte");
    if (out.data) webdav_free(out.data);

    out.data = &sentinel;
    out.len = 99;
    check(webdav_openfile(id, "/empty", &out) == 0, "an empty download succeeds");
    check(out.data == NULL && out.len == 0, "an empty download hands back no buffer and zero length");
    if (out.data && out.data != &sentinel) webdav_free(out.data);

    webdav_unmount(id);
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
    if (webdav_mount(id, cfg) != 0) {
        check(0, "mount the HTTP fixture");
        return;
    }
    check(1, "mount the HTTP fixture");

    char body[64];
    static char one_byte = 'x';
    ByteSlice data;

    data.data = &one_byte;
    data.len = (size_t)1 << 32;
    check(webdav_writefile(id, "/target", data) != 0, "a 4 GiB length over a one-byte buffer fails");
    check(fixture_get(&fx, "/target", body, sizeof body) == 8 && memcmp(body, "original", 8) == 0 &&
              fixture_count(&fx, &fx.puts) == 0 && fixture_count(&fx, &fx.moves) == 0,
          "an oversized write sends nothing and leaves the target unchanged");

    data.data = NULL;
    data.len = 5;
    check(webdav_writefile(id, "/target", data) != 0, "a NULL buffer with a nonzero length fails");
    check(fixture_get(&fx, "/target", body, sizeof body) == 8 && memcmp(body, "original", 8) == 0 &&
              fixture_count(&fx, &fx.puts) == 0 && fixture_count(&fx, &fx.moves) == 0,
          "a NULL buffer sends nothing and leaves the target unchanged");

    data.data = &one_byte;
    data.len = (size_t)1 << 32;
    check(webdav_writefile_if(id, "/target", data, NULL) == 1, "a conditional write of a 4 GiB length fails");
    check(fixture_get(&fx, "/target", body, sizeof body) == 8 && memcmp(body, "original", 8) == 0 &&
              fixture_count(&fx, &fx.puts) == 0 && fixture_count(&fx, &fx.moves) == 0,
          "an oversized conditional write sends nothing and leaves the target unchanged");

    data.data = "replaced";
    data.len = 8;
    check(webdav_writefile(id, "/target", data) == 0, "an ordinary write succeeds");
    check(fixture_get(&fx, "/target", body, sizeof body) == 8 && memcmp(body, "replaced", 8) == 0,
          "an ordinary write replaces the target");

    data.data = NULL;
    data.len = 0;
    check(webdav_writefile(id, "/target", data) == 0, "an empty write succeeds");
    check(fixture_get(&fx, "/target", body, sizeof body) == 0, "an empty write empties the target");

    webdav_unmount(id);
}

// Each mount ID is its own connection (#66).
//
// The archive kept one driver for every ID, so the last mount won: a write
// through ID 11 landed on ID 12's server, and unmounting ID 11 disconnected
// ID 12. Two fixtures stand in for two servers, each holding its own /save.
static void test_mounts_are_isolated(void) {
    http_fixture a, b;
    if (fixture_start(&a) != 0 || fixture_start(&b) != 0) {
        check(0, "the HTTP fixtures start");
        return;
    }
    fixture_put(&a, "/save", "server A", 8, 0, 0);
    fixture_put(&b, "/save", "server B", 8, 0, 0);

    char cfg_a[256], cfg_b[256], body[64];
    fixture_config(&a, cfg_a, sizeof cfg_a);
    fixture_config(&b, cfg_b, sizeof cfg_b);
    check(webdav_mount(11, cfg_a) == 0, "mount server A as ID 11");
    check(webdav_mount(12, cfg_b) == 0, "mount server B as ID 12");

    ByteSlice data;
    data.data = "X";
    data.len = 1;
    check(webdav_writefile(11, "/save", data) == 0, "a write through ID 11 succeeds");
    check(fixture_get(&a, "/save", body, sizeof body) == 1 && body[0] == 'X',
          "the write through ID 11 reaches server A");
    check(fixture_get(&b, "/save", body, sizeof body) == 8 && memcmp(body, "server B", 8) == 0,
          "the write through ID 11 leaves server B unchanged");

    check(webdav_unmount(11) == 0, "unmount ID 11");

    ByteSlice out;
    out.data = NULL;
    out.len = 0;
    check(webdav_openfile(12, "/save", &out) == 0, "ID 12 still reads after ID 11 is unmounted");
    check(out.len == 8 && out.data != NULL && memcmp(out.data, "server B", 8) == 0,
          "ID 12 reads server B's file");
    if (out.data) webdav_free(out.data);

    out.data = NULL;
    out.len = 0;
    check(webdav_openfile(11, "/save", &out) != 0, "an unmounted ID no longer reads");
    if (out.data) webdav_free(out.data);

    check(webdav_unmount(12) == 0, "unmount ID 12");
}

static void test_conditional_two_clients(const char *cfg);

// Mounted round trip.
//
// Everything above only reaches the failure paths. The success paths are where
// the interesting marshalling lives: setOutBytes hands a buffer and a length
// back across the boundary and is reached only when a read succeeds.
//
// This runs when CABI_CONFIG holds a config JSON document for this driver,
// which `chore test:cabi` supplies for the drivers that have a server.
static void test_mounted_round_trip(void) {
    const char *cfg = getenv("CABI_CONFIG");
    if (!cfg || cfg[0] == '\0') {
        // A SKIP IS RIGHT ON A LAPTOP AND WRONG IN CI. With no servers up
        // there is nothing to mount and saying so is honest. In the job that
        // exists BECAUSE the servers are up, an empty config means
        // scripts/cabi.sh did not supply one — and the whole success path of
        // this driver then goes untested while the job reports green.
        if (getenv("CI") != NULL || getenv("GITHUB_ACTIONS") != NULL) {
            fprintf(stderr,
                    "FAIL: CABI_CONFIG is empty under CI — scripts/cabi.sh's config_for()\n"
                    "      owes webdav one, and its whole mounted surface did not run.\n");
            failures++;
        }
        printf("webdav: no CABI_CONFIG; skipping the mounted tests\n");
        return;
    }

    const int id = 1;
    if (webdav_mount(id, (char *)cfg) != 0) {
        check(0, "mount against the test server");
        return;
    }
    check(1, "mount against the test server");

    const char *path = "/cabi-webdav-roundtrip.txt";
    const char *payload = "written through the C ABI";

    ByteSlice data;
    data.data = (char *)payload;
    data.len = strlen(payload);
    check(webdav_writefile(id, (char *)path, data) == 0, "writefile succeeds");

    ByteSlice out;
    out.data = NULL;
    out.len = 0;
    check(webdav_openfile(id, (char *)path, &out) == 0, "openfile succeeds");
    check(out.data != NULL, "openfile hands back a buffer");
    check(out.len == strlen(payload), "openfile reports the right length");
    if (out.data && out.len == strlen(payload)) {
        check(memcmp(out.data, payload, out.len) == 0, "the bytes survive the round trip");
    }
    if (out.data) webdav_free(out.data);

    char *info = NULL;
    check(webdav_stat(id, (char *)path, &info) == 0, "stat succeeds");
    if (info) {
        check(strstr(info, "cabi-webdav-roundtrip.txt") != NULL, "stat names the file");
        webdav_free(info);
    }

    char *listing = NULL;
    check(webdav_listdir(id, "/", &listing) == 0, "listdir succeeds");
    if (listing) webdav_free(listing);

    check(webdav_remove(id, (char *)path) == 0, "remove succeeds");

    // mkdir over a regular file fails and leaves the file alone; mkdir of a
    // directory that already exists succeeds (#67).
    const char *collision = "/cabi-webdav-collision";
    data.data = "keep me";
    data.len = 7;
    check(webdav_writefile(id, (char *)collision, data) == 0, "writefile of the collision file succeeds");
    check(webdav_mkdir(id, (char *)collision) != 0, "mkdir over a regular file fails");
    out.data = NULL;
    out.len = 0;
    check(webdav_openfile(id, (char *)collision, &out) == 0, "the collision file still reads");
    check(out.len == 7 && out.data != NULL && memcmp(out.data, "keep me", 7) == 0,
          "mkdir left the collision file's contents alone");
    if (out.data) webdav_free(out.data);
    check(webdav_remove(id, (char *)collision) == 0, "remove of the collision file succeeds");

    check(webdav_mkdir(id, "/cabi-webdav-dir") == 0, "mkdir of a new directory succeeds");
    check(webdav_mkdir(id, "/cabi-webdav-dir") == 0, "mkdir of an existing directory succeeds");
    check(webdav_remove(id, "/cabi-webdav-dir") == 0, "remove of the directory succeeds");

    check(webdav_unmount(id) == 0, "unmount succeeds");

    test_conditional_two_clients(cfg);
}


// Reads path through mount id into buf; returns its length, or -1.
static long read_back(int id, const char *path, char *buf, size_t cap) {
    ByteSlice out;
    out.data = NULL;
    out.len = 0;
    if (webdav_openfile(id, (char *)path, &out) != 0) return -1;
    long n = -1;
    if (out.len < cap) {
        memcpy(buf, out.data, out.len);
        n = (long)out.len;
    }
    if (out.data) webdav_free(out.data);
    return n;
}

// The revision stat reports for path through mount id, or "" on failure.
static void observe(int id, const char *path, char *rev, size_t cap) {
    char *info = NULL;
    rev[0] = '\0';
    if (webdav_stat(id, (char *)path, &info) == 0 && info) {
        if (revision_from_json(info, rev, cap) < 0) rev[0] = '\0';
    }
    if (info) webdav_free(info);
}

static int write_bytes(int id, const char *path, const char *text) {
    ByteSlice data;
    data.data = (char *)text;
    data.len = strlen(text);
    return webdav_writefile(id, (char *)path, data);
}

static int write_if(int id, const char *path, const char *text, const char *rev) {
    ByteSlice data;
    data.data = (char *)text;
    data.len = strlen(text);
    return webdav_writefile_if(id, (char *)path, data, (char *)rev);
}

// Conditional mutations through the C ABI, two clients on one server (#70).
//
// Client A (ID 21) observes a revision; client B (ID 22) changes the file;
// A's conditional mutation at the old revision returns 2 and leaves B's
// change in place. At the current revision it returns 0.
static void test_conditional_two_clients(const char *cfg) {
    const int a = 21, b = 22;
    if (webdav_mount(a, (char *)cfg) != 0 || webdav_mount(b, (char *)cfg) != 0) {
        check(0, "mount two clients against the test server");
        return;
    }
    check(1, "mount two clients against the test server");

    const char *doc = "/cabi-webdav-conditional.txt";
    const char *moved = "/cabi-webdav-conditional-moved.txt";
    const char *fresh = "/cabi-webdav-conditional-new.txt";
    char rev[256], stale[256], body[64];

    check(write_bytes(a, doc, "version A") == 0, "client A writes the document");
    observe(a, doc, stale, sizeof stale);
    check(stale[0] != '\0', "stat reports a revision");

    check(write_bytes(b, doc, "version B") == 0, "client B overwrites it, same size");
    check(write_if(a, doc, "stale save", stale) == 2, "a write at a stale revision returns 2");
    check(read_back(a, doc, body, sizeof body) == 9 && memcmp(body, "version B", 9) == 0,
          "the refused write left client B's contents");

    observe(a, doc, rev, sizeof rev);
    check(write_if(a, doc, "fresh save", rev) == 0, "a write at the current revision returns 0");
    check(read_back(b, doc, body, sizeof body) == 10 && memcmp(body, "fresh save", 10) == 0,
          "client B reads the conditional write");

    check(write_if(a, fresh, "created", NULL) == 0, "a write with a NULL revision creates a new file");
    check(write_if(b, fresh, "clobbered", "") == 2, "a write with an empty revision over a file returns 2");
    check(read_back(a, fresh, body, sizeof body) == 7 && memcmp(body, "created", 7) == 0,
          "the refused create left the file alone");

    check(webdav_rename_if(a, (char *)doc, (char *)moved, stale, NULL) == 2,
          "a rename at a stale revision returns 2");
    check(webdav_rename_if(a, (char *)doc, (char *)fresh, rev, NULL) == 2,
          "a rename onto an existing file with no destination revision returns 2");
    observe(a, doc, rev, sizeof rev);
    check(webdav_rename_if(a, (char *)doc, (char *)moved, rev, NULL) == 0,
          "a rename at the current revision returns 0");
    check(read_back(b, moved, body, sizeof body) == 10 && read_back(b, doc, body, sizeof body) == -1,
          "the rename moved the document");

    check(webdav_remove_if(a, (char *)moved, stale) == 2, "a remove at a stale revision returns 2");
    check(webdav_remove_if(a, (char *)moved, NULL) == 1, "a remove with no revision returns 1");
    observe(a, moved, rev, sizeof rev);
    check(webdav_remove_if(a, (char *)moved, rev) == 0, "a remove at the current revision returns 0");
    check(read_back(b, moved, body, sizeof body) == -1, "the remove deleted the document");

    webdav_remove(a, (char *)fresh);
    check(webdav_unmount(a) == 0 && webdav_unmount(b) == 0, "unmount both clients");
}

// The contract is that the caller frees what it is given. Repeating it is what
// turns a leak or a double free into an ASan report rather than something a
// consumer meets in production.
static void test_free_contract_under_repetition(void) {
    for (int i = 0; i < 2000; i++) {
        char *v = webdav_version();
        if (!v) { check(0, "version returned NULL under repetition"); return; }
        webdav_free(v);
    }
    check(1, "2000 allocate/free cycles across the boundary");
}

// Built with -cover, the archive exposes a flush hook. A c-archive has no Go
// main, so the runtime never writes its counters on the way out.
#ifdef NETWORKFS_COVERAGE
extern int webdav_flush_coverage(char *dir);
#endif

int main(void) {
    test_version();
    test_free_tolerates_null();
    test_mount_rejects_bad_input();
    test_openfile_unmounted();
    test_writefile_unmounted();
    test_operations_unmounted();
    test_free_contract_under_repetition();
    test_download_fidelity();
    test_write_length_bounds();
    test_mounts_are_isolated();
    test_mounted_round_trip();

    printf("webdav: %d checks, %d failures\n", checks, failures);

    {
        const char *config = getenv("CABI_CONFIG");
        const int mounted = (config != NULL && config[0] != '\0');
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
        if (dir && webdav_flush_coverage((char *)dir) != 0) {
            fprintf(stderr, "FAIL: could not write coverage counters\n");
            failures++;
        }
    }
#endif

    return failures == 0 ? 0 : 1;
}
