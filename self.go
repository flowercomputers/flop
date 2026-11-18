package main

import (
	_ "embed"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/urfave/cli/v3"
)

// versionInfo is flop's build and the spade it talks to
type versionInfo struct {
	Version string `json:"version"` // the release, e.g. 0.1.0; "dev" for any other build
	Commit  string `json:"commit"`  // the git commit flop was built from, -dirty for local changes
	Go      string `json:"go"`
	API     string `json:"api"`
}

func (v versionInfo) human(w io.Writer) {
	fmt.Fprintf(w, "flop %s (%s, %s), talking to %s\n", v.Version, v.Commit, v.Go, v.API)
}

// userAgent names this flop in every request, to spade (its request log) and
// to its downloads (their access logs): flop/0.2.0 (darwin/arm64), flop/dev
// for a build from source
func userAgent() string {
	return fmt.Sprintf("flop/%s (%s/%s)", appVersion, runtime.GOOS, runtime.GOARCH)
}

func Version() versionInfo {
	v := versionInfo{Version: appVersion, Commit: "unknown", API: apiBaseURL()}
	if info, ok := debug.ReadBuildInfo(); ok {
		v.Go = info.GoVersion
		dirty := false
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				v.Commit = s.Value[:min(len(s.Value), 12)]
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if dirty {
			v.Commit += "-dirty"
		}
	}
	return v
}

// commandList is every command, for a program planning what to run
type commandList struct {
	Commands  []commandInfo  `json:"commands"`
	Global    []flagInfo     `json:"global_flags"`
	ExitCodes map[string]int `json:"exit_codes"` // by error code; 0 is success
}

type commandInfo struct {
	Name    string     `json:"name"` // as typed after flop: "bog deploy"
	Usage   string     `json:"usage"`
	Args    string     `json:"args,omitempty"`
	Changes bool       `json:"changes"` // false: it only reads
	Example string     `json:"example,omitempty"`
	Flags   []flagInfo `json:"flags,omitempty"`
}

type flagInfo struct {
	Name     string   `json:"name"`
	Aliases  []string `json:"aliases,omitempty"`
	Type     string   `json:"type"` // string, bool, int, duration
	Required bool     `json:"required,omitempty"`
	Default  string   `json:"default,omitempty"`
	Usage    string   `json:"usage"`
}

func (l commandList) human(w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "COMMAND\tCHANGES\tUSAGE")
	for _, c := range l.Commands {
		changes := "-"
		if c.Changes {
			changes = "yes"
		}
		fmt.Fprintf(tw, "flop %s\t%s\t%s\n", strings.TrimSpace(c.Name+" "+c.Args), changes, c.Usage)
	}
	tw.Flush()
}

// Commands walks the command tree: every command that runs something
func Commands(root *cli.Command) commandList {
	out := commandList{Global: flags(root.Flags), ExitCodes: exitCodes}
	var walk func(prefix string, cmds []*cli.Command)
	walk = func(prefix string, cmds []*cli.Command) {
		for _, c := range cmds {
			if c.Name == "help" {
				continue // the library's own
			}
			name := strings.TrimSpace(prefix + " " + c.Name)
			if c.Action != nil {
				info := commandInfo{Name: name, Usage: c.Usage, Args: c.ArgsUsage, Flags: flags(c.Flags)}
				if c.Metadata != nil {
					info.Changes, _ = c.Metadata["changes"].(bool)
					info.Example, _ = c.Metadata["example"].(string)
				}
				out.Commands = append(out.Commands, info)
			}
			walk(name, c.Commands)
		}
	}
	walk("", root.Commands)
	sort.Slice(out.Commands, func(i, j int) bool { return out.Commands[i].Name < out.Commands[j].Name })
	return out
}

func flags(fs []cli.Flag) []flagInfo {
	var out []flagInfo
	for _, f := range fs {
		names := f.Names()
		fi := flagInfo{Name: names[0], Aliases: names[1:]}
		if d, ok := f.(cli.DocGenerationFlag); ok {
			fi.Usage = d.GetUsage()
			fi.Type = strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(d.TypeName(), "*cli."), "Flag"))
			if d.TakesValue() {
				fi.Default = d.GetValue()
			}
		}
		if fi.Type == "" {
			fi.Type = "bool"
		}
		if r, ok := f.(cli.RequiredFlag); ok {
			fi.Required = r.IsRequired()
		}
		out = append(out, fi)
	}
	return out
}

//go:embed AGENTS.md
var agentsGuide string

// guide is flop's guide for scripts and agents
type guide struct {
	Markdown string `json:"markdown"`
}

func (g guide) human(w io.Writer) { io.WriteString(w, g.Markdown) }
