package fleet

import (
	"embed"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed templates
var templatesFS embed.FS

// renderTemplate renders the named embedded template with the given data
// and writes it to path (parents are created). templates/ ships inside the
// binary so a single static file can scaffold whole projects.
func renderTemplate(path, tmplName string, data map[string]string) error {
	raw, err := templatesFS.ReadFile("templates/" + tmplName)
	if err != nil {
		return err
	}
	t, err := template.New(path).Parse(string(raw))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return t.Execute(io.Writer(f), data)
}

// writeSeed writes embedded content that carries no template placeholders.
func writeSeed(path string, tmplName string) error {
	raw, err := templatesFS.ReadFile("templates/" + tmplName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func templateData(extra map[string]string) map[string]string {
	data := map[string]string{}
	for k, v := range extra {
		data[k] = v
	}
	return data
}

// validComponentName: journal-safe, path-safe component tokens
// ([a-z0-9._-], no leading dash/dot).
func validComponentName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '.':
		case r == '_':
		default:
			return false
		}
	}
	return !strings.HasPrefix(name, "-") && !strings.HasPrefix(name, ".")
}

// validWorkorderID: like component names but uppercase allowed, since the
// house convention is WO-<n> (workorders/WO-1.md ...).
func validWorkorderID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '.':
		case r == '_':
		default:
			return false
		}
	}
	return !strings.HasPrefix(id, "-") && !strings.HasPrefix(id, ".")
}
