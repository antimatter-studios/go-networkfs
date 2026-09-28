#!/bin/sh
#
# One account, one writable directory, password authentication, sshd in the
# foreground. Everything here is a test fixture and none of it is shipped.
set -eu

user="${SFTP_USER:-testuser}"
password="${SFTP_PASSWORD:-testpass}"
root="${SFTP_ROOT:-/upload}"

adduser -D -s /sbin/nologin "$user"
echo "$user:$password" | chpasswd

# THE ROOT IS A REAL PATH, NOT A CHROOT. atmoz/sftp chrooted the account to
# its home and exposed an "upload" directory inside it, so the driver's config
# says root=/upload. Creating /upload at the actual filesystem root gives the
# same path with no ChrootDirectory to get wrong — and a chroot has ownership
# rules (the chroot itself must be root-owned and not group-writable) that are
# a silent "connection closed" when they are not met.
mkdir -p "$root"
chown "$user:$user" "$root"

# Host keys are generated per container: these sessions last minutes and the
# driver does not verify them (sftp/sftp.go uses ssh.InsecureIgnoreHostKey),
# so a key committed here would be a published private key for no gain.
ssh-keygen -A

cat > /etc/ssh/sshd_config <<CONF
Port 22
HostKey /etc/ssh/ssh_host_rsa_key
HostKey /etc/ssh/ssh_host_ed25519_key
PasswordAuthentication yes
PermitRootLogin no
UsePAM no
Subsystem sftp internal-sftp
CONF

# -D foreground, -e log to stderr: the container's log is the only place a
# refused authentication can be read from.
exec /usr/sbin/sshd -D -e
