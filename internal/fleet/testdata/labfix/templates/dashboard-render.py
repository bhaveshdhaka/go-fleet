import json
import os

STATE = "/state/state.json"
PRINCIPLES = "/state/principles.json"
HOSTS = "/state/hosts.json"
OUT = "/webroot/index.html"


def fmt_table(rows):
    return "<table>" + "".join(f"<tr>{row}</tr>" for row in rows) + "</table>"


with open(STATE) as f:
    state = json.load(f)
with open(HOSTS) as f:
    hosts = json.load(f)
try:
    with open(PRINCIPLES) as f:
        principles = json.load(f)
except OSError:
    principles = {}

rows = []
for name in sorted(state):
    entry = state[name]
    host = hosts.get(name, "-")
    rows.append(f"<td>{name}</td><td>{entry.get('tag', '-')}</td><td>{host}</td>")

html = "<html><head><title>sos-lab</title></head><body>"
if principles:
    html += "<h1>principles</h1>"
html += fmt_table(rows)
html += "</body></html>"

with open(OUT, "w") as f:
    f.write(html)
