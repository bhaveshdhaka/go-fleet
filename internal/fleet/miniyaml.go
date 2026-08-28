package fleet

import (
	"fmt"
	"strconv"
	"strings"
)

// Bounded block-YAML parser (WO-7): reads the sos-lab registry and the
// fleet SITES/registry files without external dependencies. Supports the
// constructs those files actually use: nested maps, block sequences of
// scalars or maps, flow lists, folded (>-) scalars, quoted scalars,
// booleans, integers, full-line comments. Anything outside this subset is
// a parse error, not a guess.

type yamlLine struct {
	indent int
	text   string // trimmed content
	num    int
}

func parseMiniYAML(text string) (any, error) {
	physical := strings.Split(text, "\n")
	// Merge multi-line double-quoted scalars (YAML flow continuation):
	// a line whose quotes are still open at end-of-line consumes the
	// following line(s). A trailing backslash suppresses the fold space;
	// a leading '\ ' on the continuation preserves an escaped space.
	var merged []string
	for i := 0; i < len(physical); i++ {
		raw := physical[i]
		trimmed := strings.TrimLeft(raw, " ")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		line := trimmed
		for yamlQuoteOpen(line) && i+1 < len(physical) {
			i++
			cont := strings.TrimLeft(physical[i], " ")
			cont = strings.TrimRight(cont, "\r")
			if strings.HasSuffix(line, "\\") && !strings.HasSuffix(line, "\\\\") {
				line = strings.TrimSuffix(line, "\\")
			} else {
				line += " "
			}
			if strings.HasPrefix(cont, "\\ ") {
				cont = strings.TrimPrefix(cont, "\\")
			}
			line += cont
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if strings.Contains(raw[:len(raw)-len(strings.TrimLeft(raw, " "))], "\t") {
			return nil, fmt.Errorf("yaml line %d: tabs are not allowed", i+1)
		}
		merged = append(merged, strings.Repeat(" ", indent)+line)
	}
	var lines []yamlLine
	for i, raw := range merged {
		trimmed := strings.TrimLeft(raw, " ")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, yamlLine{indent: len(raw) - len(trimmed), text: trimmed, num: i + 1})
	}
	if len(lines) == 0 {
		return map[string]any{}, nil
	}
	v, next, err := yamlNode(lines, 0, lines[0].indent)
	if err != nil {
		return nil, err
	}
	if next != len(lines) {
		return nil, fmt.Errorf("yaml line %d: unexpected content", lines[next].num)
	}
	return v, nil
}

// yamlQuoteOpen reports whether a double-quoted scalar is still open at
// end-of-line (odd count of unescaped '"').
func yamlQuoteOpen(s string) bool {
	open := false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\':
			i++
		case s[i] == '"':
			open = !open
		}
	}
	return open
}

func yamlNode(lines []yamlLine, i, indent int) (any, int, error) {
	if i >= len(lines) {
		return nil, i, nil
	}
	if strings.HasPrefix(lines[i].text, "- ") || lines[i].text == "-" {
		return yamlList(lines, i, indent)
	}
	return yamlMap(lines, i, indent)
}

func yamlList(lines []yamlLine, i, indent int) (any, int, error) {
	out := []any{}
	prevScalar := false
	for i < len(lines) && lines[i].indent == indent &&
		(strings.HasPrefix(lines[i].text, "- ") || lines[i].text == "-") {
		item := strings.TrimPrefix(strings.TrimPrefix(lines[i].text, "-"), " ")
		if item == "" {
			// nested block under a bare dash
			v, next, err := yamlNode(lines, i+1, indent+1)
			if err != nil {
				return nil, 0, err
			}
			out = append(out, v)
			i = next
			prevScalar = false
			continue
		}
		if k, v, ok := splitYAMLKey(item); ok {
			// map item: gather deeper lines as its children
			child := []yamlLine{{indent: indent + 2, text: k + ":" + yamlInline(v), num: lines[i].num}}
			j := i + 1
			for j < len(lines) && lines[j].indent > indent {
				child = append(child, lines[j])
				j++
			}
			m, _, err := yamlMap(child, 0, indent+2)
			if err != nil {
				return nil, 0, err
			}
			out = append(out, m)
			i = j
			prevScalar = false
			continue
		}
		if item == ">-" || item == "|" || item == "|-" {
			v, next := yamlFolded(lines, i+1, indent)
			out = append(out, v)
			i = next
			prevScalar = false
			continue
		}
		out = append(out, yamlScalar(item))
		i++
		prevScalar = true
		continue
	}
	// plain multi-line scalar continuation INSIDE a list item (pyyaml
	// safe_dump emits long plain scalars folded across lines at deeper
	// indent): fold into the previous string item.
	for i < len(lines) && lines[i].indent > indent && prevScalar {
		if s, ok := out[len(out)-1].(string); ok {
			out[len(out)-1] = s + " " + lines[i].text
			i++
			continue
		}
		break
	}
	return out, i, nil
}

func yamlMap(lines []yamlLine, i, indent int) (any, int, error) {
	out := map[string]any{}
	lastKey := ""
	for i < len(lines) {
		ln := lines[i]
		if ln.indent < indent {
			break
		}
		if ln.indent > indent {
			// plain multi-line scalar continuation: fold into the previous
			// key's value (bounded to the construct sos-lab's registry uses)
			if lastKey != "" {
				if prev, ok := out[lastKey].(string); ok {
					out[lastKey] = prev + " " + ln.text
					i++
					continue
				}
			}
			return nil, 0, fmt.Errorf("yaml line %d: bad indent %d (expected %d)", ln.num, ln.indent, indent)
		}
		if strings.HasPrefix(ln.text, "- ") || ln.text == "-" {
			// sequence value of the previous key at the same indent
			break
		}
		k, v, ok := splitYAMLKey(ln.text)
		if !ok {
			return nil, 0, fmt.Errorf("yaml line %d: expected 'key:'", ln.num)
		}
		if strings.TrimSpace(v) != "" {
			out[k] = yamlScalar(strings.TrimSpace(v))
			lastKey = k
			i++
			continue
		}
		// nested block: deeper map/list, or a same-indent sequence
		if i+1 < len(lines) && lines[i+1].indent > indent {
			child, next, err := yamlNode(lines, i+1, lines[i+1].indent)
			if err != nil {
				return nil, 0, err
			}
			out[k] = child
			lastKey = k
			i = next
			continue
		}
		if i+1 < len(lines) && lines[i+1].indent == indent &&
			(strings.HasPrefix(lines[i+1].text, "- ") || lines[i+1].text == "-") {
			child, next, err := yamlList(lines, i+1, indent)
			if err != nil {
				return nil, 0, err
			}
			out[k] = child
			lastKey = k
			i = next
			continue
		}
		out[k] = nil
		lastKey = k
		i++
	}
	return out, i, nil
}

func yamlFolded(lines []yamlLine, i, indent int) (string, int) {
	var parts []string
	for i < len(lines) && lines[i].indent > indent {
		parts = append(parts, lines[i].text)
		i++
	}
	return strings.TrimSpace(strings.Join(parts, " ")), i
}

func splitYAMLKey(s string) (string, string, bool) {
	if strings.HasPrefix(s, "\"") || strings.HasPrefix(s, "'") {
		return "", "", false
	}
	i := strings.Index(s, ":")
	if i <= 0 {
		return "", "", false
	}
	rest := s[i+1:]
	if rest != "" && !strings.HasPrefix(rest, " ") {
		return "", "", false
	}
	return strings.TrimSpace(s[:i]), rest, true
}

func yamlInline(v string) string {
	if v == "" {
		return ""
	}
	return " " + v
}

func yamlScalar(s string) any {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		if s[0] == '"' {
			if unq, err := strconv.Unquote(s); err == nil {
				return unq
			}
		}
		return s[1 : len(s)-1]
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		if inner == "" {
			return []any{}
		}
		var out []any
		for _, part := range splitFlow(inner) {
			out = append(out, yamlScalar(strings.TrimSpace(part)))
		}
		return out
	}
	switch s {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return s
}

// splitFlow splits a flow-list body on top-level commas (no nesting used
// in our files beyond flat lists).
func splitFlow(s string) []string {
	var out []string
	depth, start, inQ := 0, 0, byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQ != 0:
			if c == inQ {
				inQ = 0
			}
		case c == '"' || c == '\'':
			inQ = c
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
		case c == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func asList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(t)
	}
	return ""
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}
