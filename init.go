package main

import (
	"bufio"
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

	"github.com/flowercomputers/flop/api"
)

// starters are the projects `flop init` writes, one a kind: a small working
// bog on bog and axum, laid out as its AGENTS.md says. Each is a real cargo
// project under the name starterName, so it builds and is tested where it
// is; init writes one out under the new bog's name. Their files are named
// one by one, so nothing built beside them (target, data) is ever embedded.
//
//go:embed starters/api/Cargo.toml starters/api/Cargo.lock starters/api/.gitignore starters/api/AGENTS.md starters/api/README.md starters/api/src
//go:embed starters/web/Cargo.toml starters/web/Cargo.lock starters/web/.gitignore starters/web/AGENTS.md starters/web/README.md starters/web/src starters/web/web
var starters embed.FS

const starterName = "bog-starter"

// starterNamed are the starters' files that say the name
var starterNamed = map[string]bool{"Cargo.toml": true, "Cargo.lock": true, "AGENTS.md": true, "README.md": true, "index.html": true}

// A project's kind is which starter it begins as
const (
	kindAPI = "api" // an API
	kindWeb = "web" // an API with a page in front of it: its bog is public, as a browser cannot send a key
)

// How the API starter's guide says to deploy, and how it says so for a bog
// that is to be public. (The web starter's always says the second.)
const (
	deployPrivate = "flop bog deploy"
	deployPublic  = "flop bog deploy --access public"
	guidePrivate  = deployPrivate + "           # build it remotely and run it; waits until it is healthy"
	guidePublic   = deployPublic + "   # build it remotely and run it; a new bog is made public: anyone can call it"
)

// A project's name is its directory's, its cargo package's and its bog's: a
// bog name (lowercase letters, digits, dashes) that cargo takes too (it
// starts with a letter).
var projectName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

const projectNameRule = "lowercase letters, digits and dashes, starting with a letter, 63 at most"

// initOptions are what `flop init` is told. What it is not told it asks at
// a terminal, and takes the defaults for anywhere else.
type initOptions struct {
	Name   string
	Kind   string // api (the default) or web
	Access string // who may call the bog once deployed: private (the default) or public. A web app is public
}

// initialized is a new project
type initialized struct {
	Bog  string `json:"bog"`
	Kind string `json:"kind"`
	// Access is who may call the bog once it is deployed as Next says
	Access api.AccessMode `json:"access"`
	Dir    string         `json:"dir"`
	Files  []string       `json:"files"`
	Next   []string       `json:"next"` // what to run from here, in order
}

func (i initialized) human(w io.Writer) {
	what := "a small bog based store with an http api"
	if i.Kind == kindWeb {
		what = "a small web app, with a bog based store"
	}
	deploy := i.Next[len(i.Next)-1]
	fmt.Fprintf(w, "Made %s in ./%s: %s to build on (its AGENTS.md says how).\n\n", i.Bog, i.Bog, what)
	fmt.Fprintf(w, "  cd %s\n", i.Bog)
	fmt.Fprintf(w, "  %-*s  # run it here (needs Rust: https://rustup.rs)\n", len(deploy), "cargo run")
	fmt.Fprintf(w, "  %s  # run it on Flower Computer (needs no Rust)\n", deploy)
	if i.Access == api.AccessPublic {
		fmt.Fprintln(w, "\nDeployed that way it is public: anyone can call it, any method, without a key.")
	}
}

// Init writes a starter project to ./<name> and makes it a git repository:
// flop deploys a repository, from its root.
func Init(ctx context.Context, o *ui, opts initOptions) (initialized, error) {
	switch {
	case opts.Name != "" && !projectName.MatchString(opts.Name):
		return initialized{}, usage(fmt.Sprintf("%q cannot name a bog: %s", opts.Name, projectNameRule), "")
	case opts.Kind != "" && opts.Kind != kindAPI && opts.Kind != kindWeb:
		return initialized{}, usage("--kind is api or web", "")
	case opts.Access != "" && opts.Access != string(api.AccessPrivate) && opts.Access != string(api.AccessPublic):
		return initialized{}, usage("--access is private or public", "")
	case opts.Kind == kindWeb && opts.Access == string(api.AccessPrivate):
		return initialized{}, usage("a web app's bog is public: a browser cannot send a key, so a private bog's page would not load", "leave out --access, or make it --kind api")
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

	// what was not said is asked of a person at a terminal, and nobody else
	name, kind, access := opts.Name, opts.Kind, api.AccessMode(opts.Access)
	asking, in := o.canAsk && !o.json, bufio.NewReader(o.in)
	if name == "" {
		if !asking {
			return initialized{}, usage("flop init needs a name: the project's directory, its cargo package and its bog", "flop init <name> [--kind api|web] [--access private|public]")
		}
		for name == "" {
			fmt.Fprintf(o.err, "Its name (the project's directory, its cargo package and its bog): ")
			typed, err := answer(in)
			if err != nil {
				return initialized{}, err
			}
			if !projectName.MatchString(typed) {
				fmt.Fprintf(o.err, "A name is %s.\n", projectNameRule)
				continue
			}
			name = typed
		}
	}
	if kind == "" {
		kind = kindAPI
		if asking {
			if kind, err = choose(o.err, in, "What is it?", [][2]string{
				{kindAPI, "an API"},
				{kindWeb, "a web app: an API with a web page in front of it"},
			}); err != nil {
				return initialized{}, err
			}
		}
	}
	switch {
	case kind == kindWeb:
		access = api.AccessPublic
		if asking {
			o.say("A web app's bog is public: a browser cannot send a key, so anyone can access it.")
		}
	case access == "":
		access = api.AccessPrivate
		if asking {
			chosen, err := choose(o.err, in, "Who may call it, once it is deployed?", [][2]string{
				{string(api.AccessPrivate), "only callers with a key"},
				{string(api.AccessPublic), "anyone"},
			})
			if err != nil {
				return initialized{}, err
			}
			access = api.AccessMode(chosen)
		}
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

	deploy := deployPrivate
	if access == api.AccessPublic {
		deploy = deployPublic
	}
	var files []string
	from := "starters/" + kind
	err = fs.WalkDir(starters, from, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := starters.ReadFile(path)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(path, from+"/")
		if starterNamed[d.Name()] {
			b = []byte(strings.ReplaceAll(string(b), starterName, name))
		}
		if rel == "AGENTS.md" && kind == kindAPI && access == api.AccessPublic {
			b = []byte(strings.Replace(string(b), guidePrivate, guidePublic, 1))
		}
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
	return initialized{Bog: name, Kind: kind, Access: access, Dir: dir, Files: files, Next: []string{"cd " + name, "cargo run", deploy}}, nil
}

// answer reads one typed line. No line at all (stdin ended) is no answer:
// the flags say everything without one.
func answer(in *bufio.Reader) (string, error) {
	typed, err := in.ReadString('\n')
	if err != nil && typed == "" {
		return "", usage("no answer", "say it on the command line: flop init <name> [--kind api|web] [--access private|public]")
	}
	return strings.TrimSpace(typed), nil
}

// choose asks a numbered question, and returns the value of the option
// picked: by its number or its value, and the first by Enter alone.
func choose(w io.Writer, in *bufio.Reader, question string, options [][2]string) (string, error) {
	fmt.Fprintln(w, question)
	for i, option := range options {
		note := ""
		if i == 0 {
			note = " (default)"
		}
		fmt.Fprintf(w, "  %d. %s%s\n", i+1, option[1], note)
	}
	between := "to"
	if len(options) == 2 {
		between = "or"
	}
	for {
		fmt.Fprintf(w, "Choose 1 %s %d [1]: ", between, len(options))
		typed, err := answer(in)
		if err != nil {
			return "", err
		}
		if typed == "" {
			return options[0][0], nil
		}
		for i, option := range options {
			if typed == fmt.Sprint(i+1) || typed == option[0] {
				return option[0], nil
			}
		}
	}
}
