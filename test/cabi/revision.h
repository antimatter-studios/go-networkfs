// Reads the "revision" member out of a FileInfo JSON document from a stat or
// listdir export, so a harness can hand it back to a conditional export
// exactly as a consumer would.
//
// It understands the escapes Go's encoding/json writes inside a string that
// an entity tag can hold -- \" and \\ -- and refuses anything else rather
// than guess. Header-only and static, like http_fixture.h.

#ifndef NETWORKFS_REVISION_H
#define NETWORKFS_REVISION_H

#include <stddef.h>
#include <string.h>

// Copies the first "revision" string in json into out. Returns its length, or
// -1 when there is none, it does not fit, or it holds an escape not handled.
static int revision_from_json(const char *json, char *out, size_t cap) {
    const char *key = "\"revision\":\"";
    const char *p = json ? strstr(json, key) : NULL;
    if (!p || cap == 0) return -1;
    p += strlen(key);
    size_t n = 0;
    for (; *p && *p != '"'; p++) {
        char c = *p;
        if (c == '\\') {
            p++;
            if (*p != '"' && *p != '\\') return -1;
            c = *p;
        }
        if (n + 1 >= cap) return -1;
        out[n++] = c;
    }
    if (*p != '"') return -1;
    out[n] = '\0';
    return (int)n;
}

#endif
