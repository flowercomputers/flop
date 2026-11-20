package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// starterFiles are what `flop init` writes: the test fails when the embed
// line and the starter directory part ways (a file added to one only)
var starterFiles = []string{".gitignore", "AGENTS.md", "Cargo.lock", "Cargo.toml", "README.md", "src/api.rs", "src/main.rs", "src/schema.rs"}

func TestInitWritesTheStarterUnderItsName(t *testing.T) {
	t.Chdir(t.TempDir())
	made, err := Init(context.Background(), "adder")
	if err != nil {
		t.Fatal(err)
	}
	if made.Bog != "adder" || filepath.Base(made.Dir) != "adder" || !slices.Equal(made.Files, starterFiles) ||
		!slices.Equal(made.Next, []string{"cd adder", "cargo run", "flop bog deploy"}) {
		t.Fatalf("made: %+v", made)
	}
	// every file is there, and none still says the starter's own name
	for _, f := range starterFiles {
		b, err := os.ReadFile(filepath.Join(made.Dir, f))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), starterName) {
			t.Errorf("%s still says %s", f, starterName)
		}
	}
	// the package, and so the bog `flop bog deploy` makes, is the name; the
	// lock file agrees, so a locked build takes it
	if pkg, err := cargoPackageName(filepath.Join(made.Dir, "Cargo.toml")); err != nil || pkg != "adder" {
		t.Fatalf("package: %q %v", pkg, err)
	}
	lock, _ := os.ReadFile(filepath.Join(made.Dir, "Cargo.lock"))
	if !strings.Contains(string(lock), "name = \"adder\"\nversion = \"0.1.0\"") {
		t.Fatal("Cargo.lock does not name the package")
	}
	// it is a repository of its own, which is what flop deploys: its files
	// are what a deploy would upload, and what cargo builds stays out
	if root, err := gitRoot(made.Dir); err != nil || filepath.Base(root) != "adder" {
		t.Fatalf("git root: %q %v", root, err)
	}
	writeFile(t, filepath.Join(made.Dir, "data"), "bog's data")
	tarball, err := sourceTarball(made.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := tarballNames(t, tarball); !slices.Equal(got, starterFiles) {
		t.Fatalf("a deploy would upload %v", got)
	}

	var out strings.Builder
	made.human(&out)
	for _, want := range []string{"Made adder in ./adder", "cd adder", "cargo run", "flop bog deploy"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("lacks %q:\n%s", want, out.String())
		}
	}
}

func TestInitRefuses(t *testing.T) {
	ctx := context.Background()
	t.Chdir(t.TempDir())
	for _, name := range []string{"", "9lives", "My-App", "my_app", "a/b", "..", strings.Repeat("a", 64)} {
		if _, err := Init(ctx, name); classify(err).Code != "usage" {
			t.Errorf("the name %q: %v", name, err)
		}
	}
	// a directory with something in it, or a file, is never written over; an
	// empty directory is taken
	if err := os.Mkdir("taken", 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, "taken/notes.txt", "mine")
	writeFile(t, "file", "mine")
	for _, name := range []string{"taken", "file"} {
		if _, err := Init(ctx, name); classify(err).Code != "conflict" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if b, _ := os.ReadFile("taken/notes.txt"); string(b) != "mine" {
		t.Fatal("init wrote into a directory that was not empty")
	}
	if err := os.Mkdir("empty", 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(ctx, "empty"); err != nil {
		t.Fatalf("an empty directory: %v", err)
	}
	// inside a repository (the one just made): flop deploys from a
	// repository's root, so a project here could not be deployed
	t.Chdir("empty")
	if _, err := Init(ctx, "inner"); classify(err).Code != "conflict" {
		t.Fatalf("inside a repository: %v", err)
	}
	if _, err := os.Stat("inner"); !os.IsNotExist(err) {
		t.Fatal("init wrote inside a repository")
	}
}

// tarballNames are the files in a source tarball, in its order
func tarballNames(t *testing.T, tarball []byte) []string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(tarball))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
	}
}

// the embedded starter is the starter directory's source, whole
func TestTheStarterIsEmbeddedWhole(t *testing.T) {
	var embedded []string
	err := fs.WalkDir(starter, "starter", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			embedded = append(embedded, strings.TrimPrefix(path, "starter/"))
		}
		return err
	})
	if err != nil || !slices.Equal(embedded, starterFiles) {
		t.Fatalf("embedded: %v %v", embedded, err)
	}
	// and nothing in the directory's source is left out of it (what cargo
	// builds there, target and data, is not source)
	var onDisk []string
	err = filepath.WalkDir("starter", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, "starter"+string(filepath.Separator)))
		if d.IsDir() {
			if rel == "target" || rel == "data" {
				return filepath.SkipDir
			}
			return nil
		}
		onDisk = append(onDisk, rel)
		return nil
	})
	if err != nil || !slices.Equal(onDisk, starterFiles) {
		t.Fatalf("on disk: %v %v", onDisk, err)
	}
}
