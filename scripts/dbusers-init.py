#!/usr/bin/env python3
"""
Create the PocketBase superuser and service accounts from k8s secrets.

Usage:
    python3 scripts/dbusers-init.py [--backend-pod <pod-name>] [--namespace <ns>]

Steps:
    1. Create superuser via `kubectl exec ... pocketbase superuser upsert`
    2. Authenticate as superuser to get admin token
    3. Create svc_contmgr and svc_relay in the `users` collection
"""

import argparse
import base64
import json
import subprocess
import sys
import urllib.error
import urllib.request


NAMESPACE = "rootenv-infra"
BACKEND_SVC = "http://localhost:8080"

SVC_ACCOUNTS = [
    {"secret": "contmgr-secrets", "email_key": "CONTMGR_BACKEND_USERNAME", "password_key": "CONTMGR_BACKEND_PASSWORD", role: "contmgr"},
    {"secret": "relay-secrets",   "email_key": "RELAY_BACKEND_USERNAME",   "password_key": "RELAY_BACKEND_PASSWORD", role: "relay"},
]


def run(cmd, check=True):
    result = subprocess.run(cmd, capture_output=True, text=True)
    if check and result.returncode != 0:
        print(f"ERROR: {' '.join(cmd)}\n{result.stderr}", file=sys.stderr)
        sys.exit(1)
    return result.stdout.strip()


def get_secret(name, key, namespace):
    raw = run(["kubectl", "get", "secret", name, "-n", namespace,
               "-o", f"jsonpath={{.data.{key}}}"])
    return base64.b64decode(raw).decode()


def api(method, path, token=None, body=None):
    url = f"{BACKEND_SVC}/api/{path}"
    data = json.dumps(body).encode() if body else None
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        return json.loads(e.read())


def get_admin_token(email, password):
    resp = api("POST", "collections/_superusers/auth-with-password",
               body={"identity": email, "password": password})
    token = resp.get("token")
    if not token:
        print(f"ERROR: could not authenticate as superuser: {resp}", file=sys.stderr)
        sys.exit(1)
    return token


def create_user(email, password, token, role=None):
    resp = api("POST", "collections/users/records", token=token, body={
        "email": email,
        "password": password,
        "passwordConfirm": password,
        "svc_role": role,
        "emailVisibility": True,
    })
    if resp.get("id"):
        print(f"  created {email}")
    elif resp.get("code") == 400 and "already" in str(resp).lower():
        print(f"  {email} already exists, skipping")
    else:
        print(f"  WARNING: unexpected response for {email}: {resp}")


def find_backend_pod(namespace):
    out = run(["kubectl", "get", "pods", "-n", namespace,
               "-l", "app=backend",
               "-o", "jsonpath={.items[0].metadata.name}"])
    if not out:
        print("ERROR: no backend pod found", file=sys.stderr)
        sys.exit(1)
    return out


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--namespace", default=NAMESPACE)
    parser.add_argument("--env-file", default="scripts/.env")
    args = parser.parse_args()

    # Read superuser credentials from .env
    env = {}
    try:
        with open(args.env_file) as f:
            for line in f:
                line = line.strip()
                if line and not line.startswith("#") and "=" in line:
                    k, v = line.split("=", 1)
                    env[k.strip()] = v.strip()
    except FileNotFoundError:
        print(f"ERROR: {args.env_file} not found", file=sys.stderr)
        sys.exit(1)

    admin_email = env.get("POCKETBASE_ADMIN_EMAIL")
    admin_password = env.get("POCKETBASE_ADMIN_PASSWORD")
    backend_url = env.get("POCKETBASE_URL", "http://localhost:8080/api/").rstrip("/")
    # POCKETBASE_URL includes /api/ — strip it; api() adds /api/ itself
    global BACKEND_SVC
    BACKEND_SVC = backend_url.removesuffix("/api")

    if not admin_email or not admin_password:
        print(f"ERROR: POCKETBASE_ADMIN_EMAIL / POCKETBASE_ADMIN_PASSWORD missing in {args.env_file}",
              file=sys.stderr)
        sys.exit(1)

    # Step 1: create superuser via kubectl exec
    pod = find_backend_pod(args.namespace)
    print(f"[1/3] Creating superuser {admin_email} via kubectl exec ({pod})...")
    out = run(["kubectl", "exec", pod, "-n", args.namespace, "--",
               "/app/pocketbase", "superuser", "upsert", admin_email, admin_password])
    print(f"  {out or 'ok'}")

    # Step 2: get admin token
    print(f"[2/3] Authenticating as superuser...")
    token = get_admin_token(admin_email, admin_password)
    print(f"  ok")

    # Step 3: create service accounts
    print(f"[3/3] Creating service accounts...")
    for svc in SVC_ACCOUNTS:
        email = get_secret(svc["secret"], svc["email_key"], args.namespace)
        password = get_secret(svc["secret"], svc["password_key"], args.namespace)
        create_user(email, password, token, role=svc.get("role"))

    print("\nDone.")


if __name__ == "__main__":
    main()
