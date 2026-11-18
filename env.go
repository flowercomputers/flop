package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"

	"github.com/flowercomputers/flop/api"
)

func (c *Client) ListBogEnv(ctx context.Context, bog string) (api.EnvResponse, error) {
	var out api.EnvResponse
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/bogs/"+url.PathEscape(bog)+"/env", nil, &out)
	return out, err
}

func (c *Client) SetBogEnv(ctx context.Context, bog string, req api.SetEnvRequest) (api.EnvResponse, error) {
	var out api.EnvResponse
	err := c.doJSON(ctx, http.MethodPut, "/api/v1/bogs/"+url.PathEscape(bog)+"/env", req, &out)
	return out, err
}

func (c *Client) UnsetBogEnv(ctx context.Context, bog, key string, noRestart bool) (api.EnvResponse, error) {
	path := "/api/v1/bogs/" + url.PathEscape(bog) + "/env/" + url.PathEscape(key)
	if noRestart {
		path += "?no_restart=true"
	}
	var out api.EnvResponse
	err := c.doJSON(ctx, http.MethodDelete, path, nil, &out)
	return out, err
}

// envList is a bog's variables and secrets (secrets' values never)
type envList struct {
	Bog  string       `json:"bog"`
	Vars []api.EnvVar `json:"vars"`
}

func (l envList) human(out io.Writer) {
	if len(l.Vars) == 0 {
		fmt.Fprintf(out, "No variables or secrets. Set them with `flop bog env set %s KEY=value` (variables) or `flop bog env set --secret %s KEY` (secrets).\n", l.Bog, l.Bog)
		return
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tKIND\tVALUE\tSET\tBY")
	for _, v := range l.Vars {
		kind, value := "variable", v.Value
		if v.Secret {
			kind, value = "secret", "(hidden)"
		}
		// one line per variable in the table
		value = strings.ReplaceAll(value, "\n", `\n`)
		by := "-"
		if v.SetBy != nil {
			by = v.SetBy.Email
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", v.Name, kind, value, ago(v.SetAt), by)
	}
	tw.Flush()
}

func EnvList(ctx context.Context, bog string) (envList, error) {
	client, err := loggedInClient()
	if err != nil {
		return envList{}, err
	}
	return listEnv(ctx, client, bog)
}

func listEnv(ctx context.Context, client *Client, bog string) (envList, error) {
	if bog == "" {
		return envList{}, usage("env list needs a bog", "flop bog env list <bog>")
	}
	resp, err := client.ListBogEnv(ctx, bog)
	if err != nil {
		return envList{}, fmt.Errorf("env: %w", err)
	}
	return envList{Bog: bog, Vars: resp.Vars}, nil
}

// envChanged is what env set or env unset did, and when the bog runs with it
type envChanged struct {
	Bog     string   `json:"bog"`
	Set     []string `json:"set,omitempty"`     // names set
	Removed []string `json:"removed,omitempty"` // names removed
	// Applies: "now" (the bog restarts with it), "next_deploy" (--no-restart)
	// or "when_deployed" (nothing runs it now)
	Applies string `json:"applies"`
}

func (c envChanged) human(w io.Writer) {
	if len(c.Set) > 0 {
		fmt.Fprintf(w, "Set %s for %s", strings.Join(c.Set, ", "), c.Bog)
	} else {
		fmt.Fprintf(w, "Removed %s from %s", strings.Join(c.Removed, ", "), c.Bog)
	}
	switch c.Applies {
	case "now":
		fmt.Fprintln(w, "; it restarts with them now.")
	case "next_deploy":
		fmt.Fprintln(w, "; saved for its next deploy.")
	default:
		fmt.Fprintln(w, "; it starts with them when deployed.")
	}
}

func applies(restarting, noRestart bool) string {
	switch {
	case restarting:
		return "now"
	case noRestart:
		return "next_deploy"
	default:
		return "when_deployed"
	}
}

func EnvSet(ctx context.Context, o *ui, bog string, args []string, secret, noRestart bool) (envChanged, error) {
	client, err := loggedInClient()
	if err != nil {
		return envChanged{}, err
	}
	return setEnv(ctx, client, o, bog, args, secret, noRestart)
}

// setEnv sets variables (KEY=value ...) or one secret (KEY, its value from
// stdin: typed without echo at a terminal, else read whole, one trailing
// newline dropped). A secret is never taken from the command line: it would
// stay in the shell's history.
func setEnv(ctx context.Context, client *Client, o *ui, bog string, args []string, secret, noRestart bool) (envChanged, error) {
	if bog == "" || len(args) == 0 {
		return envChanged{}, usage("env set needs a bog and what to set", "flop bog env set <bog> KEY=value ..., or flop bog env set --secret <bog> KEY (its value on stdin)")
	}
	vars := map[string]api.EnvValue{}
	if secret {
		if len(args) != 1 || strings.Contains(args[0], "=") {
			return envChanged{}, usage("a secret's value is never given on the command line (your shell would keep it)",
				fmt.Sprintf("flop bog env set --secret %s KEY, then type or paste it, or pipe it in: `... < file`", bog))
		}
		var value string
		if o.inIsTTY && o.canAsk {
			fmt.Fprintf(o.err, "%s (typed text is not shown): ", args[0])
			raw, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(o.err)
			if err != nil {
				return envChanged{}, err
			}
			value = string(raw)
		} else if o.inIsTTY {
			return envChanged{}, usage("a secret's value comes on stdin, and flop may not ask for it (--no-input)",
				fmt.Sprintf("pipe it in: `flop bog env set --secret %s %s < file`", bog, args[0]))
		} else {
			raw, err := io.ReadAll(io.LimitReader(o.in, 64<<10))
			if err != nil {
				return envChanged{}, err
			}
			value = strings.TrimSuffix(string(raw), "\n")
		}
		vars[args[0]] = api.EnvValue{Value: value, Secret: true}
	} else {
		for _, a := range args {
			k, v, ok := strings.Cut(a, "=")
			if !ok {
				return envChanged{}, usage(fmt.Sprintf("%q: variables are KEY=value", a), "for a secret: flop bog env set --secret <bog> KEY")
			}
			vars[k] = api.EnvValue{Value: v}
		}
	}
	resp, err := client.SetBogEnv(ctx, bog, api.SetEnvRequest{Vars: vars, NoRestart: noRestart})
	if err != nil {
		return envChanged{}, fmt.Errorf("env set: %w", err)
	}
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	slices.Sort(names)
	return envChanged{Bog: bog, Set: names, Applies: applies(resp.Restarting, noRestart)}, nil
}

func EnvUnset(ctx context.Context, bog string, keys []string, noRestart bool) (envChanged, error) {
	client, err := loggedInClient()
	if err != nil {
		return envChanged{}, err
	}
	return unsetEnv(ctx, client, bog, keys, noRestart)
}

// unsetEnv removes some; the bog restarts once, with the last
func unsetEnv(ctx context.Context, client *Client, bog string, keys []string, noRestart bool) (envChanged, error) {
	if bog == "" || len(keys) == 0 {
		return envChanged{}, usage("env unset needs a bog and names", "flop bog env unset <bog> KEY ...")
	}
	var restarting bool
	for i, k := range keys {
		resp, err := client.UnsetBogEnv(ctx, bog, k, noRestart || i < len(keys)-1)
		if err != nil {
			return envChanged{}, fmt.Errorf("env unset %s: %w", k, err)
		}
		restarting = resp.Restarting
	}
	return envChanged{Bog: bog, Removed: keys, Applies: applies(restarting, noRestart)}, nil
}
