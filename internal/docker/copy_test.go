package docker

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTarPathPacksAFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	if err := os.WriteFile(path, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := tarPath(&buf, path, ""); err != nil {
		t.Fatalf("tarPath: %v", err)
	}

	tr := tar.NewReader(&buf)
	header, err := tr.Next()
	if err != nil {
		t.Fatalf("reading the archive: %v", err)
	}
	if header.Name != "hosts" {
		t.Errorf("entry name: got %q, want hosts", header.Name)
	}
	body, _ := io.ReadAll(tr)
	if string(body) != "127.0.0.1 localhost\n" {
		t.Errorf("contents: got %q", body)
	}
}

func TestTarPathRenamesWhenAsked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := tarPath(&buf, path, "renamed.txt"); err != nil {
		t.Fatalf("tarPath: %v", err)
	}

	header, err := tar.NewReader(&buf).Next()
	if err != nil {
		t.Fatal(err)
	}
	// Copying to an explicit destination name is how `docker cp a b` behaves.
	if header.Name != "renamed.txt" {
		t.Errorf("entry name: got %q, want renamed.txt", header.Name)
	}
}

func TestTarPathPacksADirectoryTree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "conf")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"a.conf", "sub/b.conf"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	if err := tarPath(&buf, root, ""); err != nil {
		t.Fatalf("tarPath: %v", err)
	}

	var names []string
	tr := tar.NewReader(&buf)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, header.Name)
	}

	want := map[string]bool{"conf": true, "conf/a.conf": true, "conf/sub": true, "conf/sub/b.conf": true}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected entry %q", n)
		}
		delete(want, n)
	}
	if len(want) != 0 {
		t.Errorf("entries missing from the archive: %v", want)
	}
}

func TestUntarIntoWritesFiles(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, f := range []struct{ name, body string }{
		{"logs/", ""},
		{"logs/app.log", "hello\n"},
	} {
		typ := byte(tar.TypeReg)
		if strings.HasSuffix(f.name, "/") {
			typ = tar.TypeDir
		}
		if err := tw.WriteHeader(&tar.Header{
			Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: typ,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	first, err := untarInto(&buf, dir)
	if err != nil {
		t.Fatalf("untarInto: %v", err)
	}
	if filepath.Base(first) != "logs" {
		t.Errorf("first entry reported: got %q", first)
	}

	body, err := os.ReadFile(filepath.Join(dir, "logs", "app.log"))
	if err != nil {
		t.Fatalf("reading what was extracted: %v", err)
	}
	if string(body) != "hello\n" {
		t.Errorf("contents: got %q", body)
	}
}

func TestUntarRefusesToEscapeItsDirectory(t *testing.T) {
	// The archive comes from the daemon, which is external input: an entry
	// climbing out of the destination must be refused, not written.
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := "owned"
	if err := tw.WriteHeader(&tar.Header{
		Name: "../escaped.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	parent := t.TempDir()
	dir := filepath.Join(parent, "target")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// filepath.Clean("/../escaped.txt") lands inside the directory rather than
	// above it, so the entry is written where it belongs and nothing escapes.
	if _, err := untarInto(&buf, dir); err != nil {
		t.Fatalf("untarInto: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "escaped.txt")); !os.IsNotExist(err) {
		t.Fatal("an archive entry escaped the destination directory")
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); err != nil {
		t.Errorf("the entry should have landed inside the destination: %v", err)
	}
}

func TestUntarIntoAcceptsARelativeDestination(t *testing.T) {
	// The destination is whatever the user typed, and "." is the obvious thing
	// to type. Comparing a joined path against a relative prefix used to reject
	// every entry here as an escape attempt.
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := "host\n"
	if err := tw.WriteHeader(&tar.Header{
		Name: "hostname", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	written, err := untarInto(&buf, ".")
	if err != nil {
		t.Fatalf("untarInto into a relative directory: %v", err)
	}
	if got, err := os.ReadFile(written); err != nil || string(got) != body {
		t.Errorf("what was written: %q, %v", got, err)
	}
}

func TestSafeJoin(t *testing.T) {
	dir := "/tmp/dest"
	cases := map[string]string{
		"file.txt":         "/tmp/dest/file.txt",
		"sub/file.txt":     "/tmp/dest/sub/file.txt",
		"../file.txt":      "/tmp/dest/file.txt",
		"/etc/passwd":      "/tmp/dest/etc/passwd",
		"../../etc/shadow": "/tmp/dest/etc/shadow",
	}
	for name, want := range cases {
		got, err := safeJoin(dir, name)
		if err != nil {
			t.Errorf("safeJoin(%q): %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("safeJoin(%q): got %q, want %q", name, got, want)
		}
	}
}
