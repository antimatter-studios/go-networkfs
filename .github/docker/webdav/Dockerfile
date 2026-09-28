# The WebDAV server for the webdav driver's tests, built from test/webdavd.
#
# IT REPLACES bytemark/webdav:latest, which is published for linux/amd64 only.
# On an arm64 host that image exited with "exec format error" before serving a
# single request, so `chore test` could not complete at all — and the consumer
# of these archives is a macOS application, so an Apple Silicon workstation is
# exactly where a developer runs this. Issues #16 and #20.
#
# Built here rather than pulled, the way .github/docker/samba and
# .github/docker/mockapi already are: multi-arch by construction, pinned by
# this repository's own commit rather than by somebody else's floating tag,
# and unable to drift from what the driver expects.
FROM golang:1.26-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/webdavd ./test/webdavd

# Distroless, as mockapi is. WORKDIR /data is what creates the storage
# directory: nothing in this image can mkdir at runtime, because the binary
# runs as nonroot and owns nothing else — the same trap sss3 fell into when it
# was pointed at an unwritable path and exited 1 looking ready (#13).
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/webdavd /webdavd
WORKDIR /data
ENV WEBDAVD_ADDR=:80
ENV WEBDAVD_ROOT=/data
EXPOSE 80
ENTRYPOINT ["/webdavd"]
