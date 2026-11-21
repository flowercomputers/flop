# bog-starter

A bog: a Rust server that keeps its data with [bog](https://github.com/flowercomputers/bog), run on Flower Computer with [flop](https://github.com/flowercomputers/flop). This file is for whoever changes it next, a person or an agent.

It starts as the smallest whole example: a page that shows a sum and adds to it, in front of an API where bog keeps that sum. Replace that with what this bog is for, keeping how it is built.

## Run, deploy, call

```
cargo run                         # the page at http://127.0.0.1:7877, its data in ./data
curl -s localhost:7877/add -H 'content-type: application/json' -d '{"amount":2}'
curl -s localhost:7877/sum
cargo test

flop bog deploy --access public   # build it remotely and run it; waits until it is healthy, then prints its address
flop bog curl bog-starter /sum
flop docs                         # the rest of flop: keys, logs, sizes, failures
```

A deploy uploads this git repository as it is on disk (tracked files, and untracked ones git does not ignore). It needs no commit, and no Rust on this machine.

`--access public` makes a new bog public, which a page needs: see "This bog is public".

## Where things go

| file | holds | change it for |
|---|---|---|
| `src/schema.rs` | what is stored (sources), what bog derives from it (views), the rules, a test | anything about the data |
| `src/api.rs` | wire types, handlers, routes | anything about the API |
| `web/`, `src/web.rs` | the page's files, and the list that serves them | anything about the page |
| `src/main.rs` | the address and data directory, the `bog` helper, errors, `/healthz`, stopping | rarely |

## How it is built

These are preliminary: they will change as bog does. Each is shown once in the code.

1. **The schema is the backend.** Rules (`reject`) and derived data (views) are declared in `schema.rs`, and bog keeps them with every commit. A handler that computes over many rows wants to be a view.
2. **One request, one snapshot or one transaction.** Everything a response reads comes from one `db.read`, so it agrees. A write is one `db.write`: all of it commits, or none.
3. **Stored rows are not the API.** What callers send and get are the wire types in `api.rs`, and handlers turn one into the other. Wire types change freely; stored rows do not (below).
4. **bog blocks.** Its calls read and write the disk, so a handler reaches it only through `bog(&db, |db| ...)` in `main.rs`, which runs the call off the async threads.
5. **One process, one disk.** A bog is a single program with its data directory: see "What the platform gives and asks".

## The page

- The files in `web/` are compiled into the binary (`include_bytes!` in `src/web.rs`): what runs deployed is the binary alone, so there is no directory to serve from. A change to one needs a build (`cargo run` again). A new file needs a line in `FILES` there, and `cargo test` fails for one left out.
- They are plain HTML, CSS and JavaScript, with no build step: the remote build runs cargo, and has no Node. A page made with other tools needs its built files committed into `web/`.
- The page calls the API at its own address (`fetch("/sum")`), so nothing here needs CORS.
- It does not poll. It asks for the sum when it loads, when it is looked at again, and with each addition: a request every few seconds would keep a sandbox bog awake, and billed.

## This bog is public

A browser cannot send a bog key, and a key written into a page belongs to everyone who loads it. So a bog with a page is public: `flop bog deploy --access public` makes a new one so. One deployed without it answers 401 at its address until `flop bog access bog-starter public`.

Public means the router lets every request through: any method, from anyone. As it stands, anyone can add to the sum. What each caller may do is this bog's own to decide, in its handlers:

- The router tells a bog who is calling: `X-Bog-Auth: anonymous`, or `X-Bog-Auth: key` with `X-Bog-Key-Id` for a request sent with one of its keys (`flop bog keys create bog-starter`). Only routers can reach a bog, so these headers can be trusted. A handler that is the owner's alone can require `key`.
- For people who log in, the bog keeps its own accounts and sessions: cookies, and an `Authorization` header that is not a bog key, reach it untouched.

`flop docs` has the whole of it.

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
- What runs is this package's binary and nothing beside it: no other files. Embed what it needs, as `src/web.rs` does the page.
- With no size named it runs in the sandbox: it sleeps when idle and wakes on its next request.

## Not here yet

- Live updates, where each change is pushed to clients as it commits. bog reports every commit's changes, but serving them to many clients needs care this starter does not take yet. Answer with requests for now.

## More

- bog's own documentation: `cargo doc -p bog --open`. Its source is under `~/.cargo/git/checkouts/` once this has built.
- A newer bog: `cargo update -p bog`, then `cargo test`.
- `/openapi.json` describes the API from the handlers' types: a route added with `api_route` in `api.rs` is in it.
