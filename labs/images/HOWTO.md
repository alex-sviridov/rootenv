# Lab Image Requirements

This document describes the minimal contract a container image must satisfy to be used
as a lab asset in rootenv. The provisioner (contmgr) handles user creation and setup
via the Kubernetes exec API — no SSH access to the container is required for provisioning.

## Required

### 1. Long-running process as PID 1

The container must not exit on its own — `restartPolicy: Never` means a dead container
requires a full reprovision cycle. Run whatever the lab needs (sshd, a custom daemon,
etc.) in the foreground as PID 1.

### 2. Root as the container user

contmgr runs setup scripts via `kubectl exec` (Kubernetes API, not SSH). The exec runs
as the container's default user, which must be root so the setup script can create
system users, write to `/etc/`, and manipulate file ownership.

### 3. Standard Unix user tools (when `ssh_user` is set)

If the lab YAML defines `ssh_user`, the following must be present and in `$PATH`:

- `useradd`, `groupadd`, `usermod`, `getent`
- `mkdir`, `chmod`, `chown`
- `printf`

These are used by contmgr's SSH setup script to create the user and write
`authorized_keys`.

---

## SSH support (optional)

SSH is only needed when the lab YAML specifies `ssh_user`. In that case the image must
also run `sshd`.

### sshd entrypoint

```sh
#!/bin/sh
ssh-keygen -A -q        # generate host keys if missing
exec /usr/sbin/sshd -D -e "$@"
```

### sshd_config requirements

```
PubkeyAuthentication yes  # relay authenticates with the injected key
PasswordAuthentication no # no password logins
```

`PermitRootLogin` is **not** required — contmgr injects keys and runs setup scripts
via `kubectl exec`, not SSH.

---

## What contmgr does at provision time

All steps run via `kubectl exec` (Kubernetes API) against the running pod, as root.

When `ssh_user` is set in the lab YAML:

1. Create the group and user (idempotent — skipped if already exist)
2. Unlock the account with `usermod -p '*'` so PAM allows pubkey auth
3. Write `~<user>/.ssh/authorized_keys` with the relay's public key

Then, if a `setup` script is defined in the asset YAML:

4. Run the setup script

The image does **not** need to pre-create the SSH user. If the user already exists,
steps 1–2 are skipped safely and the key is still injected.

---

## What the image does NOT need

- Any knowledge of the SSH username (`ssh_user` is defined in the lab YAML)
- Any knowledge of the SSH key (generated fresh per provision by contmgr)
- An `SSH_USERS` environment variable handler
- `PermitRootLogin yes` — provisioning never goes through SSH

---

## Testing an image

Every image directory must contain a `test.sh` script. CI runs it automatically after
the Docker build; it also works locally.

### Running locally

```sh
docker build -t my-image labs/images/<image-name>
bash labs/images/<image-name>/test.sh my-image
```

### What the test script must do

The script receives the image tag as `$1` and must exit non-zero on any failure.
It should simulate the full contmgr provision flow using `docker exec` — the same
mechanism as `kubectl exec` in production:

1. Start the container (`docker run -d`)
2. Wait for the service to be ready (e.g. poll until sshd listens on `:22`)
3. Simulate user creation and key injection via `docker exec` as root
4. Verify the expected access method works (SSH, HTTP, etc.)
5. Clean up the container on exit (use a `trap cleanup EXIT`)

### Writing a test script

A minimal template for an SSH image:

```sh
#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:?usage: test.sh <image>}"
CONTAINER="lab-test-$$"
TMPDIR=$(mktemp -d)

cleanup() {
  docker rm -f "$CONTAINER" 2>/dev/null || true
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

docker run -d --name "$CONTAINER" "$IMAGE"

# Wait for sshd
for i in $(seq 1 20); do
  docker exec "$CONTAINER" sh -c 'ss -tlnp | grep -q ":22 "' 2>/dev/null && break
  [ "$i" -eq 20 ] && { echo "FAIL: sshd did not start"; exit 1; }
  sleep 1
done

# Generate a throwaway keypair
ssh-keygen -t ed25519 -f "$TMPDIR/key" -N "" -q

# Simulate contmgr exec: create user + inject key
docker exec "$CONTAINER" sh -c "
  getent group testlab || groupadd testlab
  id testlab 2>/dev/null || useradd -m -s /bin/bash -g testlab testlab
  usermod -p '*' testlab
  mkdir -p /home/testlab/.ssh
  chown testlab:testlab /home/testlab/.ssh
  chmod 700 /home/testlab/.ssh
  touch /home/testlab/.ssh/authorized_keys
  chown testlab:testlab /home/testlab/.ssh/authorized_keys
  chmod 600 /home/testlab/.ssh/authorized_keys
"
docker cp "$TMPDIR/key.pub" "$CONTAINER:/home/testlab/.ssh/authorized_keys"
docker exec "$CONTAINER" chown testlab:testlab /home/testlab/.ssh/authorized_keys

# SSH smoke test
IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$CONTAINER")
RESULT=$(ssh \
  -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 \
  -i "$TMPDIR/key" "testlab@$IP" "id -un")
[ "$RESULT" = "testlab" ] || { echo "FAIL: unexpected output: $RESULT"; exit 1; }
echo "PASS"
```

Non-SSH images follow the same pattern — replace the wait loop and smoke test with
whatever access method the image provides.

### CI integration

The `integration-test` job in `.github/workflows/lab-images.yml` runs `test.sh`
for every image in the matrix. It runs after the build succeeds and blocks
`docker-push` — nothing is pushed to the registry unless the test passes.

To add a new image to CI, add its name to the matrix in the `setup` job and make
sure its directory contains a `test.sh`.

---

## Example: minimal Alpine image with SSH

```dockerfile
FROM alpine:3.21
RUN apk add --no-cache openssh-server shadow && \
    mkdir -p /run/sshd && \
    sed -i 's/#PubkeyAuthentication.*/PubkeyAuthentication yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
EXPOSE 22
ENTRYPOINT ["/entrypoint.sh"]
```

The `shadow` package is required on Alpine for `usermod -p` support.
