package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

// Every command returns one result. With --json it is printed as JSON; people
// see it through its human method, drawn from the same fields, so the two
// never say different things. In both modes stdout carries the result and
// nothing else; progress, build output and prompts go to stderr
// (AGENTS.md).
type result interface {
	human(w io.Writer)
}

// ui is how a command talks: where its result and its narration go, and
// whether it may ask a person anything.
type ui struct {
	json    bool      // --json or FLOP_OUTPUT=json
	out     io.Writer // the result
	err     io.Writer // everything else
	in      io.Reader
	tty     bool // stdout is a terminal: spinners and badges
	canAsk  bool // stdin is a terminal and --no-input is off
	inIsTTY bool
}

func newUI(cmd *cli.Command) *ui {
	stdin := term.IsTerminal(int(os.Stdin.Fd()))
	o := &ui{
		json:    cmd.Bool("json") || os.Getenv("FLOP_OUTPUT") == "json",
		out:     os.Stdout,
		err:     os.Stderr,
		in:      os.Stdin,
		inIsTTY: stdin,
	}
	o.tty = !o.json && term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	o.canAsk = stdin && !cmd.Bool("no-input") && os.Getenv("FLOP_NO_INPUT") == ""
	return o
}

// say narrates: a line for a person, on stderr in both modes.
func (o *ui) say(format string, args ...any) {
	fmt.Fprintf(o.err, format+"\n", args...)
}

// emit prints a command's result.
func (o *ui) emit(r result) error {
	if r == nil {
		return nil
	}
	if o.json {
		return json.NewEncoder(o.out).Encode(r)
	}
	r.human(o.out)
	return nil
}

// event prints one item of a stream: a JSON line, or the item's human form.
func (o *ui) event(r result) {
	if o.json {
		b, _ := json.Marshal(r)
		fmt.Fprintf(o.out, "%s\n", b)
		return
	}
	r.human(o.out)
}

// command adapts a flop command to the CLI: it gets the command's io, and
// its result is printed for it.
func command(fn func(ctx context.Context, cmd *cli.Command, o *ui) (result, error)) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		o := newUI(cmd)
		r, err := fn(ctx, cmd, o)
		if err != nil {
			return err
		}
		return o.emit(r)
	}
}

// globalFlags are taken by every command
func globalFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{Name: "json", Usage: "print the result as JSON on stdout (errors as JSON on stderr); also FLOP_OUTPUT=json"},
		&cli.BoolFlag{Name: "no-input", Usage: "never ask anything (also without a terminal, or FLOP_NO_INPUT=1)"},
	}
}

// printError prints a failure: JSON on stderr with --json, else a line and
// its hint.
func printError(w io.Writer, asJSON bool, e *cliError) {
	if asJSON {
		json.NewEncoder(w).Encode(map[string]*cliError{"error": e})
		return
	}
	fmt.Fprintf(w, "error: %s\n", e.Message)
	if e.Hint != "" {
		fmt.Fprintf(w, "hint: %s\n", e.Hint)
	}
}

// jsonRequested reads --json before the command line is parsed, for errors
// the parser itself reports
func jsonRequested(args []string) bool {
	if os.Getenv("FLOP_OUTPUT") == "json" {
		return true
	}
	for _, a := range args {
		if a == "--json" || a == "-json" || strings.HasPrefix(a, "--json=true") {
			return true
		}
		if a == "--" {
			break
		}
	}
	return false
}
