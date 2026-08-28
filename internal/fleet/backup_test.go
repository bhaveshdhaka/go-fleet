package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResticRoundTrip (WO-20, C20b seed): REAL restic against a local
// repo — init, two backups (dedup), snapshot listing, restore, byte
// compare. Skips when the toolchain restic binary is absent.
func TestResticRoundTrip(t *testing.T) {
	bin := "/home/openchamber/workspaces/.toolchain/bin/restic"
	if _, err := os.Stat(bin); err != nil {
		t.Skip("toolchain restic missing")
	}
	scratch := t.TempDir()
	repo := filepath.Join(scratch, "repo")
	src := filepath.Join(scratch, "data")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &backupConfig{Repo: repo} // local backend
	creds := &r2Creds{Password: "test-password"}
	if _, err := hostRestic(creds, cfg, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := hostRestic(creds, cfg, "backup", src, "--tag", "service=t"); err != nil {
		t.Fatalf("backup1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "b.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := hostRestic(creds, cfg, "backup", src, "--tag", "service=t"); err != nil {
		t.Fatalf("backup2: %v", err)
	}
	snap, err := hostRestic(creds, cfg, "snapshots", "--json", "--tag", "service=t")
	if err != nil {
		t.Fatalf("snapshots: %v", err)
	}
	if !strings.Contains(snap, `"short_id"`) && !strings.Contains(snap, `"id"`) {
		t.Fatalf("snapshots json unexpected: %s", snap)
	}
	dst := filepath.Join(scratch, "restored")
	if _, err := hostRestic(creds, cfg, "restore", "latest", "--tag", "service=t",
		"--target", dst); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "srv", "data", "a.txt"))
	// restic recreates absolute paths under target
	if err != nil {
		got, err = os.ReadFile(filepath.Join(dst, strings.TrimPrefix(src, "/"), "a.txt"))
		if err != nil {
			t.Fatalf("restored file: %v", err)
		}
	}
	if string(got) != "hello world\n" {
		t.Fatalf("content mismatch: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dst, strings.TrimPrefix(src, "/"), "b.txt")); err != nil {
		t.Fatalf("second file missing after restore: %v", err)
	}
	// retention is a no-op here but must not error
	if _, err := hostRestic(creds, cfg, "forget", "--prune", "--keep-daily", "7"); err != nil {
		t.Fatalf("forget: %v", err)
	}
}
