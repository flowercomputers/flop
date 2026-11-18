package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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
