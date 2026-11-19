# flop for scripts and agents

flop deploys and runs bogs on Flower Computer. Everything a person does with it, a script or an agent can do too, without a terminal: this is how.

## Signing in

- **Automation:** an organization token, made in the panel, which calls it an API key (API keys → Create API key). Give it the bog scopes the job needs:

  | scope | for |
  |---|---|
  | `bog:read` | every command that only reads, and what the others look up first |
  | `bog:create` | `deploy` (its build, and a new bog), `build`, `keys create`, `curl`, `rebox`, `resize`, `restore` |
  | `bog:update` | `deploy` of a bog already deployed (a rollout), `rollout`, `env set`, `access`, `grow` |
  | `bog:delete` | `undeploy`, `keys revoke`, `env unset`, `destroy`; `curl` revokes its key with it |

  ```
  export FLOP_TOKEN=<the token> FLOP_ORGANIZATION_ID=<the organization's id>
  export FLOP_API_BASE_URL=https://api.dev.bog.run   # leave unset for production
  ```

  `flop organization list`, run by a person who is logged in, shows the id. An organization token reaches bog commands and `flop sizes` only; it lasts 90 days.
- **A person, through an agent:** `flop auth login --no-browser --json` prints, on stderr, one JSON line `{"event":"verify","url":...,"user_code":...,"expires_at":...}`. Show the URL and code to the person; flop waits until they approve (they have 10 minutes), then prints the login on stdout. A login lasts 30 days. Someone in several organizations chooses the one commands act in with `flop organization use <id or name>` (`flop organization list` shows them).
- `flop auth whoami --json` says who flop acts as.

## Output

Add `--json` to any command (or set `FLOP_OUTPUT=json`):

- **stdout** carries exactly one JSON document, the command's result, and nothing else. `flop bog logs --follow --json` streams one JSON object per line instead.
- **stderr** carries everything for a person: progress, build output, waiting. Ignore it, or show it.
- People see the same result rendered as text; the two never disagree.
- When a newer flop is out, stderr ends with one JSON line `{"event":"update","version":"0.1.0","latest":"1.0.0","breaking":true}`. `breaking` (a new major version) means this flop may no longer work: tell your person, who can run `flop update`. `FLOP_NO_UPDATE_CHECK=1` turns it off.

Result shapes follow spade's API (a deployment, a bog, a key, ...). `flop bog deploy --json` returns:

```json
{"bog":"search-server","created":false,"build":{"id":"...","package":"search-server","version":"58294f5","image":"...:58294f5-1793200805"},
 "waited":true,"deployment":{"id":"...","generation":4,...},"url":"https://search-server-whlyn.bog.run"}
```

## Failures

A failed command exits non-zero and, with `--json`, prints on stderr:

```json
{"error":{"code":"not_found","message":"bog nope: no such bog (404)","hint":"see what there is: `flop bog deployments`, ...","status":404}}
```

Branch on `code` (or the exit status); `hint` is the next step, often a command to run.

| exit | code | meaning |
|---|---|---|
| 0 | | it worked |
| 1 | `failed` | anything else |
| 2 | `usage`, `invalid` | wrong arguments or flags, or spade refused the input |
| 3 | `not_logged_in`, `unauthorized`, `forbidden` | no token, a refused or expired token, a missing scope (the hint names it), or an account Flower Computer suspended (the message says so) |
| 4 | `not_found` | no such bog, deployment, key or build |
| 5 | `conflict` | e.g. still deployed, already moving, past one of the organization's limits |
| 6 | `timeout` | a wait gave up; the bog may still get there |
| 7 | `bog_failed`, `build_failed` | the build failed (its output is on stderr), or the bog could not start or never passed `/healthz` |
| 8 | `unavailable` | spade could not be reached, or failed; try again. Also spade holding a caller back (too many requests from one address, or too many builds or keys made in an hour): the message says how long to wait |
| 9 | `payment_required` | the organization cannot pay, or the size asked for needs a card on file; add credits or a card in the panel |

## Never waiting for a keyboard

Without a terminal (or with `--no-input`, or `FLOP_NO_INPUT=1`) flop asks nothing:

- `flop bog destroy <bog> --confirm <bog>`: destroying data needs the name twice; without `--confirm` it refuses (exit 2).
- `flop bog env set --secret <bog> KEY` reads the secret's value from stdin (never from the command line, where shells keep it).

## Common flows

```
# deploy the project in the current git repository, and wait until it is healthy
flop bog deploy --json

# or deploy, and wait separately
flop bog deploy --no-wait --json
flop bog wait <bog> --timeout 10m --json      # exit 0 healthy, 6 timed out, 7 failed

# look at one bog
flop bog status <bog> --json

# call it (a key made for this one request; to call it many times, make one key and use it: see below)
flop bog curl <bog> /search -d '{"q":"bogs"}' --json   # {"status":200,"body":...}

# why is it failing?
flop bog logs <bog> --since 15m --json
flop bog status <bog> --json                   # deployment errors are in .deployment

# configuration: a variable, then a secret from stdin
flop bog env set <bog> FEATURE_X=on --json
printf %s "$TOKEN" | flop bog env set --secret <bog> API_TOKEN --json

# roll back: deploy an earlier build's image
flop bog builds --json                          # pick an earlier .builds[].image_ref, built for where the bog runs now
flop bog rollout <bog> --image <image> --json

# a key for another service, or for many calls of your own
flop bog keys create <bog> --name <who> --json   # .secret is shown once; send it as Authorization: Bearer <secret> to .url

# who may call it: private (every request needs a key) or public (anyone, any method)
flop bog access <bog> public --json

# sizes: what a bog's machine and data may be, what each costs, which this organization may choose
flop sizes --json                               # .sizes[].default, .allowed; .not_allowed says why not
flop bog deploy --memory 16 --cpu more --data 30 --json   # a new bog at that size; a deployed one rolls out, then moves to it and grows its data
flop bog resize <bog> --memory 32 --json        # the same CPU at another memory. On a box of its own: a new box, down a few minutes; exit 7 if the move fails (it stays where it was)
flop bog resize <bog> --memory 4 --cpu standard --json   # a sandbox bog, moved with its data to a box of its own
flop bog grow <bog> --data 50 --json            # online, while it serves; data only grows, and on a box of its own once in 6 hours (exit 5 says when)
flop bog status <bog> --json                    # .advice: "ran out of memory …", "data is 87% full …"

# a sandbox bog's data is copied once a day, and its last 7 copies kept: start it from one
flop bog copies <bog> --json                    # .copies[], newest first; any with "state":"done"
flop bog undeploy <bog> --json                  # a restore refuses a deployed bog, and one still stopping (exit 5)
flop bog restore <bog> --snapshot <copy id> --json
flop bog status <bog> --json                    # deploy it once .data.pending is no longer "restore"
```

Commands that take a bog accept its name; `rollout`, `rebox`, `undeploy`, `resize` and `wait` also accept a deployment id, or the start of one.

## Where a bog runs

`flop sizes --json` lists the machines a bog may run on (`.sizes[]`: `memory_gb`, `cpu`, `tier`, `price_per_hour`, which is the `default`, and whether this organization is `allowed` it) and the organization's limits (`.limits`). There are two kinds:

- **The sandbox** (`--cpu shared`, 1 or 2 GB of memory): where a new bog goes when no `--cpu` is named, and no more memory than it has. It sleeps after a few minutes without requests and wakes on its next one in under a second; only its time awake is billed. It needs no card, and an organization may run 3 at once. Its data is 1 to 10 GB.
- **A box of its own** (`--cpu standard` or `more`, from 4 GB of memory): always on. It needs a card on file (exit 9, `payment_required`). Its data is 10 to 1000 GB. `--memory 16` alone is one too, with the standard CPU.

A sleeping bog is `"status":"asleep"` and `"healthy":false` in `flop bog status`: nothing is wrong, and its next request wakes it. A change to a sleeping bog (a deploy, `env set`, a resize) wakes it too, so the change runs at once and `deploy` waits until it is healthy; it sleeps again when idle. `flop bog wait` on a bog that is asleep with nothing new waits for a request, and gives up with exit 6 if none comes: `flop bog curl <bog> /healthz` is one.

Past one of the organization's limits (its bogs' memory in all, the data it keeps, its sandbox bogs) is exit 5 (`conflict`): Flower Computer raises limits.

`flop bog resize` with `--cpu standard` or `more` moves a sandbox bog, with its data, to a box of its own; `--cpu shared` moves one back, if its data fits in 10 GB. The bog is built again for where it is going and keeps running meanwhile, then is down a few minutes while its data moves; if it does not get healthy there, it moves back (exit 7).

A build is for one kind or the other. `flop bog deploy` builds for where the bog runs, or will. `flop bog build` builds for a box of its own unless given `--arch amd64` (the sandbox), and a `rollout` needs an image built for where the bog runs: a sandbox build's image ends in `-amd64`.

## Who may call a bog

The router checks keys; the bog decides what each caller may do. A key is only an identity: a valid one passes any method, and an invalid one is refused (401) whatever the mode. A **private** bog (the default) needs a key on every request; a **public** bog takes anything without one. CORS preflights reach every bog without a key, so a bog answers its own CORS. The bog sees:

- `X-Bog-Auth: key` and `X-Bog-Key-Id: <id>` for a request with a key (the key itself is removed), or `X-Bog-Auth: anonymous`;
- on a public bog, any `Authorization` that isn't a bog key (`bog_…`), untouched: the bog's own login scheme.

Only routers can reach a bog, so these headers can be trusted; any a client sends are dropped. Routers also limit how fast one bog, and one key, is called: past that they answer 429 with `Retry-After`, and the bog never sees the request. Even a private bog gets anonymous `OPTIONS` preflights: answer CORS on `OPTIONS` and do no other work there. The router drops `Domain` from `Set-Cookie` headers so a bog's cookies stay on its hostname, but it can't stop a page's JavaScript setting `document.cookie` for all of `bog.run`.

## What each command does

`flop commands --json` lists every command: its arguments, its flags, whether it **changes** anything (`"changes": true`) or only reads, and an example. Read it before acting; prefer the read-only commands to find out where things stand.
