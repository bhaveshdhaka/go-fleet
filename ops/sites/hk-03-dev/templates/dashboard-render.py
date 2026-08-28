#!/usr/bin/env python3
"""sos-dashboard renderer: fleet table + principles -> /webroot/index.html"""
import json
import os
import time
import urllib.request

API = os.environ.get("GATUS_API", "http://gatus.sos-lab.svc.cluster.local:8080/api/v1/endpoints/statuses")
STATE_DIR = "/state"
OUT = "/webroot/index.html"


def load_json(name):
    try:
        return json.load(open(os.path.join(STATE_DIR, name)))
    except Exception as e:
        print(f"read {name} failed: {e}")
        return {}


def fetch_health():
    try:
        with urllib.request.urlopen(API, timeout=15) as resp:
            data = json.load(resp)
    except Exception as e:
        print("gatus api failed:", e)
        return {}
    # API shape varies by gatus version: some return {"results": [...]},
    # this version returns a bare [...] list.
    eps = data.get("results", []) if isinstance(data, dict) else data
    out = {}
    for ep in eps:
        # newer versions expose a top-level per-endpoint "status"
        # ("healthy"/"unhealthy"); this version only exposes results[]
        # with per-check "success" booleans. Handle both.
        checks = ep.get("results") or []
        if checks:
            up = all(bool(c.get("success")) for c in checks[-1:])
        else:
            st = str(ep.get("status", "")).lower()
            up = st in ("up", "healthy")
        out[ep.get("name", "")] = "up" if up else "down"
    return out


def esc(s):
    return str(s).replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


def render():
    state = load_json("state.json")
    hosts = load_json("hosts.json")
    principles = load_json("principles.json")
    health = fetch_health()

    rows = []
    for svc in sorted(set(state) | set(health)):
        info = state.get(svc, {})
        tag = esc(info.get("tag", "-"))
        dot = '<span class="dot up"></span>up' if health.get(svc) == "up" else '<span class="dot down"></span>down'
        host = hosts.get(svc, "")
        url = f'<a href="https://{esc(host)}">{esc(host)}</a>' if host else "-"
        rows.append(f"<tr><td>{esc(svc)}</td><td><code>{tag}</code></td><td>{dot}</td><td>{url}</td></tr>")

    prin = "".join(
        f"<li><strong>{esc(k.replace('_', ' '))}</strong> — {esc(v)}</li>"
        for k, v in principles.items()
    )

    html = f"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>sos-lab fleet</title>
<style>
body{{background:#0d1117;color:#c9d1d9;font-family:system-ui,-apple-system,sans-serif;margin:2rem;max-width:960px}}
h1{{color:#58a6ff;font-size:1.5rem}} h2{{color:#8b949e;font-size:1.1rem;margin-top:2rem}}
table{{border-collapse:collapse;width:100%}}
th,td{{padding:.5rem .75rem;border-bottom:1px solid #21262d;text-align:left;font-size:.95rem}}
th{{color:#8b949e;text-transform:uppercase;font-size:.75rem;letter-spacing:.05em}}
.dot{{display:inline-block;width:10px;height:10px;border-radius:50%;margin-right:.4rem;vertical-align:middle}}
.up{{background:#3fb950}}.down{{background:#f85149}}
a{{color:#58a6ff;text-decoration:none}} code{{color:#79c0ff;background:#161b22;padding:.1rem .3rem;border-radius:4px}}
li{{margin:.4rem 0}} strong{{color:#e6edf3}} footer{{margin-top:2rem;color:#484f58;font-size:.8rem}}
</style>
</head>
<body>
<h1>sos-lab fleet</h1>
<table>
<tr><th>service</th><th>tag</th><th>health</th><th>url</th></tr>
{''.join(rows)}
</table>
<h2>System Principles</h2>
<ul>
{prin}
</ul>
<footer>rendered {time.strftime('%Y-%m-%d %H:%M:%S UTC', time.gmtime())} &middot; sos-dashboard</footer>
</body>
</html>
"""
    tmp = OUT + ".tmp"
    with open(tmp, "w") as f:
        f.write(html)
    os.replace(tmp, OUT)
    print(f"rendered {OUT} ({len(html)} bytes, {len(rows)} services)")


if __name__ == "__main__":
    while True:
        render()
        time.sleep(60)
