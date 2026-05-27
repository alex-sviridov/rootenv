# Lab Image Requirements

This document describes the minimal contract a container image must satisfy to be used
as a lab asset in rootenv. The provisioner (contmgr) handles user creation and SSH key
injection — the image itself does not need to know the SSH username or key.

## Required

### 1. sshd running as PID 1

The container's entrypoint must start `sshd` in the foreground and keep it running.
The simplest correct entrypoint:

```sh
#!/bin/sh
ssh-keygen -A -q        # generate host keys if missing
exec /usr/sbin/sshd -D -e "$@"
```

The container must not exit on its own — `restartPolicy: Never` means a dead container
requires a full reprovision cycle.

### 2. sshd_config requirements

```
PermitRootLogin yes       # contmgr execs setup scripts as root
PubkeyAuthentication yes  # relay authenticates with the injected key
PasswordAuthentication no # no password logins
```

### 3. Standard Unix user tools

The following must be present and in `$PATH` (available in any standard Linux image):

- `useradd`, `groupadd`, `usermod`, `getent`
- `mkdir`, `chmod`, `chown`
- `printf`

These are used by contmgr's setup script to create the SSH user and write
`authorized_keys`.

---

## What contmgr does at provision time

After the pod reaches `Running`, contmgr execs the following as root:

1. Create the group and user (idempotent — skipped if already exist)
2. Unlock the account with `usermod -p '*'` so PAM allows pubkey auth
3. Write `~<user>/.ssh/authorized_keys` with the relay's public key
4. Run the lab's `setup` script (if defined in the asset YAML)

The image does **not** need to pre-create the SSH user. If the user already exists in
the image, steps 1–2 are skipped safely and the key is still injected.

---

## What the image does NOT need

- Any knowledge of the SSH username (`ssh_user` is defined in the lab YAML)
- Any knowledge of the SSH key (generated fresh per provision by contmgr)
- An `SSH_USERS` environment variable handler
- A custom entrypoint beyond host-key generation + sshd

---

## Testing an image locally

```sh
# Run the image
docker run --rm -d --name test-lab -p 2222:22 <your-image>

# Simulate what contmgr does
docker exec test-lab sh -c "
  getent group lab || groupadd lab
  id lab || useradd -m -s /bin/bash -g lab lab
  usermod -p '*' lab
  mkdir -p /home/lab/.ssh
  chown lab:lab /home/lab/.ssh
  chmod 700 /home/lab/.ssh
  echo 'ssh-ed25519 AAAA...' > /home/lab/.ssh/authorized_keys
  chown lab:lab /home/lab/.ssh/authorized_keys
  chmod 600 /home/lab/.ssh/authorized_keys
"

# Connect with the matching private key
ssh -p 2222 -i /path/to/private_key lab@localhost

docker stop test-lab
```

---

## Example: minimal Alpine image

```dockerfile
FROM alpine:3.21
RUN apk add --no-cache openssh-server shadow && \
    mkdir -p /run/sshd && \
    sed -i 's/#PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PubkeyAuthentication.*/PubkeyAuthentication yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
EXPOSE 22
ENTRYPOINT ["/entrypoint.sh"]
```

The `shadow` package is required on Alpine for `usermod -p` support.
