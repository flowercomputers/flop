package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/urfave/cli/v3"
)

func main() {
	app := newApp()
	prepare(app)
	check := startUpdateCheck() // while the command runs (updates.go)
	code := 0
	if err := app.Run(context.Background(), os.Args); err != nil {
		e := classify(err)
		printError(os.Stderr, jsonRequested(os.Args[1:]), e)
		code = e.exit()
	}
	check.notify(os.Stderr, jsonRequested(os.Args[1:]))
	os.Exit(code)
}

// of adapts a command's typed result to the result every command returns
func of[T result](v T, err error) (result, error) {
	if err != nil {
		return nil, err
	}
	return v, nil
}

// changes marks a command that changes something (flop commands --json says
// which do, for an agent deciding what is safe to run)
func changes(example string) map[string]any {
	return map[string]any{"changes": true, "example": example}
}

func reads(example string) map[string]any {
	return map[string]any{"changes": false, "example": example}
}

func newApp() *cli.Command {
	return &cli.Command{
		Name:  "flop",
		Usage: "deploy and run bogs on Flower Computer",
		Description: "Every command can answer in JSON (--json): its result on stdout, errors as JSON on stderr,\n" +
			"and an exit status by kind of failure. `flop docs` is the guide for scripts and agents;\n" +
			"`flop commands --json` lists every command.",
		Flags: globalFlags(),
		// main prints errors (as JSON with --json) and picks the exit status
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Commands: []*cli.Command{
			{
				Name:      "init",
				Usage:     "start a project: a small working bog in ./<name>, ready to run and to deploy. At a terminal it asks for what it is not told",
				ArgsUsage: "[name]",
				Metadata:  changes("flop init notes --kind web --json"),
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "kind", Usage: "what it starts as: api (the default), or web: an API with a page in front of it, whose bog is public"},
					&cli.StringFlag{Name: "access", Usage: "who may call the bog once deployed: private (every request needs a key; the default) or public (anyone, any method)"},
				},
				Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
					return of(Init(ctx, o, initOptions{Name: cmd.Args().First(), Kind: cmd.String("kind"), Access: cmd.String("access")}))
				}),
			},
			{
				Name:  "bog",
				Usage: "build, deploy and run bogs",
				Commands: []*cli.Command{
					{
						Name:     "deploy",
						Usage:    "build the current project and run it (a rollout if the bog is already deployed)",
						Metadata: changes("flop bog deploy --json"),
						Flags: append(buildFlags(),
							&cli.StringFlag{Name: "name", Usage: "bog name (defaults to the package name)"},
							&cli.StringFlag{Name: "box", Usage: "instance id to place a NEW deployment on (default: spade launches a box)"},
							&cli.BoolFlag{Name: "build-only", Usage: "stop after the image is built and pushed"},
							&cli.BoolFlag{Name: "no-wait", Usage: "return once the deployment is updated, without waiting for the box (then: flop bog wait)"},
							&cli.DurationFlag{Name: "timeout", Value: 10 * time.Minute, Usage: "how long to wait for the box to run the new image (a new box takes a few minutes)"},
							&cli.IntFlag{Name: "memory", Usage: "the machine's memory in GB (`flop sizes`); a deployed bog at another size moves to it after the rollout"},
							&cli.StringFlag{Name: "cpu", Usage: "shared (the sandbox: sleeps when idle, billed awake only; the default where it runs, up to its memory), standard, or more (twice the vCPUs): a box of its own"},
							&cli.IntFlag{Name: "data", Usage: "its data in GB: a new bog's size, or more than it has to grow it (it only grows)"},
							&cli.StringFlag{Name: "access", Usage: "who may call a NEW bog: private (every request needs a key; the default) or public (anyone, any method). A bog that exists keeps its own (flop bog access changes it)"},
						),
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return DeployBog(ctx, o, deployOptions{
								Access:    cmd.String("access"),
								Package:   cmd.String("package"),
								Fresh:     cmd.Bool("fresh"),
								Name:      cmd.String("name"),
								Box:       cmd.String("box"),
								BuildOnly: cmd.Bool("build-only"),
								NoWait:    cmd.Bool("no-wait"),
								Timeout:   cmd.Duration("timeout"),
								Size:      sizing{MemoryGB: int(cmd.Int("memory")), CPU: cmd.String("cpu"), DataGB: int(cmd.Int("data"))},
							})
						}),
					},
					{
						Name:     "build",
						Usage:    "build and push an image without deploying it (same as deploy --build-only)",
						Metadata: changes("flop bog build --json"),
						Flags: append(buildFlags(),
							&cli.StringFlag{Name: "arch", Usage: "arm64 (EC2, the default) or amd64 (the sandbox tier on Fly)"},
						),
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return DeployBog(ctx, o, deployOptions{Package: cmd.String("package"), Fresh: cmd.Bool("fresh"), BuildOnly: true, Arch: cmd.String("arch")})
						}),
					},
					{
						Name:      "status",
						Usage:     "show one bog: its deployment, health, address, data and access",
						ArgsUsage: "<bog>",
						Metadata:  reads("flop bog status search-server --json"),
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(BogStatus(ctx, cmd.Args().First()))
						}),
					},
					{
						Name:      "wait",
						Usage:     "wait until a bog runs its current generation, healthy (exit 6 on timeout, 7 if it fails)",
						ArgsUsage: "<bog>",
						Metadata:  reads("flop bog wait search-server --timeout 10m --json"),
						Flags: []cli.Flag{
							&cli.DurationFlag{Name: "timeout", Value: 10 * time.Minute, Usage: "how long to wait"},
						},
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(WaitForBog(ctx, o, cmd.Args().First(), cmd.Duration("timeout")))
						}),
					},
					{
						Name:      "deploy-to",
						Usage:     "run a bog image on a specific box",
						ArgsUsage: "<instance-id>",
						Metadata:  changes("flop bog deploy-to i-0123456789abcdef0 --image <ref> --name search-server"),
						Flags: []cli.Flag{
							&cli.StringFlag{Name: "image", Usage: "image ref to run", Required: true},
							&cli.StringFlag{Name: "name", Usage: "bog name (lowercase, digits, dashes)", Required: true},
						},
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(DeployTo(ctx, cmd.Args().First(), cmd.String("image"), cmd.String("name")))
						}),
					},
					{
						Name:     "deployments",
						Usage:    "list deployments: desired vs observed state",
						Metadata: reads("flop bog deployments --json"),
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(ListDeployments(ctx))
						}),
					},
					{
						Name:      "rollout",
						Usage:     "roll a bog to a new image (a new generation)",
						ArgsUsage: "<bog>",
						Metadata:  changes("flop bog rollout search-server --image <ref>"),
						Flags: []cli.Flag{
							&cli.StringFlag{Name: "image", Usage: "new image ref", Required: true},
						},
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(Rollout(ctx, cmd.Args().First(), cmd.String("image")))
						}),
					},
					{
						Name:      "rebox",
						Usage:     "move a bog to a fresh box spade launches, e.g. for a new image (the bog is down while it moves)",
						ArgsUsage: "<bog>",
						Metadata:  changes("flop bog rebox search-server"),
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(Rebox(ctx, cmd.Args().First()))
						}),
					},
					{
						Name:      "resize",
						Usage:     "move a bog to a machine of another size (the bog is down while it moves; `flop sizes` lists them)",
						ArgsUsage: "<bog>",
						Metadata:  changes("flop bog resize search-server --memory 16 --cpu more"),
						Flags: []cli.Flag{
							&cli.IntFlag{Name: "memory", Usage: "memory in GB (`flop sizes` lists what each CPU offers; default: as it is)"},
							&cli.StringFlag{Name: "cpu", Usage: "standard, more (twice the vCPUs), or shared (the sandbox: it moves there with its data); default: as it is"},
							&cli.BoolFlag{Name: "no-wait", Usage: "return once spade has the request, without waiting for the move"},
							&cli.DurationFlag{Name: "timeout", Value: 15 * time.Minute, Usage: "how long to wait for the move"},
						},
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(Resize(ctx, o, cmd.Args().First(), int(cmd.Int("memory")), cmd.String("cpu"), cmd.Bool("no-wait"), cmd.Duration("timeout")))
						}),
					},
					{
						Name:      "grow",
						Usage:     "grow a bog's data, while it runs (it only grows; on a box of its own, at most once in 6 hours)",
						ArgsUsage: "<bog>",
						Metadata:  changes("flop bog grow search-server --data 30"),
						Flags: []cli.Flag{
							&cli.IntFlag{Name: "data", Usage: "the new size in GB (larger than now; at most 1000, or 10 in the sandbox)", Required: true},
							&cli.BoolFlag{Name: "no-wait", Usage: "return once spade has the request, without waiting for the filesystem"},
							&cli.DurationFlag{Name: "timeout", Value: 10 * time.Minute, Usage: "how long to wait for the filesystem to grow"},
						},
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(Grow(ctx, o, cmd.Args().First(), int(cmd.Int("data")), cmd.Bool("no-wait"), cmd.Duration("timeout")))
						}),
					},
					{
						Name:      "undeploy",
						Usage:     "stop a bog (drained first; its data is kept)",
						ArgsUsage: "<bog>",
						Metadata:  changes("flop bog undeploy search-server"),
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(Undeploy(ctx, cmd.Args().First()))
						}),
					},
					{
						Name:  "keys",
						Usage: "API keys for calling a bog",
						Commands: []*cli.Command{
							{
								Name:      "create",
								Usage:     "make an API key (its secret is shown once)",
								ArgsUsage: "<bog>",
								Metadata:  changes("flop bog keys create search-server --name ci --json"),
								Flags: []cli.Flag{
									&cli.StringFlag{Name: "name", Usage: "what the key is for"},
									&cli.StringFlag{Name: "expires", Usage: "lifetime, like 720h (default: never)"},
								},
								Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
									return of(KeysCreate(ctx, cmd.Args().First(), cmd.String("name"), cmd.String("expires")))
								}),
							},
							{
								Name:      "list",
								Usage:     "list a bog's API keys",
								ArgsUsage: "<bog>",
								Metadata:  reads("flop bog keys list search-server --json"),
								Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
									return of(KeysList(ctx, cmd.Args().First()))
								}),
							},
							{
								Name:      "revoke",
								Usage:     "revoke an API key",
								ArgsUsage: "<bog> <key-id>",
								Metadata:  changes("flop bog keys revoke search-server key_abc123"),
								Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
									return of(KeysRevoke(ctx, cmd.Args().Get(0), cmd.Args().Get(1)))
								}),
							},
						},
					},
					{
						Name:  "env",
						Usage: "a bog's variables (readable) and secrets (never shown again): its environment",
						Commands: []*cli.Command{
							{
								Name:      "list",
								Usage:     "list a bog's variables and secrets (secrets' values hidden)",
								ArgsUsage: "<bog>",
								Metadata:  reads("flop bog env list search-server --json"),
								Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
									return of(EnvList(ctx, cmd.Args().First()))
								}),
							},
							{
								Name:      "set",
								Usage:     "set variables (KEY=value ...), or with --secret one secret (KEY; its value on stdin)",
								ArgsUsage: "<bog> KEY=value ... | --secret <bog> KEY",
								Metadata:  changes("printf %s \"$TOKEN\" | flop bog env set --secret search-server API_TOKEN"),
								Flags: []cli.Flag{
									&cli.BoolFlag{Name: "secret", Usage: "a secret: its value is read from stdin (or typed at a terminal), never shown again"},
									&cli.BoolFlag{Name: "no-restart", Usage: "save for the bog's next deploy instead of restarting it now"},
								},
								Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
									return of(EnvSet(ctx, o, cmd.Args().First(), cmd.Args().Tail(), cmd.Bool("secret"), cmd.Bool("no-restart")))
								}),
							},
							{
								Name:      "unset",
								Usage:     "remove variables or secrets",
								ArgsUsage: "<bog> KEY ...",
								Metadata:  changes("flop bog env unset search-server OLD_FLAG"),
								Flags: []cli.Flag{
									&cli.BoolFlag{Name: "no-restart", Usage: "save for the bog's next deploy instead of restarting it now"},
								},
								Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
									return of(EnvUnset(ctx, cmd.Args().First(), cmd.Args().Tail(), cmd.Bool("no-restart")))
								}),
							},
						},
					},
					{
						Name:      "access",
						Usage:     "show who may call a bog, or set it: private (every request needs a key) or public (anyone, any method)",
						ArgsUsage: "<bog> [private|public]",
						Metadata:  changes("flop bog access search-server public"),
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(Access(ctx, cmd.Args().Get(0), cmd.Args().Get(1)))
						}),
					},
					{
						Name:      "curl",
						Usage:     "call your own bog with a key made for this one request",
						ArgsUsage: "<bog> [path]",
						Metadata:  changes("flop bog curl search-server /search -d '{\"q\":\"bogs\"}' --json"),
						Flags: []cli.Flag{
							&cli.StringFlag{Name: "request", Aliases: []string{"X"}, Usage: "method (default GET, or POST with --data)"},
							&cli.StringFlag{Name: "data", Aliases: []string{"d"}, Usage: "JSON body"},
						},
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(Curl(ctx, o, cmd.Args().Get(0), cmd.Args().Get(1), cmd.String("request"), cmd.String("data")))
						}),
					},
					{
						Name:      "stats",
						Usage:     "show a bog's traffic, latency, CPU and memory",
						ArgsUsage: "<bog>",
						Metadata:  reads("flop bog stats search-server --window 6h --json"),
						Flags: []cli.Flag{
							&cli.StringFlag{Name: "window", Value: "1h", Usage: "how far back, like 15m, 6h or 2d"},
						},
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(Stats(ctx, cmd.Args().First(), cmd.String("window")))
						}),
					},
					{
						Name:      "logs",
						Usage:     "show a bog's log lines (--follow to keep watching; with --json, one JSON line each)",
						ArgsUsage: "<bog>",
						Metadata:  reads("flop bog logs search-server --since 15m --json"),
						Flags: []cli.Flag{
							&cli.StringFlag{Name: "since", Value: "1h", Usage: "how far back, like 15m, 6h or 2d"},
							&cli.IntFlag{Name: "limit", Value: 200, Usage: "at most this many recent lines"},
							&cli.BoolFlag{Name: "follow", Aliases: []string{"f"}, Usage: "keep printing new lines"},
						},
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return Logs(ctx, o, cmd.Args().First(), cmd.String("since"), cmd.Int("limit"), cmd.Bool("follow"))
						}),
					},
					{
						Name:     "volumes",
						Usage:    "list the organization's kept data volumes",
						Metadata: reads("flop bog volumes --json"),
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(Volumes(ctx))
						}),
					},
					{
						Name:      "copies",
						Usage:     "list a sandbox bog's copies of its data (one a day, the last 7 kept), which `flop bog restore` starts it from",
						ArgsUsage: "<bog>",
						Metadata:  reads("flop bog copies search-server --json"),
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(Copies(ctx, cmd.Args().First()))
						}),
					},
					{
						Name:      "restore",
						Usage:     "start an undeployed bog from one of its snapshots at its next deploy (its current volume is kept)",
						ArgsUsage: "<bog>",
						Metadata:  changes("flop bog restore search-server --snapshot snap-0123456789abcdef0"),
						Flags: []cli.Flag{
							&cli.StringFlag{Name: "snapshot", Usage: "snapshot id (snap-...); for a sandbox bog, the id of one of its copies (`flop bog copies`)", Required: true},
						},
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(Restore(ctx, cmd.Args().First(), cmd.String("snapshot")))
						}),
					},
					{
						Name:      "destroy",
						Usage:     "permanently delete a bog's kept data (the bog's name typed back, or --confirm <bog>)",
						ArgsUsage: "<bog>",
						Metadata:  changes("flop bog destroy old-bog --confirm old-bog"),
						Flags: []cli.Flag{
							&cli.StringFlag{Name: "confirm", Usage: "the bog's name again: required where flop cannot ask"},
						},
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							client, err := loggedInClient()
							if err != nil {
								return nil, err
							}
							return of(destroyBogData(ctx, client, o, cmd.Args().First(), cmd.String("confirm")))
						}),
					},
					{
						Name:     "builds",
						Usage:    "list builds",
						Metadata: reads("flop bog builds --package search-server --json"),
						Flags: []cli.Flag{
							&cli.StringFlag{Name: "package", Aliases: []string{"p"}, Usage: "only show builds of this package"},
						},
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(ListBuilds(ctx, cmd.String("package")))
						}),
					},
				},
			},
			{
				Name:     "sizes",
				Usage:    "the machine and data sizes a bog may have, their prices, and which this organization may choose",
				Metadata: reads("flop sizes --json"),
				Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
					return of(Sizes(ctx))
				}),
			},
			{
				Name:  "organization",
				Usage: "organization selection",
				Commands: []*cli.Command{
					{
						Name:     "list",
						Usage:    "list your organizations",
						Metadata: reads("flop organization list --json"),
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(ListOrganizations(ctx))
						}),
					},
					{
						Name:      "use",
						Usage:     "select the organization for subsequent commands",
						ArgsUsage: "<organization id or name>",
						Metadata:  changes("flop organization use acme"),
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(UseOrganization(ctx, cmd.Args().First()))
						}),
					},
				},
			},
			{
				Name:  "auth",
				Usage: "sign in and out (automation: FLOP_TOKEN and FLOP_ORGANIZATION_ID instead)",
				Commands: []*cli.Command{
					{
						Name:     "login",
						Usage:    "sign in: a person approves this device in the panel",
						Metadata: changes("flop auth login --no-browser --json"),
						Flags: []cli.Flag{
							&cli.BoolFlag{Name: "no-browser", Usage: "print the address and code instead of opening a browser"},
						},
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(Login(ctx, o, cmd.Bool("no-browser")))
						}),
					},
					{
						Name:     "logout",
						Usage:    "forget this device's login",
						Metadata: changes("flop auth logout"),
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(Logout())
						}),
					},
					{
						Name:     "whoami",
						Usage:    "show who flop acts as, in which organization, at which spade",
						Metadata: reads("flop auth whoami --json"),
						Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
							return of(Whoami(ctx))
						}),
					},
				},
			},
			{
				Name:     "version",
				Usage:    "show flop's version and the spade it talks to",
				Metadata: reads("flop version --json"),
				Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
					return Version(), nil
				}),
			},
			{
				Name:     "update",
				Usage:    "install the latest release of flop over this one",
				Metadata: changes("flop update --json"),
				Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
					return of(Update(ctx, o))
				}),
			},
			{
				Name:     "commands",
				Usage:    "list every command, its arguments and flags, and whether it changes anything",
				Metadata: reads("flop commands --json"),
				Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
					return Commands(cmd.Root()), nil
				}),
			},
			{
				Name:     "docs",
				Usage:    "print the guide for scripts and agents (AGENTS.md)",
				Metadata: reads("flop docs"),
				Action: command(func(ctx context.Context, cmd *cli.Command, o *ui) (result, error) {
					return guide{Markdown: agentsGuide}, nil
				}),
			},
		},
	}
}

// prepare readies every command: its usage errors are flop's (a usage
// error, with the command's help as the hint) rather than the library's (its
// help printed on stdout, where only results belong), and its help shows its
// example
func prepare(cmd *cli.Command) {
	cmd.OnUsageError = func(_ context.Context, cmd *cli.Command, err error, _ bool) error {
		return usage(err.Error(), fmt.Sprintf("see `%s --help`", cmd.FullName()))
	}
	if ex, ok := cmd.Metadata["example"].(string); ok && cmd.Description == "" {
		cmd.Description = "Example:\n   " + ex
	}
	for _, c := range cmd.Commands {
		prepare(c)
	}
}

// buildFlags are shared by every command that builds
func buildFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "package",
			Aliases: []string{"p"},
			Usage:   "package name (defaults to the [package] name in Cargo.toml)",
		},
		&cli.BoolFlag{
			Name:  "fresh",
			Usage: "discard cached build state before building",
		},
	}
}
