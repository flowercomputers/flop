package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"
)

var (
	builtOnce sync.Once
	builtBin  string
	builtErr  error
)

// the flop binary, built once for every test that runs it
func flopBinary(t *testing.T) string {
	t.Helper()
	builtOnce.Do(func() {
		dir, err := os.MkdirTemp("", "flop-test")
		if err != nil {
			builtErr = err
			return
		}
		builtBin = filepath.Join(dir, "flop")
		if out, err := exec.Command("go", "build", "-o", builtBin, ".").CombinedOutput(); err != nil {
			builtErr = errors.New(string(out))
		}
	})
	if builtErr != nil {
		t.Fatal(builtErr)
	}
	return builtBin
}

type ran struct {
	stdout, stderr string
	code           int
}

// runFlop runs the binary against spade with an organization token, as a script
// would; env adds to (or with a "KEY=" entry, clears) the environment
func runFlop(t *testing.T, spade string, stdin string, env []string, args ...string) ran {
	t.Helper()
	cmd := exec.Command(flopBinary(t), args...)
	cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+t.TempDir(), "FLOP_API_BASE_URL="+spade,
		"FLOP_TOKEN=fp_test", "FLOP_ORGANIZATION_ID=flower", "FLOP_OUTPUT=")
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	err := cmd.Run()
	var exit *exec.ExitError
	r := ran{stdout: out.String(), stderr: errs.String()}
	if errors.As(err, &exit) {
		r.code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return r
}

// jsonError is the error a failed command printed on stderr with --json
func jsonError(t *testing.T, r ran) cliError {
	t.Helper()
	var e struct{ Error cliError }
	if err := json.Unmarshal([]byte(r.stderr), &e); err != nil {
		t.Fatalf("stderr is not a JSON error: %v\n%s", err, r.stderr)
	}
	return e.Error
}

func TestTheBinaryDescribesItself(t *testing.T) {
	r := runFlop(t, "http://127.0.0.1:1", "", nil, "commands", "--json")
	var l commandList
	if r.code != 0 || json.Unmarshal([]byte(r.stdout), &l) != nil {
		t.Fatalf("commands: %+v", r)
	}
	byName := map[string]commandInfo{}
	for _, c := range l.Commands {
		byName[c.Name] = c
	}
	if d, ok := byName["bog deploy"]; !ok || !d.Changes || d.Example == "" || len(d.Flags) == 0 {
		t.Fatalf("bog deploy: %+v", d)
	}
	if s := byName["bog status"]; s.Changes || s.Args != "<bog>" {
		t.Fatalf("bog status: %+v", s)
	}
	// every command says whether it changes anything, and shows an example
	for _, c := range l.Commands {
		if c.Example == "" {
			t.Errorf("%s has no example", c.Name)
		}
	}
	if l.ExitCodes["not_found"] != 4 || len(l.Global) < 2 {
		t.Fatalf("exit codes and global flags: %+v %+v", l.ExitCodes, l.Global)
	}
	if r := runFlop(t, "http://127.0.0.1:1", "", nil, "version", "--json"); r.code != 0 || !strings.Contains(r.stdout, `"api":"http://127.0.0.1:1"`) {
		t.Fatalf("version: %+v", r)
	}
	if r := runFlop(t, "http://127.0.0.1:1", "", nil, "docs"); r.code != 0 || !strings.HasPrefix(r.stdout, "# flop") {
		t.Fatalf("docs: %+v", r)
	}
}

// The guide, the README and flop's own hints and help name commands and
// flags: each must be one flop has, so none of them sends anyone to something
// that isn't there.
func TestEveryCommandNamedExists(t *testing.T) {
	root := newApp()
	texts := map[string]string{}
	files, _ := filepath.Glob("*.go")
	for _, f := range append(files, "AGENTS.md", "README.md") {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		texts[f] = string(b)
	}
	// `flop ...` in backticks anywhere, and a command line in a code block
	// (at its start, or after a pipe), up to its comment
	span := regexp.MustCompile("`flop ([^`\n]*)`")
	line := regexp.MustCompile(`(?m)^(?:[^#\n]*\| )?flop ([^#\n]*)`)
	fence := regexp.MustCompile("(?s)```.*?```")
	named := 0
	for file, text := range texts {
		mentions := span.FindAllStringSubmatch(text, -1)
		for _, block := range fence.FindAllString(text, -1) {
			mentions = append(mentions, line.FindAllStringSubmatch(block, -1)...)
		}
		for _, m := range mentions {
			named++
			if err := commandExists(root, strings.Fields(m[1])); err != nil {
				t.Errorf("%s: `flop %s`: %v", file, strings.TrimSpace(m[1]), err)
			}
		}
	}
	if named < 40 {
		t.Fatalf("only %d commands found in the guide and the source: the patterns no longer match", named)
	}
	// each command's example is of that command
	for _, c := range Commands(root).Commands {
		if !strings.Contains(c.Example, "flop "+c.Name) {
			t.Errorf("%s: its example is %q", c.Name, c.Example)
		}
	}
}

// commandExists: words (what follows "flop") name a command flop has, with
// flags it takes; anything else in them is an argument
func commandExists(root *cli.Command, words []string) error {
	if len(words) == 0 || strings.HasPrefix(words[0], "<") || strings.HasPrefix(words[0], "-") {
		return nil // flop itself, or any command: `flop <command> --help`
	}
	cmd := root
	for len(words) > 0 {
		var next *cli.Command
		for _, c := range cmd.Commands {
			if c.Name == words[0] {
				next = c
			}
		}
		if next == nil {
			break
		}
		cmd, words = next, words[1:]
	}
	switch {
	case cmd == root:
		return errors.New("no such command")
	case cmd.Action == nil && len(words) > 0 && !strings.HasPrefix(words[0], "-") && !strings.HasPrefix(words[0], "<"):
		return errors.New("no " + words[0] + " in " + cmd.Name)
	}
	takes := map[string]bool{"help": true, "h": true}
	for _, f := range append(cmd.Flags, root.Flags...) {
		for _, n := range f.Names() {
			takes[n] = true
		}
	}
	for _, w := range words {
		name, _, _ := strings.Cut(strings.TrimLeft(w, "-"), "=")
		if strings.HasPrefix(w, "-") && name != "" && !takes[name] {
			return errors.New(cmd.Name + " takes no " + w)
		}
	}
	return nil
}

// the guide's table of exit statuses is flop's
func TestTheGuideHasEveryExitStatus(t *testing.T) {
	row := regexp.MustCompile("(?m)^\\| (\\d) \\| (`[^|]*) \\|")
	code := regexp.MustCompile("`([a-z_]+)`")
	seen := map[string]bool{}
	for _, m := range row.FindAllStringSubmatch(agentsGuide, -1) {
		exit, _ := strconv.Atoi(m[1])
		for _, c := range code.FindAllStringSubmatch(m[2], -1) {
			seen[c[1]] = true
			if exitCodes[c[1]] != exit {
				t.Errorf("the guide says %s exits %d; flop exits %d", c[1], exit, exitCodes[c[1]])
			}
		}
	}
	for c := range exitCodes {
		if !seen[c] {
			t.Errorf("the guide's table lacks %s", c)
		}
	}
}
