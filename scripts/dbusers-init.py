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
import os
import ssl
import subprocess
import sys
import urllib.error
import urllib.request


NAMESPACE = "rootenv-infra"
DEFAULT_BACKEND_URL = "http://localhost:8080/api/"

SVC_ACCOUNTS = [
    {
        "secret": "attempt-controller-secrets", 
        "email_key": "ATTEMPT_CONTROLLER_BACKEND_USERNAME", 
        "password_key": "ATTEMPT_CONTROLLER_BACKEND_PASSWORD", 
        "role": "attempt-controller"
    },
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


def api(backend_svc, method, path, token=None, body=None):
    url = f"{backend_svc}/{path}"
    data = json.dumps(body).encode() if body else None
    headers = {"Content-Type": "application/json", "User-Agent": "dbusers-init/1.0"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    ssl_ctx = ssl._create_unverified_context() if os.environ.get("SSL_NO_VERIFY", "").lower() == "true" else None
    try:
        with urllib.request.urlopen(req, context=ssl_ctx) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        return json.loads(e.read())
    except urllib.error.URLError as e:
        print(f"ERROR: could not reach PocketBase at {url}: {e.reason}", file=sys.stderr)
        sys.exit(1)


def get_admin_token(backend_svc, email, password):
    resp = api(backend_svc, "POST", "collections/_superusers/auth-with-password",
               body={"identity": email, "password": password})
    token = resp.get("token")
    if not token:
        print(f"ERROR: could not authenticate as superuser: {resp}", file=sys.stderr)
        sys.exit(1)
    return token


def create_user(backend_svc, email, password, token, role=None):
    resp = api(backend_svc, "POST", "collections/users/records", token=token, body={
        "email": email,
        "password": password,
        "passwordConfirm": password,
        "svc_role": role,
        "emailVisibility": True,
    })
    email_error = resp.get("data", {}).get("email", {}).get("code")
    if resp.get("id"):
        print(f"  created {email}")
    elif resp.get("status") == 400 and email_error == "validation_not_unique":
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

    if (os.environ.get("SSL_NO_VERIFY") or env.get("SSL_NO_VERIFY", "")).lower() == "true":
        os.environ["SSL_NO_VERIFY"] = "true"

    admin_email = os.environ.get("POCKETBASE_ADMIN_EMAIL") or env.get("POCKETBASE_ADMIN_EMAIL")
    admin_password = os.environ.get("POCKETBASE_ADMIN_PASSWORD") or env.get("POCKETBASE_ADMIN_PASSWORD")
    backend_svc = (os.environ.get("POCKETBASE_URL") or env.get("POCKETBASE_URL", DEFAULT_BACKEND_URL)).strip().rstrip("/")

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
    token = get_admin_token(backend_svc, admin_email, admin_password)
    print(f"  ok")

    # Step 3: create service accounts
    print(f"[3/3] Creating service accounts...")
    for svc in SVC_ACCOUNTS:
        email = get_secret(svc["secret"], svc["email_key"], args.namespace)
        password = get_secret(svc["secret"], svc["password_key"], args.namespace)
        create_user(backend_svc, email, password, token, role=svc.get("role"))

    print("\nDone.")


if __name__ == "__main__":
    main()
