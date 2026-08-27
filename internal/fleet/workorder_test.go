package fleet

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const woFrontMatter = `---
wo: WO-9
title: Test workorder
status: IN_PROGRESS
plan: PLAN.md
pieces:
  - id: 1
    title: first piece
    verify: bash scripts/test.sh
    integrated: true
  - id: 2
    title: second piece
    verify: go test ./...
    integrated: false
---

# WO-9 — Test workorder

> **Status:** IN PROGRESS this session · Owner directive: n/a
`

const woLegacy = `# WO-2 — legacy workorder

> **Status:** EXECUTED this session · Owner directive: read+approve dashboard

body
`

func writeWO(t *testing.T, dir, name, content string) Workorder {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return parseWorkorder(p)
}

func TestParseFrontMatter(t *testing.T) {
	dir := t.TempDir()
	w := writeWO(t, dir, "WO-9.md", woFrontMatter)
	if w.Schema != 1 {
		t.Fatalf("schema = %d, want 1", w.Schema)
	}
	if w.ID != "WO-9" || w.Title != "Test workorder" || w.Status != "IN_PROGRESS" {
		t.Fatalf("header fields = %s/%s/%s", w.ID, w.Title, w.Status)
	}
	if w.Plan != "PLAN.md" {
		t.Fatalf("plan = %q", w.Plan)
	}
	if len(w.Pieces) != 2 {
		t.Fatalf("pieces = %d, want 2", len(w.Pieces))
	}
	if w.Pieces[0].ID != "1" || w.Pieces[0].Title != "first piece" ||
		w.Pieces[0].Verify != "bash scripts/test.sh" || !w.Pieces[0].Integrated {
		t.Fatalf("piece 0 = %+v", w.Pieces[0])
	}
	if w.Pieces[1].Integrated {
		t.Fatal("piece 1 must not be integrated")
	}
	if !w.isActive() {
		t.Fatal("IN_PROGRESS must be active")
	}
	if w.unintegratedCount() != 1 {
		t.Fatalf("unintegrated = %d, want 1", w.unintegratedCount())
	}
}

func TestParseLegacyWorkorder(t *testing.T) {
	dir := t.TempDir()
	w := writeWO(t, dir, "WO-2.md", woLegacy)
	if w.Schema != 0 {
		t.Fatalf("schema = %d, want 0", w.Schema)
	}
	if w.Status != "EXECUTED this session" {
		t.Fatalf("status = %q, want prose verbatim (legacy)", w.Status)
	}
	if w.isActive() {
		t.Fatal("EXECUTED must not be active")
	}
}

func TestLoadWorkordersSorted(t *testing.T) {
	dir := t.TempDir()
	writeWO(t, dir, "WO-10.md", woLegacy)
	writeWO(t, dir, "WO-2.md", woLegacy)
	writeWO(t, dir, "WO-9.md", woFrontMatter)
	wos := loadWorkorders(dir)
	var got []string
	for _, w := range wos {
		got = append(got, w.ID)
	}
	want := []string{"WO-10", "WO-2", "WO-9"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	if wos[2].Schema != 1 || wos[1].Schema != 0 {
		t.Fatal("schema attribution wrong")
	}
}

func TestInvalidStatusKeptRaw(t *testing.T) {
	dir := t.TempDir()
	w := writeWO(t, dir, "WO-7.md",
		strings.Replace(woFrontMatter, "status: IN_PROGRESS", "status: BROKEN", 1))
	if w.Status != "BROKEN" {
		t.Fatalf("raw invalid status must be preserved for check, got %q", w.Status)
	}
	if w.isActive() {
		t.Fatal("invalid status must not count as active")
	}
}
