package fleet

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// jsonio: encoding/json wrappers with HTML escaping disabled — python's
// json module never escapes <, >, & and our byte-parity targets depend on
// matching it (WO-8).

func jsonMarshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Encode appends a newline; drop it — callers control framing.
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func jsonMarshalIndent(v any) ([]byte, error) {
	raw, err := jsonMarshal(v)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// decodeJSONNumber decodes raw JSON preserving number literals, then
// converts integral numbers to int so decoded python JSON has the same
// types the renderers produce (miniyaml yields int for ports etc.).
func decodeJSONNumber(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return normalizeJSONNumbers(v), nil
}

func normalizeJSONNumbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = normalizeJSONNumbers(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = normalizeJSONNumbers(val)
		}
		return t
	case json.Number:
		if i, err := strconv.ParseInt(t.String(), 10, 64); err == nil &&
			strings.TrimPrefix(t.String(), "+") == strconv.FormatInt(i, 10) &&
			!strings.Contains(t.String(), ".") {
			if i >= -1<<31 && i < 1<<31 {
				return int(i)
			}
		}
		return string(t)
	default:
		return v
	}
}
