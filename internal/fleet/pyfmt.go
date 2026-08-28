package fleet

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
) // pyfmt (WO-8): bounded emitters that reproduce PYTHON's serialization
// byte-for-byte for the formats the sos-lab engine embeds in cluster
// objects and state files. NOT general emitters — the shapes are fixed by
// labctl:
//   - pyJSONDumps:  json.dumps(obj, sort_keys=True)           (single line,
//     default separators ", " / ": ", ensure_ascii) — monitor CM strings
//   - pyJSONIndent: json.dump(obj, indent=2, sort_keys=True)  (state files)
//   - pyGatusDump:  yaml.safe_dump({"endpoints": eps}, sort_keys=False)
//
// Parity against python is asserted by TestLabRenderParity /
// TestLabMonitorParity / TestLabStateParity (go test) and corpus C13a.

func pyScalarJSON(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case string:
		return pyEscapeString(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	default:
		return pyEscapeString(fmt.Sprintf("%v", t))
	}
}

// json_Number lets both decode paths (UseNumber) share the scalar writer.
type json_Number string

func pyEscapeString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r < 0x20 || r > 0x7e:
			if r > 0xffff {
				hi, lo := utf16.EncodeRune(r)
				fmt.Fprintf(&b, `\u%04x\u%04x`, hi, lo)
			} else {
				fmt.Fprintf(&b, `\u%04x`, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// pyJSONDumps mirrors json.dumps(obj, sort_keys=True): single line,
// ", " between items and ": " after keys.
func pyJSONDumps(v any) string {
	var b strings.Builder
	pyJSONInline(&b, v)
	return b.String()
}

func pyJSONInline(b *strings.Builder, v any) {
	switch t := v.(type) {
	case map[string]any:
		keys := sortedKeys(t)
		b.WriteString("{")
		for i, k := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(pyEscapeString(k))
			b.WriteString(": ")
			pyJSONInline(b, t[k])
		}
		b.WriteString("}")
	case []any:
		b.WriteString("[")
		for i, item := range t {
			if i > 0 {
				b.WriteString(", ")
			}
			pyJSONInline(b, item)
		}
		b.WriteString("]")
	default:
		b.WriteString(pyScalarJSON(v))
	}
}

// pyJSONIndent mirrors json.dump(obj, indent=2, sort_keys=True).
func pyJSONIndent(v any) string {
	var b strings.Builder
	pyIndentWrite(&b, v, 0)
	return b.String()
}

func pyIndentWrite(b *strings.Builder, v any, depth int) {
	ind, indIn := strings.Repeat("  ", depth), strings.Repeat("  ", depth+1)
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			b.WriteString("{}")
			return
		}
		keys := sortedKeys(t)
		b.WriteString("{\n")
		for i, k := range keys {
			b.WriteString(indIn)
			b.WriteString(pyEscapeString(k))
			b.WriteString(": ")
			pyIndentWrite(b, t[k], depth+1)
			if i < len(keys)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(ind)
		b.WriteByte('}')
	case []any:
		if len(t) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, item := range t {
			b.WriteString(indIn)
			pyIndentWrite(b, item, depth+1)
			if i < len(t)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(ind)
		b.WriteByte(']')
	default:
		b.WriteString(pyScalarJSON(v))
	}
}

// pyGatusDump mirrors yaml.safe_dump({"endpoints": endpoints},
// sort_keys=False) for labctl's endpoint shape. Endpoint dicts emit in
// construction order (name, url, interval, conditions); list dashes sit at
// the parent key's indent; strings quote only when pyyaml would.
func pyGatusDump(endpoints []any) string {
	var b strings.Builder
	if len(endpoints) == 0 {
		b.WriteString("endpoints: []\n")
		return b.String()
	}
	b.WriteString("endpoints:\n")
	for _, eAny := range endpoints {
		e := asMap(eAny)
		b.WriteString("- name: ")
		b.WriteString(pyYamlScalar(asString(e["name"])))
		b.WriteString("\n")
		b.WriteString("  url: ")
		b.WriteString(pyYamlScalar(asString(e["url"])))
		b.WriteString("\n")
		b.WriteString("  interval: ")
		b.WriteString(pyYamlScalar(asString(e["interval"])))
		b.WriteString("\n")
		b.WriteString("  conditions:\n")
		for _, c := range asList(e["conditions"]) {
			b.WriteString("  - ")
			b.WriteString(pyYamlScalar(asString(c)))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// pyYamlScalar renders one scalar the way pyyaml's emitter does for our
// value domain: plain when safe, single-quoted otherwise.
func pyYamlScalar(s string) string {
	if !pyYamlPlainSafe(s) {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return s
}

func pyYamlPlainSafe(s string) bool {
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, "\n\r") || strings.Contains(s, ": ") ||
		strings.Contains(s, " #") || strings.HasSuffix(s, " ") {
		return false
	}
	// leading indicator characters force quoting
	if strings.ContainsAny(s[:1], "-?:,[]{}#&*!|>'\"%@`") {
		return false
	}
	// strings that would parse as another type are quoted
	switch strings.ToLower(s) {
	case "true", "false", "null", "yes", "no", "on", "off", "~":
		return false
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return false
	}
	return true
}

// fmtPythonList renders []string like python str(list) — "['a', 'b']" —
// which f-strings embed into labctl's drift messages.
func fmtPythonList(items []string) string {
	parts := make([]string, len(items))
	for i, s := range items {
		parts[i] = "'" + s + "'"
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// canonicalJSON is fleet's canonical byte form for render output: sorted
// keys, 2-space indent, no HTML escaping. Both python golden generation
// (json.dumps(indent=2, sort_keys=True), ASCII data) and the corpus diff
// rely on this exact shape.
func canonicalJSON(v any) ([]byte, error) {
	raw, err := jsonMarshal(v)
	if err != nil {
		return nil, err
	}
	norm, err := decodeJSONNumber(raw)
	if err != nil {
		return nil, err
	}
	return jsonMarshalIndent(norm)
}
