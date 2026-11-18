# The mock API server, built from test/mockapi.
#
# Dropbox, Google Drive and OneDrive cannot be run locally the way Samba or
# stupid-simple-s3 can. This stands in for all three so the drivers that use
# them can be tested without any connected service — including from the C ABI
# harnesses, which are separate programs and cannot reach an in-process fake.
#
# THE BINARY IS COMPILED BEFORE THIS BUILD, by compile_mockapi in
# scripts/servers.sh, and is the whole build context. A compile stage here
# starts with an empty Go cache on every runner, so each run compiled the
# standard library again; outside the build it uses the cache CI restores.
FROM gcr.io/distroless/static-debian12
COPY mockapi /mockapi
ENV MOCKAPI_ADDR=:8081
EXPOSE 8081
ENTRYPOINT ["/mockapi"]
