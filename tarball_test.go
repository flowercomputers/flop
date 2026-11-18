package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	args = append([]string{"-C", root, "-c", "user.email=test@test", "-c", "user.name=test"}, args...)
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSourceTarball(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	writeFile(t, filepath.Join(root, ".gitignore"), "ignored.txt\n")
	writeFile(t, filepath.Join(root, "tracked.txt"), "tracked")
	writeFile(t, filepath.Join(root, "ghost.txt"), "ghost")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")
	writeFile(t, filepath.Join(root, "untracked.txt"), "untracked")
	writeFile(t, filepath.Join(root, "ignored.txt"), "ignored")
	// deleted from the worktree but still in the index: must be skipped
	if err := os.Remove(filepath.Join(root, "ghost.txt")); err != nil {
		t.Fatal(err)
	}

	tarball, err := sourceTarball(root)
	if err != nil {
		t.Fatal(err)
	}

	gz, err := gzip.NewReader(bytes.NewReader(tarball))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got[hdr.Name] = true
	}

	want := []string{".gitignore", "tracked.txt", "untracked.txt"}
	if len(got) != len(want) {
		t.Errorf("got entries %v, want %v", got, want)
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("missing %s in tarball", name)
		}
	}
	if got["ignored.txt"] {
		t.Error("gitignored file made it into the tarball")
	}
	if got["ghost.txt"] {
		t.Error("worktree-deleted file made it into the tarball")
	}
}

func TestCargoPackageName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Cargo.toml")
	writeFile(t, path, "[workspace]\nmembers = []\n\n[package]\nedition = \"2021\"\nname = \"my-bog\" # comment\n\n[dependencies]\nname-alike = \"1\"\n")
	name, err := cargoPackageName(path)
	if err != nil {
		t.Fatal(err)
	}
	if name != "my-bog" {
		t.Errorf("got %q, want %q", name, "my-bog")
	}

	writeFile(t, path, "[workspace]\nmembers = [\"a\"]\n")
	if _, err := cargoPackageName(path); err == nil {
		t.Error("expected error for workspace-only Cargo.toml")
	}
}
