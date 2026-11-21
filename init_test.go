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

	"github.com/flowercomputers/flop/api"
)

// starterFiles are what `flop init` writes, a kind: the tests fail when the
// embed lines and a starter's directory part ways (a file added to one only)
var starterFiles = map[string][]string{
	kindAPI: {".gitignore", "AGENTS.md", "Cargo.lock", "Cargo.toml", "README.md", "src/api.rs", "src/main.rs", "src/schema.rs"},
	kindWeb: {".gitignore", "AGENTS.md", "Cargo.lock", "Cargo.toml", "README.md", "src/api.rs", "src/main.rs", "src/schema.rs", "src/web.rs", "web/app.js", "web/index.html", "web/style.css"},
}

// initAt runs init in a new empty directory, as someone who cannot be asked
// anything unless typed says what they type
func initAt(t *testing.T, typed string, asJSON bool, opts initOptions) (initialized, string, error) {
	t.Helper()
	t.Chdir(t.TempDir())
	o, out, errs := testUI(typed, typed != "", asJSON)
	made, err := Init(context.Background(), o, opts)
	if out.Len() > 0 {
		t.Fatalf("stdout is the result's alone: %s", out.String())
	}
	return made, errs.String(), err
}

func TestInitWritesAStarterUnderItsName(t *testing.T) {
	for _, c := range []struct {
		kind   string
		access api.AccessMode
		deploy string
		says   string
	}{
		{kindAPI, api.AccessPrivate, "flop bog deploy", "a small bog based store with an http api"},
		{kindWeb, api.AccessPublic, "flop bog deploy --access public", "a small web app, with a bog based store"},
	} {
		t.Run(c.kind, func(t *testing.T) {
			made, asked, err := initAt(t, "", false, initOptions{Name: "adder", Kind: c.kind})
			if err != nil || asked != "" {
				t.Fatalf("%v, and asked: %s", err, asked)
			}
			if made.Bog != "adder" || made.Kind != c.kind || made.Access != c.access || filepath.Base(made.Dir) != "adder" ||
				!slices.Equal(made.Files, starterFiles[c.kind]) || !slices.Equal(made.Next, []string{"cd adder", "cargo run", c.deploy}) {
				t.Fatalf("made: %+v", made)
			}
			// every file is there, and none still says the starter's own name
			for _, f := range starterFiles[c.kind] {
				b, err := os.ReadFile(filepath.Join(made.Dir, f))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(b), starterName) {
					t.Errorf("%s still says %s", f, starterName)
				}
			}
			// the package, and so the bog `flop bog deploy` makes, is the name;
			// the lock file agrees, so a locked build takes it
			if pkg, err := cargoPackageName(filepath.Join(made.Dir, "Cargo.toml")); err != nil || pkg != "adder" {
				t.Fatalf("package: %q %v", pkg, err)
			}
			lock, _ := os.ReadFile(filepath.Join(made.Dir, "Cargo.lock"))
			if !strings.Contains(string(lock), "name = \"adder\"\nversion = \"0.1.0\"") {
				t.Fatal("Cargo.lock does not name the package")
			}
			// its guide deploys it as next does
			guide, _ := os.ReadFile(filepath.Join(made.Dir, "AGENTS.md"))
			if !strings.Contains(string(guide), "\n"+c.deploy+"  ") {
				t.Fatalf("the guide does not deploy with %q", c.deploy)
			}
			// it is a repository of its own, which is what flop deploys: its
			// files are what a deploy would upload, and what cargo builds
			// stays out
			if root, err := gitRoot(made.Dir); err != nil || filepath.Base(root) != "adder" {
				t.Fatalf("git root: %q %v", root, err)
			}
			writeFile(t, filepath.Join(made.Dir, "data"), "bog's data")
			tarball, err := sourceTarball(made.Dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := tarballNames(t, tarball); !slices.Equal(got, starterFiles[c.kind]) {
				t.Fatalf("a deploy would upload %v", got)
			}

			var out strings.Builder
			made.human(&out)
			for _, want := range []string{"Made adder in ./adder: " + c.says, "cd adder", "cargo run", c.deploy + "  #"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("lacks %q:\n%s", want, out.String())
				}
			}
			if strings.Contains(out.String(), "it is public") != (c.access == api.AccessPublic) {
				t.Fatalf("who may call it:\n%s", out.String())
			}
		})
	}
}

// An API that is to be public says so where its first deploy is read from:
// the next steps, and its guide. (Only a new bog's deploy takes the access.)
func TestInitCarriesAccessToTheFirstDeploy(t *testing.T) {
	// the line init rewrites is the API guide's, once
	guide, _ := starters.ReadFile("starters/api/AGENTS.md")
	if strings.Count(string(guide), "\n"+guidePrivate+"\n") != 1 {
		t.Fatalf("the API starter's guide does not say, once: %s", guidePrivate)
	}
	for _, c := range []struct {
		access         string
		deploy, in, no string
	}{
		{"public", deployPublic, guidePublic, guidePrivate},
		{"private", deployPrivate, guidePrivate, guidePublic},
		{"", deployPrivate, guidePrivate, guidePublic},
	} {
		made, _, err := initAt(t, "", false, initOptions{Name: "adder", Access: c.access})
		if err != nil {
			t.Fatal(err)
		}
		guide, _ := os.ReadFile(filepath.Join(made.Dir, "AGENTS.md"))
		if made.Kind != kindAPI || made.Next[2] != c.deploy || !strings.Contains(string(guide), "\n"+c.in+"\n") || strings.Contains(string(guide), c.no) {
			t.Errorf("--access %q: next %q, and the guide:\n%s", c.access, made.Next, guide)
		}
	}
}

// What init is not told it asks, of a person at a terminal and nobody else:
// flags answer everything, and without them an agent or a script gets the
// defaults (an API, private) and is asked nothing.
func TestInitAsksOnlyWhatItIsNotTold(t *testing.T) {
	for _, c := range []struct {
		name   string
		typed  string // "" is nobody to ask
		json   bool
		opts   initOptions
		bog    string
		kind   string
		access api.AccessMode
		asks   []string // what it asks, in order; none: it asks nothing
	}{
		{name: "everything", typed: "counter\n2\n", bog: "counter", kind: kindWeb, access: api.AccessPublic,
			asks: []string{"Its name", "What is it?", "A web app's bog is public"}},
		{name: "Enter takes the defaults", typed: "\n\n", opts: initOptions{Name: "counter"}, bog: "counter", kind: kindAPI, access: api.AccessPrivate,
			asks: []string{"What is it?", "1. an API (default)", "Who may call it", "1. only callers with a key (default)"}},
		{name: "a public API", typed: "1\n2\n", opts: initOptions{Name: "counter"}, bog: "counter", kind: kindAPI, access: api.AccessPublic,
			asks: []string{"What is it?", "Who may call it"}},
		{name: "an answer that is none is asked again", typed: "9lives\ncounter\n7\nweb\n", bog: "counter", kind: kindWeb, access: api.AccessPublic,
			asks: []string{"Its name", "A name is lowercase", "Its name", "Choose 1 or 2 [1]: Choose 1 or 2 [1]: "}},
		{name: "a kind named", typed: "2\n", opts: initOptions{Name: "counter", Kind: kindAPI}, bog: "counter", kind: kindAPI, access: api.AccessPublic,
			asks: []string{"Who may call it"}},
		{name: "an access named", typed: "\n", opts: initOptions{Name: "counter", Access: "public"}, bog: "counter", kind: kindAPI, access: api.AccessPublic,
			asks: []string{"What is it?"}},
		{name: "all named", typed: "never read\n", opts: initOptions{Name: "counter", Kind: kindAPI, Access: "private"}, bog: "counter", kind: kindAPI, access: api.AccessPrivate},
		{name: "a web app named", typed: "never read\n", opts: initOptions{Name: "counter", Kind: kindWeb}, bog: "counter", kind: kindWeb, access: api.AccessPublic,
			asks: []string{"A web app's bog is public"}},
		{name: "nobody to ask", opts: initOptions{Name: "counter"}, bog: "counter", kind: kindAPI, access: api.AccessPrivate},
		{name: "--json asks nobody", typed: "2\n2\n", json: true, opts: initOptions{Name: "counter"}, bog: "counter", kind: kindAPI, access: api.AccessPrivate},
	} {
		t.Run(c.name, func(t *testing.T) {
			made, asked, err := initAt(t, c.typed, c.json, c.opts)
			if err != nil || made.Bog != c.bog || made.Kind != c.kind || made.Access != c.access {
				t.Fatalf("%v: %+v\n%s", err, made, asked)
			}
			at := 0
			for _, want := range c.asks {
				i := strings.Index(asked[at:], want)
				if i < 0 {
					t.Fatalf("does not ask %q, in order:\n%s", want, asked)
				}
				at += i + len(want)
			}
			if len(c.asks) == 0 && asked != "" {
				t.Fatalf("asked:\n%s", asked)
			}
		})
	}
	// no name, and nobody to ask for one; or the answers run out
	for _, c := range []struct {
		typed string
		json  bool
		opts  initOptions
	}{
		{"", false, initOptions{}},
		{"counter\n", true, initOptions{}},
		{"counter\n", false, initOptions{}},
		{"\n", false, initOptions{Name: "counter"}},
	} {
		made, _, err := initAt(t, c.typed, c.json, c.opts)
		if classify(err).Code != "usage" || !strings.Contains(classify(err).Hint, "flop init <name> [--kind api|web] [--access private|public]") {
			t.Errorf("typed %q: %v", c.typed, err)
		}
		if _, statErr := os.Stat("counter"); !os.IsNotExist(statErr) || made.Dir != "" {
			t.Errorf("typed %q: a project was written", c.typed)
		}
	}
}

func TestInitRefuses(t *testing.T) {
	ctx := context.Background()
	t.Chdir(t.TempDir())
	o, _, _ := testUI("", false, false)
	for _, name := range []string{"9lives", "My-App", "my_app", "a/b", "..", strings.Repeat("a", 64)} {
		if _, err := Init(ctx, o, initOptions{Name: name}); classify(err).Code != "usage" {
			t.Errorf("the name %q: %v", name, err)
		}
	}
	for _, opts := range []initOptions{
		{Name: "ok", Kind: "spa"},
		{Name: "ok", Access: "open"},
		{Name: "ok", Kind: kindWeb, Access: "private"}, // its page could not load
	} {
		if _, err := Init(ctx, o, opts); classify(err).Code != "usage" {
			t.Errorf("%+v: %v", opts, err)
		}
	}
	if _, err := os.Stat("ok"); !os.IsNotExist(err) {
		t.Fatal("init wrote a project it refused")
	}
	// a directory with something in it, or a file, is never written over; an
	// empty directory is taken
	if err := os.Mkdir("taken", 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, "taken/notes.txt", "mine")
	writeFile(t, "file", "mine")
	for _, name := range []string{"taken", "file"} {
		if _, err := Init(ctx, o, initOptions{Name: name}); classify(err).Code != "conflict" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if b, _ := os.ReadFile("taken/notes.txt"); string(b) != "mine" {
		t.Fatal("init wrote into a directory that was not empty")
	}
	if err := os.Mkdir("empty", 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(ctx, o, initOptions{Name: "empty"}); err != nil {
		t.Fatalf("an empty directory: %v", err)
	}
	// inside a repository (the one just made): flop deploys from a
	// repository's root, so a project here could not be deployed. It says so
	// before asking anything
	t.Chdir("empty")
	asker, _, asked := testUI("inner\n", true, false)
	if _, err := Init(ctx, asker, initOptions{}); classify(err).Code != "conflict" || asked.Len() > 0 {
		t.Fatalf("inside a repository: %v, having asked: %s", err, asked.String())
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

// each embedded starter is its directory's source, whole
func TestTheStartersAreEmbeddedWhole(t *testing.T) {
	for kind, files := range starterFiles {
		var embedded []string
		err := fs.WalkDir(starters, "starters/"+kind, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				embedded = append(embedded, strings.TrimPrefix(path, "starters/"+kind+"/"))
			}
			return err
		})
		if err != nil || !slices.Equal(embedded, files) {
			t.Fatalf("%s, embedded: %v %v", kind, embedded, err)
		}
		// and nothing in the directory's source is left out of it (what cargo
		// builds there, target and data, is not source)
		var onDisk []string
		dir := filepath.Join("starters", kind)
		err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel := filepath.ToSlash(strings.TrimPrefix(path, dir+string(filepath.Separator)))
			if d.IsDir() {
				if rel == "target" || rel == "data" {
					return filepath.SkipDir
				}
				return nil
			}
			onDisk = append(onDisk, rel)
			return nil
		})
		if err != nil || !slices.Equal(onDisk, files) {
			t.Fatalf("%s, on disk: %v %v", kind, onDisk, err)
		}
	}
	if kinds, _ := os.ReadDir("starters"); len(kinds) != len(starterFiles) {
		t.Fatalf("starters holds %d starters, and %d are embedded", len(kinds), len(starterFiles))
	}
}

// The web starter is the API starter with a page in front: what the two
// share is the same in both, so a change to one that misses the other fails
// here. (The lock file with them: both are built from one set of crates.)
func TestTheStartersShareWhatTheyShare(t *testing.T) {
	read := func(kind, file string) string {
		b, err := starters.ReadFile("starters/" + kind + "/" + file)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, file := range []string{".gitignore", "Cargo.lock", "Cargo.toml", "README.md", "src/api.rs", "src/schema.rs"} {
		if read(kindAPI, file) != read(kindWeb, file) {
			t.Errorf("%s differs between the starters", file)
		}
	}
	// a section of a guide: from its heading to the next
	section := func(kind, heading string) string {
		_, rest, found := strings.Cut(read(kind, "AGENTS.md"), "\n## "+heading+"\n")
		if !found {
			t.Fatalf("the %s starter's guide has no section %q", kind, heading)
		}
		body, _, _ := strings.Cut(rest, "\n## ")
		return body
	}
	for _, heading := range []string{"How it is built", "Changing the schema", "Not here yet", "More"} {
		if section(kindAPI, heading) != section(kindWeb, heading) {
			t.Errorf("the guides' section %q differs between the starters", heading)
		}
	}
}
