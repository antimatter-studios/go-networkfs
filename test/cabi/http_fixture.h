// A small in-process HTTP fixture for the C ABI harnesses.
//
// The real WebDAV server the harnesses mount (Apache mod_dav) answers every
// request correctly, so it cannot show what an export does when a server
// misbehaves: a body cut short of its Content-Length, or two servers whose
// writes must not cross. This fixture can. It runs on a thread inside the
// harness, listens on 127.0.0.1 on a port the kernel picks, and holds a few
// resources in memory.
//
// It speaks just enough of HTTP/1.1 and WebDAV for the driver: OPTIONS (the
// mount's connection check), GET, PUT, MOVE (with Overwrite) and DELETE. Each
// connection carries one request and is closed after the response, so no
// keep-alive state exists to get wrong.
//
// A resource can be marked truncated: GET then promises its full length in
// Content-Length, sends only part of the body and closes the connection.
//
// Everything is header-only and static, so each harness that includes it gets
// its own copy; the harnesses are single files by design.

#ifndef NETWORKFS_HTTP_FIXTURE_H
#define NETWORKFS_HTTP_FIXTURE_H

#include <arpa/inet.h>
#include <netinet/in.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/socket.h>
#include <unistd.h>

#define FIXTURE_MAX_RESOURCES 16
#define FIXTURE_MAX_PATH 256
#define FIXTURE_MAX_HEADER 16384

typedef struct {
    int used;
    char path[FIXTURE_MAX_PATH];
    char *body;
    size_t len;
    size_t truncate_to; // GET sends only this many bytes when truncated is set
    int truncated;
} fixture_resource;

typedef struct {
    int listen_fd;
    int port;
    pthread_t thread;
    pthread_mutex_t lock;
    fixture_resource resources[FIXTURE_MAX_RESOURCES];
    int puts;   // PUT requests received
    int moves;  // MOVE requests received
} http_fixture;

static fixture_resource *fixture_find(http_fixture *f, const char *path) {
    for (int i = 0; i < FIXTURE_MAX_RESOURCES; i++) {
        if (f->resources[i].used && strcmp(f->resources[i].path, path) == 0) {
            return &f->resources[i];
        }
    }
    return NULL;
}

static void fixture_drop(fixture_resource *r) {
    free(r->body);
    memset(r, 0, sizeof *r);
}

// Stores a copy of body at path, replacing what was there. Caller holds lock.
static int fixture_store_locked(http_fixture *f, const char *path, const char *body, size_t len) {
    fixture_resource *r = fixture_find(f, path);
    if (!r) {
        for (int i = 0; i < FIXTURE_MAX_RESOURCES && !r; i++) {
            if (!f->resources[i].used) r = &f->resources[i];
        }
        if (!r) return -1;
    } else {
        fixture_drop(r);
    }
    r->used = 1;
    snprintf(r->path, sizeof r->path, "%s", path);
    r->body = malloc(len ? len : 1);
    if (len) memcpy(r->body, body, len);
    r->len = len;
    return 0;
}

// Seeds a resource. truncate_to < len makes GET of it a short read.
static void fixture_put(http_fixture *f, const char *path, const char *body, size_t len,
                        int truncated, size_t truncate_to) {
    pthread_mutex_lock(&f->lock);
    if (fixture_store_locked(f, path, body, len) == 0) {
        fixture_resource *r = fixture_find(f, path);
        r->truncated = truncated;
        r->truncate_to = truncate_to;
    }
    pthread_mutex_unlock(&f->lock);
}

// Copies the body at path into buf. Returns its length, or -1 if absent.
static long fixture_get(http_fixture *f, const char *path, char *buf, size_t cap) {
    long n = -1;
    pthread_mutex_lock(&f->lock);
    fixture_resource *r = fixture_find(f, path);
    if (r) {
        n = (long)r->len;
        memcpy(buf, r->body, r->len < cap ? r->len : cap);
    }
    pthread_mutex_unlock(&f->lock);
    return n;
}

static int fixture_count(http_fixture *f, int *counter) {
    pthread_mutex_lock(&f->lock);
    int n = *counter;
    pthread_mutex_unlock(&f->lock);
    return n;
}

static void fixture_send(int fd, const char *data, size_t len) {
    while (len > 0) {
        ssize_t n = send(fd, data, len, 0);
        if (n <= 0) return;
        data += n;
        len -= (size_t)n;
    }
}

static void fixture_status(int fd, int code, const char *reason) {
    char head[256];
    int n = snprintf(head, sizeof head,
                     "HTTP/1.1 %d %s\r\nDAV: 1\r\nContent-Length: 0\r\nConnection: close\r\n\r\n",
                     code, reason);
    fixture_send(fd, head, (size_t)n);
}

// Finds a header's value in the request head. Returns 0 if absent.
static int fixture_header(const char *head, const char *name, char *out, size_t cap) {
    size_t name_len = strlen(name);
    for (const char *line = strstr(head, "\r\n"); line; line = strstr(line + 2, "\r\n")) {
        const char *p = line + 2;
        if (strncasecmp(p, name, name_len) == 0 && p[name_len] == ':') {
            p += name_len + 1;
            while (*p == ' ') p++;
            size_t n = strcspn(p, "\r\n");
            if (n >= cap) n = cap - 1;
            memcpy(out, p, n);
            out[n] = '\0';
            return 1;
        }
    }
    return 0;
}

static void fixture_serve(http_fixture *f, int fd) {
    char *head = malloc(FIXTURE_MAX_HEADER + 1);
    size_t have = 0;
    char *end = NULL;
    while (have < FIXTURE_MAX_HEADER) {
        ssize_t n = recv(fd, head + have, FIXTURE_MAX_HEADER - have, 0);
        if (n <= 0) break;
        have += (size_t)n;
        head[have] = '\0';
        if ((end = strstr(head, "\r\n\r\n")) != NULL) break;
    }
    if (!end) {
        free(head);
        return;
    }

    char method[16] = {0}, path[FIXTURE_MAX_PATH] = {0};
    sscanf(head, "%15s %255s", method, path);

    // The body: whatever followed the head, then the rest of Content-Length.
    char value[FIXTURE_MAX_PATH];
    size_t body_len = fixture_header(head, "Content-Length", value, sizeof value)
                          ? (size_t)strtoull(value, NULL, 10) : 0;
    char *body = malloc(body_len ? body_len : 1);
    size_t got = have - (size_t)(end + 4 - head);
    if (got > body_len) got = body_len;
    memcpy(body, end + 4, got);
    while (got < body_len) {
        ssize_t n = recv(fd, body + got, body_len - got, 0);
        if (n <= 0) break;
        got += (size_t)n;
    }

    pthread_mutex_lock(&f->lock);
    if (strcmp(method, "OPTIONS") == 0) {
        fixture_status(fd, 200, "OK");
    } else if (strcmp(method, "GET") == 0) {
        fixture_resource *r = fixture_find(f, path);
        if (!r) {
            fixture_status(fd, 404, "Not Found");
        } else {
            char hdr[256];
            int n = snprintf(hdr, sizeof hdr,
                             "HTTP/1.1 200 OK\r\nContent-Length: %zu\r\nConnection: close\r\n\r\n",
                             r->len);
            fixture_send(fd, hdr, (size_t)n);
            fixture_send(fd, r->body, r->truncated ? r->truncate_to : r->len);
        }
    } else if (strcmp(method, "PUT") == 0) {
        f->puts++;
        if (got == body_len && fixture_store_locked(f, path, body, body_len) == 0) {
            fixture_status(fd, 201, "Created");
        } else {
            fixture_status(fd, 500, "Internal Server Error");
        }
    } else if (strcmp(method, "MOVE") == 0) {
        f->moves++;
        char dest[FIXTURE_MAX_PATH] = {0}, overwrite[8] = "T";
        fixture_header(head, "Overwrite", overwrite, sizeof overwrite);
        const char *dest_path = NULL;
        if (fixture_header(head, "Destination", dest, sizeof dest)) {
            const char *scheme = strstr(dest, "://");
            dest_path = scheme ? strchr(scheme + 3, '/') : dest;
        }
        fixture_resource *src = fixture_find(f, path);
        fixture_resource *dst = dest_path ? fixture_find(f, dest_path) : NULL;
        if (src && dest_path && strcmp(path, dest_path) == 0) {
            fixture_status(fd, 403, "Forbidden");
        } else if (!src || !dest_path) {
            fixture_status(fd, src ? 400 : 404, src ? "Bad Request" : "Not Found");
        } else if (dst && overwrite[0] == 'F') {
            fixture_status(fd, 412, "Precondition Failed");
        } else {
            fixture_store_locked(f, dest_path, src->body, src->len);
            fixture_drop(fixture_find(f, path));
            fixture_status(fd, dst ? 204 : 201, dst ? "No Content" : "Created");
        }
    } else if (strcmp(method, "DELETE") == 0) {
        fixture_resource *r = fixture_find(f, path);
        if (r) fixture_drop(r);
        fixture_status(fd, r ? 204 : 404, r ? "No Content" : "Not Found");
    } else {
        fixture_status(fd, 405, "Method Not Allowed");
    }
    pthread_mutex_unlock(&f->lock);

    free(body);
    free(head);
}

static void *fixture_loop(void *arg) {
    http_fixture *f = arg;
    for (;;) {
        int fd = accept(f->listen_fd, NULL, NULL);
        if (fd < 0) return NULL;
        fixture_serve(f, fd);
        close(fd);
    }
}

// Starts the fixture. Returns 0 on success; f->port names the port.
static int fixture_start(http_fixture *f) {
    memset(f, 0, sizeof *f);
    pthread_mutex_init(&f->lock, NULL);
    f->listen_fd = socket(AF_INET, SOCK_STREAM, 0);
    if (f->listen_fd < 0) return -1;

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof addr);
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    addr.sin_port = 0;
    socklen_t len = sizeof addr;
    if (bind(f->listen_fd, (struct sockaddr *)&addr, sizeof addr) != 0 ||
        listen(f->listen_fd, 16) != 0 ||
        getsockname(f->listen_fd, (struct sockaddr *)&addr, &len) != 0) {
        close(f->listen_fd);
        return -1;
    }
    f->port = ntohs(addr.sin_port);
    if (pthread_create(&f->thread, NULL, fixture_loop, f) != 0) {
        close(f->listen_fd);
        return -1;
    }
    pthread_detach(f->thread);
    return 0;
}

// The mount config for this fixture, as the WebDAV driver reads it.
static void fixture_config(const http_fixture *f, char *out, size_t cap) {
    snprintf(out, cap, "{\"url\":\"http://127.0.0.1:%d\",\"user\":\"u\",\"pass\":\"p\"}", f->port);
}

#endif
