package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadKeyFile_RejectsWorldReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kek")

	// Permissão 0644 — bits de grupo/outro setados.
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadKeyFile(path)
	if err == nil {
		t.Fatal("expected error for 0644 file")
	}
}

func TestReadKeyFile_Accepts0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kek")
	if err := os.WriteFile(path, []byte("super-secret-32-bytes-or-more!"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "super-secret-32-bytes-or-more!" {
		t.Errorf("got %q, want %q", got, "super-secret-32-bytes-or-more!")
	}
}

func TestReadKeyFile_RejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, err := ReadKeyFile(link)
	if err == nil {
		t.Fatal("expected error for symlink")
	}
}

func TestReadKeyFile_TrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kek")
	if err := os.WriteFile(path, []byte("  secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret" {
		t.Errorf("got %q, want %q", got, "secret")
	}
}

func TestReadKeyFile_EmptyPath(t *testing.T) {
	_, err := ReadKeyFile("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestReadKeyFile_NonexistentFile(t *testing.T) {
	_, err := ReadKeyFile("/nonexistent/path/secret")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestReadKeyFile_RejectsGroupWritable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kek")
	// 0o660 — group writable (bit 0o020).
	if err := os.WriteFile(path, []byte("secret"), 0o660); err != nil {
		t.Fatal(err)
	}
	// Pode falhar no os.WriteFile se umask for mais restritivo; ignorar.
	if _, err := os.Stat(path); err != nil {
		t.Skipf("skipping: cannot create 0o660 file: %v", err)
	}
	_, err := ReadKeyFile(path)
	if err == nil {
		t.Fatal("expected error for 0660 file (group writable)")
	}
}
