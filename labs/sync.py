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


def folder_id(folder_path: Path) -> str:
    rel = folder_path.relative_to(LABS_DIR)
    return "_".join(rel.parts)


def lab_id(lab_path: Path) -> str:
    rel = lab_path.relative_to(LABS_DIR)
    return "_".join(rel.with_suffix("").parts)


def collect_folders() -> list[Path]:
    """Return all subdirectory paths under LABS_DIR, sorted (parents before children)."""
    folders = sorted(
        p for p in LABS_DIR.rglob("*")
        if p.is_dir() and not any(part.startswith(".") for part in p.relative_to(LABS_DIR).parts)
    )
    return folders


def collect_labs() -> list[Path]:
    """Return all lab YAML files (excludes index.yaml)."""
    return sorted(
        p for p in LABS_DIR.rglob("*.yaml")
        if p.name != "index.yaml"
        and not any(part.startswith(".") for part in p.relative_to(LABS_DIR).parts)
    )


def validate_folder(path: Path) -> bool:
    fid = folder_id(path)
    errors = []
    for seg in path.relative_to(LABS_DIR).parts:
        if not _SLUG_RE.match(seg):
            errors.append(f"invalid segment '{seg}': only A-Za-z0-9 allowed")
    status = "ok" if not errors else "FAIL"
    print(f"  {status:<4}  {fid}  (folder)")
    for e in errors:
        print(f"          {e}")
    return not errors


def validate_lab(path: Path) -> bool:
    lid = lab_id(path)
    errors = []

    for seg in path.relative_to(LABS_DIR).with_suffix("").parts:
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
                assets = env.get("assets", []) if isinstance(env, dict) else []
                if not assets:
                    errors.append("environment.assets is empty")
                else:
                    names = [a.get("name") for a in assets if isinstance(a, dict)]
                    seen = set()
                    for n in names:
                        if n in seen:
                            errors.append(f"environment.assets has duplicate name '{n}'")
                        seen.add(n)
        except yaml.YAMLError as e:
            errors.append(f"YAML parse error: {e}")
        except OSError as e:
            errors.append(f"cannot read file: {e}")

    status = "ok" if not errors else "FAIL"
    print(f"  {status:<4}  {lid}")
    for e in errors:
        print(f"          {e}")
    return not errors


def upsert_record(token, collection, record_id, record):
    status, resp = api(token, "PATCH", f"/api/collections/{collection}/records/{record_id}", record)
    if status == 404:
        status, resp = api(token, "POST", f"/api/collections/{collection}/records", {"id": record_id, **record})
        if status in (200, 201):
            print(f"  created  {record_id}")
        else:
            print(f"  error    {record_id}: {resp}")
    elif status == 200:
        print(f"  updated  {record_id}")
    else:
        print(f"  error    {record_id}: status {status}")


def upsert_folder(token, path: Path):
    fid = folder_id(path)
    rel = path.relative_to(LABS_DIR)

    index = path / "index.yaml"
    title = rel.parts[-1]
    description = ""
    if index.exists():
        try:
            with open(index) as f:
                doc = yaml.safe_load(f)
            if isinstance(doc, dict) and isinstance(doc.get("meta"), dict):
                title = doc["meta"].get("title", title)
                description = doc["meta"].get("description", "")
        except yaml.YAMLError:
            pass

    parent = "_".join(rel.parts[:-1]) if len(rel.parts) > 1 else ""

    record = {"title": title, "description": description, "type": "folder"}
    if parent:
        record["parent"] = parent

    upsert_record(token, "labs", fid, record)


def upsert_lab(token, path: Path):
    lid = lab_id(path)
    rel = path.relative_to(LABS_DIR)
    parent = "_".join(rel.with_suffix("").parts[:-1])

    with open(path) as f:
        doc = yaml.safe_load(f)

    record = {
        "title": doc["meta"]["title"],
        "description": doc["meta"].get("description", ""),
        "content": doc.get("content", []),
        "environment": doc.get("environment", {}),
        "type": "lab",
        "parent": parent,
    }

    upsert_record(token, "labs", lid, record)


def fetch_all_ids(token, collection) -> set[str]:
    ids = set()
    page, total = 1, None
    while total is None or page <= total:
        _, data = api(token, "GET", f"/api/collections/{collection}/records?perPage=200&page={page}")
        if total is None:
            total = -(-data.get("totalItems", 0) // 200)
        for rec in data.get("items", []):
            ids.add(rec["id"])
        page += 1
    return ids


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--verify", action="store_true", help="validate files without syncing")
    args = parser.parse_args()

    folders = collect_folders()
    labs = collect_labs()

    print(f"Validating {len(folders)} folder(s) and {len(labs)} lab(s)...")
    results = [validate_folder(p) for p in folders] + [validate_lab(p) for p in labs]
    failed = results.count(False)
    total = len(results)
    print(f"  {total - failed}/{total} valid" + (f", {failed} failed" if failed else "") + ".")
    if failed:
        raise SystemExit(1)

    if args.verify:
        return

    load_env()
    for var in ("POCKETBASE_URL", "POCKETBASE_ADMIN_EMAIL", "POCKETBASE_ADMIN_PASSWORD"):
        if not os.environ.get(var):
            raise SystemExit(f"Missing env var: {var}")

    token = authenticate()

    print(f"\nUploading {len(folders)} folder(s)...")
    for path in folders:
        upsert_folder(token, path)

    print(f"\nUploading {len(labs)} lab(s)...")
    for path in labs:
        upsert_lab(token, path)

    local_ids = (
        {folder_id(p) for p in folders} |
        {lab_id(p) for p in labs}
    )
    remote_ids = fetch_all_ids(token, "labs")
    to_delete = remote_ids - local_ids

    if to_delete:
        print(f"\nDeleting {len(to_delete)} stale record(s)...")
        for rid in sorted(to_delete):
            api(token, "DELETE", f"/api/collections/labs/records/{rid}")
            print(f"  deleted  {rid}")

    print("\nDone.")


if __name__ == "__main__":
    main()
