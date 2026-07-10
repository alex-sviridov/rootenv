#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:?usage: test.sh <image>}"
CONTAINER="lab-test-$$"

cleanup() {
  docker rm -f "$CONTAINER" 2>/dev/null || true
}
trap cleanup EXIT

echo "==> Starting container"
docker run -d --name "$CONTAINER" "$IMAGE" sleep infinity

echo "==> Verifying default exec user is lab (non-root)"
RESULT=$(docker exec "$CONTAINER" whoami)
[ "$RESULT" = "lab" ] || { echo "FAIL: expected 'lab', got '$RESULT'"; exit 1; }
echo "PASS: exec lands as lab"

echo "==> Verifying lab has no root/sudo escalation"
docker exec "$CONTAINER" sh -c 'command -v sudo' >/dev/null 2>&1 && { echo "FAIL: sudo present"; exit 1; }
echo "PASS: no sudo binary"

echo "==> Verifying seeded DataStream scenario is present"
docker exec "$CONTAINER" test -f /app/datastream/config.yaml || { echo "FAIL: config.yaml missing"; exit 1; }
docker exec "$CONTAINER" test -f /var/log/datastream/access.log || { echo "FAIL: access.log missing"; exit 1; }
docker exec "$CONTAINER" test -f /var/lib/datastream/events.db || { echo "FAIL: events.db missing"; exit 1; }
echo "PASS: seeded files present"

echo "==> Verifying lab cannot write into datastream-owned paths"
docker exec "$CONTAINER" sh -c 'touch /app/datastream/should-fail' 2>/dev/null \
  && { echo "FAIL: lab could write to /app/datastream"; exit 1; }
echo "PASS: /app/datastream not writable by lab"

echo "==> Verifying lab can write to its own home"
docker exec "$CONTAINER" sh -c 'echo ok > /home/lab/reports/test.txt && cat /home/lab/reports/test.txt' | grep -q ok \
  || { echo "FAIL: lab cannot write to /home/lab/reports"; exit 1; }
echo "PASS: lab can write to /home/lab/reports"
