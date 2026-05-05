#!/usr/bin/env bash
set -e

# Generate host keys if missing (first boot or ephemeral container).
ssh-keygen -A -q

exec /usr/sbin/sshd -D -e "$@"
