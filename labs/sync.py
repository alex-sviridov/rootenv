#!/usr/bin/env python3
import argparse
import json
import os
import re
import urllib.request
import urllib.error
from pathlib import Path

import yaml

_SLUG_RE = re.compile(r'^[A-Za-z0-9]+$')

LABS_DIR = Path(__file__).parent
ENV_FILE = LABS_DIR / ".env"


def load_env():
    if ENV_FILE.exists():
        for line in ENV_FILE.read_text().splitlines():
            line = line.strip()
            if line and not line.startswith("#") and "=" in line:
                k, v = line.split("=", 1)
                os.environ.setdefault(k.strip(), v.strip())


def api(token, method, path, body=None):
    url = os.environ["POCKETBASE_URL"].rstrip("/") + path.removeprefix("/api")
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", token)
    try:
        with urllib.request.urlopen(req) as r:
            body = r.read()
            return r.status, json.loads(body) if body else {}
    except urllib.error.HTTPError as e:
        body = e.read()
        return e.code, json.loads(body) if body else {}


def authenticate():
    pb_url = os.environ["POCKETBASE_URL"].rstrip("/")
    _, data = api(None, "POST", "/api/collections/_superusers/auth-with-password", {
        "identity": os.environ["POCKETBASE_ADMIN_EMAIL"],
        "password": os.environ["POCKETBASE_ADMIN_PASSWORD"],
    })
    token = data.get("token")
    if not token:
        raise SystemExit(f"Auth failed at {pb_url}/collections/_superusers/auth-with-password: {data}")
    return token


def validate_lab(path: Path):
    rel = path.relative_to(LABS_DIR)
    segments = list(rel.with_suffix("").parts)
    lab_id = "_".join(segments)
    errors = []

    for seg in segments:
        if not _SLUG_RE.match(seg):
            errors.append(f"invalid path segment '{seg}': only A-Za-z0-9 allowed")

    if not errors:
        try:
            with open(path) as f:
                doc = yaml.safe_load(f)
            if not isinstance(doc, dict):
                errors.append("not a YAML mapping")
            else:
                meta = doc.get("meta")
                if not meta:
                    errors.append("missing meta")
                elif not meta.get("title"):
                    errors.append("meta.title is required")
                content = doc.get("content")
                if not content:
                    errors.append("missing content")
                elif not isinstance(content, list):
                    errors.append("content must be a list")
                else:
                    for i, task in enumerate(content):
                        if not task.get("title"):
                            errors.append(f"content[{i}] missing title")
                        if not task.get("content"):
                            errors.append(f"content[{i}] missing content")
                env = doc.get("environment", {})
                servers = env.get("servers", []) if isinstance(env, dict) else []
                if not servers:
                    errors.append("environment.servers is empty")
        except yaml.YAMLError as e:
            errors.append(f"YAML parse error: {e}")
        except OSError as e:
            errors.append(f"cannot read file: {e}")

    status = "ok" if not errors else "FAIL"
    print(f"  {status:<4}  {lab_id}")
    for e in errors:
        print(f"          {e}")
    return not errors


def upsert_lab(token, path: Path):
    rel = path.relative_to(LABS_DIR)
    lab_id = "_".join(rel.with_suffix("").parts)

    with open(path) as f:
        doc = yaml.safe_load(f)

    record = {
        "title": doc["meta"]["title"],
        "description": doc["meta"].get("description", ""),
        "content": doc.get("content", []),
        "environment": doc.get("environment", {}),
    }

    status, resp = api(token, "PATCH", f"/api/collections/labs/records/{lab_id}", record)
    if status == 404:
        status, resp = api(token, "POST", "/api/collections/labs/records", {"id": lab_id, **record})
        if status in (200, 201):
            print(f"  created  {lab_id}")
        else:
            print(f"  error    {lab_id}: {resp}")
    elif status == 200:
        print(f"  updated  {lab_id}")
    else:
        print(f"  error    {lab_id}: status {status}")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--verify", action="store_true", help="validate YAML files without syncing")
    args = parser.parse_args()

    files = sorted(LABS_DIR.rglob("*.yaml"))

    print(f"Validating {len(files)} lab(s)...")
    results = [validate_lab(p) for p in files]
    failed = results.count(False)
    print(f"  {len(files) - failed}/{len(files)} valid" + (f", {failed} failed" if failed else "") + ".")
    if failed:
        raise SystemExit(1)

    if args.verify:
        return

    load_env()
    for var in ("POCKETBASE_URL", "POCKETBASE_ADMIN_EMAIL", "POCKETBASE_ADMIN_PASSWORD"):
        if not os.environ.get(var):
            raise SystemExit(f"Missing env var: {var}")

    token = authenticate()

    print(f"\nUploading {len(files)} lab(s)...")
    for path in files:
        upsert_lab(token, path)

    local_ids = {
        "_".join(p.relative_to(LABS_DIR).with_suffix("").parts)
        for p in files
    }
    page, total = 1, None
    to_delete = []
    while total is None or page <= total:
        _, data = api(token, "GET", f"/api/collections/labs/records?perPage=200&page={page}")
        if total is None:
            total = -(-data.get("totalItems", 0) // 200)
        for rec in data.get("items", []):
            if rec["id"] not in local_ids:
                to_delete.append(rec["id"])
        page += 1

    if to_delete:
        print(f"\nDeleting {len(to_delete)} stale lab(s)...")
        for lab_id in to_delete:
            api(token, "DELETE", f"/api/collections/labs/records/{lab_id}")
            print(f"  deleted  {lab_id}")

    print("\nDone.")


if __name__ == "__main__":
    main()
