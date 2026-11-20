package main

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// starter is the project `flop init` writes: a small working bog on bog and
// axum, laid out as its AGENTS.md says. It is a real cargo project under the
// name starterName, so it builds and is tested where it is; init writes it
// out under the new bog's name. Its files are named one by one, so nothing
// built beside them (target, data) is ever embedded.
//
//go:embed starter/Cargo.toml starter/Cargo.lock starter/.gitignore starter/AGENTS.md starter/README.md starter/src
var starter embed.FS

const starterName = "bog-starter"

// starterNamed are the starter's files that say its name
var starterNamed = map[string]bool{"Cargo.toml": true, "Cargo.lock": true, "AGENTS.md": true, "README.md": true}

// A project's name is its directory's, its cargo package's and its bog's: a
// bog name (lowercase letters, digits, dashes) that cargo takes too (it
// starts with a letter).
var projectName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// initialized is a new project
type initialized struct {
	Bog   string   `json:"bog"`
	Dir   string   `json:"dir"`
	Files []string `json:"files"`
	Next  []string `json:"next"` // what to run from here, in order
}

func (i initialized) human(w io.Writer) {
	fmt.Fprintf(w, "Made %s in ./%s: a small working bog to build on (its AGENTS.md says how).\n\n", i.Bog, i.Bog)
	fmt.Fprintf(w, "  cd %s\n", i.Bog)
	fmt.Fprintln(w, "  cargo run          # run it here (needs Rust: https://rustup.rs)")
	fmt.Fprintln(w, "  flop bog deploy    # run it on Flower Computer (needs no Rust)")
}

// Init writes the starter project to ./<name> and makes it a git repository:
// flop deploys a repository, from its root.
func Init(ctx context.Context, name string) (initialized, error) {
	if name == "" {
		return initialized{}, usage("flop init needs a name: the project's directory, its cargo package and its bog", "flop init <name>")
	}
	if !projectName.MatchString(name) {
		return initialized{}, usage(fmt.Sprintf("%q cannot name a bog: lowercase letters, digits and dashes, starting with a letter, 63 at most", name), "")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return initialized{}, fail("failed", "git is not installed: flop deploys a git repository", "install git, then run this again")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return initialized{}, err
	}
	if root, err := gitRoot(cwd); err == nil {
		return initialized{}, fail("conflict", fmt.Sprintf("this is inside a git repository (%s): flop deploys a repository from its root, so a project made here could not be deployed", root),
			"run it outside any repository")
	}
	dir := filepath.Join(cwd, name)
	switch info, err := os.Stat(dir); {
	case err == nil && !info.IsDir():
		return initialized{}, fail("conflict", dir+" is in the way: it is not a directory", "choose another name")
	case err == nil:
		entries, err := os.ReadDir(dir)
		if err != nil {
			return initialized{}, err
		}
		if len(entries) > 0 {
			return initialized{}, fail("conflict", dir+" is not empty", "choose another name, or empty it")
		}
	case !os.IsNotExist(err):
		return initialized{}, err
	}

	var files []string
	err = fs.WalkDir(starter, "starter", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := starter.ReadFile(path)
		if err != nil {
			return err
		}
		if starterNamed[d.Name()] {
			b = []byte(strings.ReplaceAll(string(b), starterName, name))
		}
		rel := strings.TrimPrefix(path, "starter/")
		to := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		files = append(files, rel)
		return os.WriteFile(to, b, 0o644)
	})
	if err != nil {
		return initialized{}, err
	}
	if out, err := exec.CommandContext(ctx, "git", "init", "-q", dir).CombinedOutput(); err != nil {
		return initialized{}, fmt.Errorf("the project is in %s, but git init failed there: %s", dir, strings.TrimSpace(string(out)))
	}
	return initialized{Bog: name, Dir: dir, Files: files, Next: []string{"cd " + name, "cargo run", "flop bog deploy"}}, nil
}
