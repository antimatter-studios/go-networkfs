# The SFTP server for the sftp driver's tests.
#
# IT REPLACES atmoz/sftp:latest, which is published for linux/amd64 only. On
# an arm64 host that image exited with "exec format error" before answering on
# port 22, and `chore servers.sh up` stopped there — so the one task that
# exercises every driver's tagged integration tests and the C ABI did not run
# at all on the architecture a macOS developer has. Issues #16 and #20.
#
# alpine + openssh, which is the .github/docker/samba pattern and publishes
# for every architecture this family is developed on.
FROM alpine:3.21

RUN apk add --no-cache openssh-server openssh-sftp-server

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

ENV SFTP_USER=testuser
ENV SFTP_PASSWORD=testpass
ENV SFTP_ROOT=/upload

EXPOSE 22

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
