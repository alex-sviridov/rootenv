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

echo "==> Starting container"
docker run -d --name "$CONTAINER" "$IMAGE"

echo "==> Waiting for sshd on :22"
for i in $(seq 1 20); do
  docker exec "$CONTAINER" sh -c 'ss -tlnp | grep -q ":22 "' 2>/dev/null && break
  [ "$i" -eq 20 ] && { echo "FAIL: sshd did not start"; exit 1; }
  sleep 1
done

echo "==> Generating test keypair"
ssh-keygen -t ed25519 -f "$TMPDIR/key" -N "" -q

echo "==> Simulating contmgr exec (user creation + key injection)"
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

echo "==> SSH smoke test"
IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$CONTAINER")
RESULT=$(ssh \
  -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null \
  -o ConnectTimeout=10 \
  -i "$TMPDIR/key" \
  "testlab@$IP" \
  "id -un")

[ "$RESULT" = "testlab" ] || { echo "FAIL: expected 'testlab', got '$RESULT'"; exit 1; }
echo "PASS: SSH login as testlab succeeded"

echo "==> Verifying root SSH is disabled"
! ssh \
  -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null \
  -o ConnectTimeout=5 \
  -o BatchMode=yes \
  -i "$TMPDIR/key" \
  "root@$IP" \
  "echo should-not-reach" 2>/dev/null \
  || { echo "FAIL: root SSH login succeeded (should be denied)"; exit 1; }
echo "PASS: root SSH login correctly denied"
