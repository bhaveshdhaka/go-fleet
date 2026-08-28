// tests/lib/jsonq — stdlib-only JSON query helper for the bash corpus
// (WO-20 close-out). The corpus previously validated its --json fixtures
// with an interpreted-runtime helper; this file replaces it so the whole
// workflow is interpreter-free. Hermetic build (see lib.sh jsonq_build):
//
//	go build -trimpath -o jsonq tests/lib/jsonq.go
//
// Usage: jsonq <file> <cmd> [args...]
//
//	valid                        exit 0 if the file parses as JSON
//	type <path>                  print object|list|string|number|bool|null
//	len <path>                   print length (list/object/string), else -1
//	keys <path>                  sorted object keys, comma-separated
//	keys-each <path>             one line per list element: sorted keys CSV
//	str <path>                   print the scalar at path
//	has <path> <key>             exit 0 if the object at path has <key>
//	match <path> <sub>           exit 0 if the string at path contains <sub>
//	count <path> <key>=<val>     count list elements whose member <key> == <val>
//	find <path> <key>=<val> <field>  member <field> of the first matching element
//
// Paths are dot-separated from the root ("." = root); numeric segments
// index into lists. Any miss exits 1 with a message on stderr.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "jsonq: "+f+"\n", a...)
	os.Exit(1)
}

func kind(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "list"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	default:
		return "null"
	}
}

func scalar(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(x), true
	default:
		return "", false
	}
}

func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// at walks a dot path from the root; numeric segments index lists.
func at(root any, path string) (any, error) {
	cur := root
	trimmed := strings.TrimPrefix(path, ".")
	if trimmed == "" {
		return cur, nil
	}
	for _, seg := range strings.Split(trimmed, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return nil, fmt.Errorf("path %s: no key %q", path, seg)
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(node) {
				return nil, fmt.Errorf("path %s: bad index %q", path, seg)
			}
			cur = node[i]
		default:
			return nil, fmt.Errorf("path %s: %q descends into a scalar", path, seg)
		}
	}
	return cur, nil
}

func main() {
	if len(os.Args) < 3 {
		die("usage: jsonq <file> <cmd> [args...]")
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		die("read: %v", err)
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		die("parse: %v", err)
	}
	cmd, args := os.Args[2], os.Args[3:]

	if cmd == "valid" {
		return
	}
	if len(args) == 0 {
		die("cmd %s needs a path", cmd)
	}
	v, err := at(root, args[0])
	if err != nil {
		die("%v", err)
	}
	rest := args[1:]

	switch cmd {
	case "type":
		fmt.Println(kind(v))
	case "len":
		switch x := v.(type) {
		case map[string]any:
			fmt.Println(len(x))
		case []any:
			fmt.Println(len(x))
		case string:
			fmt.Println(len(x))
		default:
			fmt.Println(-1)
		}
	case "keys":
		m, ok := v.(map[string]any)
		if !ok {
			die("keys: not an object at %s", args[0])
		}
		fmt.Println(strings.Join(sortedKeys(m), ","))
	case "keys-each":
		l, ok := v.([]any)
		if !ok {
			die("keys-each: not a list at %s", args[0])
		}
		for i, e := range l {
			m, ok := e.(map[string]any)
			if !ok {
				die("keys-each: element %d not an object", i)
			}
			fmt.Println(strings.Join(sortedKeys(m), ","))
		}
	case "str":
		s, ok := scalar(v)
		if !ok {
			die("str: not a scalar at %s", args[0])
		}
		fmt.Println(s)
	case "has":
		if len(rest) != 1 {
			die("has: needs <key>")
		}
		m, ok := v.(map[string]any)
		if !ok {
			os.Exit(1)
		}
		if _, present := m[rest[0]]; !present {
			os.Exit(1)
		}
	case "match":
		if len(rest) != 1 {
			die("match: needs <sub>")
		}
		s, ok := scalar(v)
		if !ok || !strings.Contains(s, rest[0]) {
			os.Exit(1)
		}
	case "count":
		if len(rest) != 1 || !strings.Contains(rest[0], "=") {
			die("count: needs <key>=<val>")
		}
		l, ok := v.([]any)
		if !ok {
			die("count: not a list at %s", args[0])
		}
		kv := strings.SplitN(rest[0], "=", 2)
		n := 0
		for _, e := range l {
			m, ok := e.(map[string]any)
			if !ok {
				continue
			}
			if s, ok := scalar(m[kv[0]]); ok && s == kv[1] {
				n++
			}
		}
		fmt.Println(n)
	case "find":
		if len(rest) != 2 || !strings.Contains(rest[0], "=") {
			die("find: needs <key>=<val> <field>")
		}
		l, ok := v.([]any)
		if !ok {
			die("find: not a list at %s", args[0])
		}
		kv := strings.SplitN(rest[0], "=", 2)
		for _, e := range l {
			m, ok := e.(map[string]any)
			if !ok {
				continue
			}
			if s, ok := scalar(m[kv[0]]); ok && s == kv[1] {
				out, ok := scalar(m[rest[1]])
				if !ok {
					die("find: field %s is not a scalar", rest[1])
				}
				fmt.Println(out)
				return
			}
		}
		os.Exit(1)
	default:
		die("unknown cmd %s", cmd)
	}
}
