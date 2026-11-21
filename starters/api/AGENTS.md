# bog-starter

A bog: a Rust server that keeps its data with [bog](https://github.com/flowercomputers/bog), run on Flower Computer with [flop](https://github.com/flowercomputers/flop). This file is for whoever changes it next, a person or an agent.

It starts as the smallest whole example: amounts are added, and bog keeps their sum. Replace that with what this bog is for, keeping how it is built.

## Run, deploy, call

```
cargo run                 # http://127.0.0.1:7877, its data in ./data
curl -s localhost:7877/add -H 'content-type: application/json' -d '{"amount":2}'
curl -s localhost:7877/sum
cargo test

flop bog deploy           # build it remotely and run it; waits until it is healthy
flop bog curl bog-starter /add -d '{"amount":2}'
flop bog curl bog-starter /sum
flop docs                 # the rest of flop: keys, logs, sizes, failures
```

A deploy uploads this git repository as it is on disk (tracked files, and untracked ones git does not ignore). It needs no commit, and no Rust on this machine.

## Where things go

| file | holds | change it for |
|---|---|---|
| `src/schema.rs` | what is stored (sources), what bog derives from it (views), the rules, a test | anything about the data |
| `src/api.rs` | wire types, handlers, routes | anything about the API |
| `src/main.rs` | the address and data directory, the `bog` helper, errors, `/healthz`, stopping | rarely |

## How it is built

These are preliminary: they will change as bog does. Each is shown once in the code.

1. **The schema is the backend.** Rules (`reject`) and derived data (views) are declared in `schema.rs`, and bog keeps them with every commit. A handler that computes over many rows wants to be a view.
2. **One request, one snapshot or one transaction.** Everything a response reads comes from one `db.read`, so it agrees. A write is one `db.write`: all of it commits, or none.
3. **Stored rows are not the API.** What callers send and get are the wire types in `api.rs`, and handlers turn one into the other. Wire types change freely; stored rows do not (below).
4. **bog blocks.** Its calls read and write the disk, so a handler reaches it only through `bog(&db, |db| ...)` in `main.rs`, which runs the call off the async threads.
5. **One process, one disk.** A bog is a single program with its data directory: see "What the platform gives and asks".

## Changing the schema

What is stored outlives the code that stored it, and bog has no migrations yet. Checked against the bog pinned in `Cargo.lock`:

| change | what happens |
|---|---|
| add, change or remove a view | fine: bog rebuilds it from the sources at the next start |
| add a source | fine: it starts empty |
| change wire types, handlers or routes | fine: nothing stored knows them |
| add, remove, reorder or retype a field of a stored row | the rows already stored no longer read as they were written: a row is stored as its fields in order, without names. With a field added, every read of them fails |
| rename a stored row's type, or rename or remove a source that holds rows | the bog does not start |

A build of the last two kinds fails its deploy (`flop bog deploy` exits 7), and the bog is down until the build before it runs again: deploy the old code, or pick its image from `flop bog builds` and `flop bog rollout bog-starter --image <image>`. `/healthz` reads a row of every source (`check` in `schema.rs`) so that such a build fails instead of serving errors: add each new source there.

To change a row's shape, add a new source with a new row type beside the old one, and keep the old one declared. To start the data over: `flop bog undeploy bog-starter`, then `flop bog destroy bog-starter`.

## What the platform gives and asks

- `PORT`: listen on it, on every address. `BOG_DATA_DIR`: keep the data there; it outlives deploys and restarts. `main.rs` reads both.
- `GET /healthz` answering 200 means healthy. A deploy waits for it.
- `SIGTERM` means stop: `main.rs` finishes its requests, then checkpoints.
- What runs is this package's binary and nothing beside it: no other files. Embed what it needs (`include_str!`, `include_bytes!`).
- A bog is private unless it is made public. A private bog's callers need a key (`flop bog keys create bog-starter`): the router refuses a request without one, and takes the key off before the bog sees the request. A public bog takes any request, from anyone, and decides for itself what each caller may do. `--access public` on a bog's first deploy makes it public, and `flop bog access bog-starter public` (or `private`) changes it after. `flop docs` says what a bog is told about its caller.
- With no size named it runs in the sandbox: it sleeps when idle and wakes on its next request.

## Not here yet

- Live updates, where each change is pushed to clients as it commits. bog reports every commit's changes, but serving them to many clients needs care this starter does not take yet. Answer with requests for now.

## More

- bog's own documentation: `cargo doc -p bog --open`. Its source is under `~/.cargo/git/checkouts/` once this has built.
- A newer bog: `cargo update -p bog`, then `cargo test`.
- `/openapi.json` describes the API from the handlers' types: a route added with `api_route` in `api.rs` is in it.
