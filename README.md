# anything

A web server with no content. Ask for a page, and a model invents it on the
spot and streams the HTML back to your browser.

The path, query, method, headers, cookies and body give it something to work
with. Links lead to pages that don't exist yet. Forms get an imagined response.
Every page request starts a new generation; anything caches no pages.

```text
http://127.0.0.1:8080/haunted-lighthouse/gift-shop?sort=cursed
http://127.0.0.1:8080/museum/lost-and-found
http://127.0.0.1:8080/anything/you/can/think/of
```

Run it with **Claude Code**, an **Anthropic API key**, or a local model through
**Ollama**, **oMLX**, or **LM Studio / LM Studio Bionic**. Add `-session` and the
site builds a shared memory of the pages it has invented. Themes, random seeds
and notes from the terminal let you steer the world as you explore it.

[Get started](#get-started) · [Backends](#backends) ·
[Sessions](#sessions) · [Steer the site](#steer-the-site) ·
[Configuration](#configuration) · [How pages work](#how-pages-work) ·
[Troubleshooting](#troubleshooting) · [Privacy](#privacy-and-exposure) ·
[Build](#build-and-release)

## Get started

Download a binary from the [releases page](../../releases), or
[build from source](#build-and-release).

| Platform | Binary |
|---|---|
| macOS, Apple Silicon | `anything-darwin-arm64` |
| Linux, x86-64 | `anything-linux-amd64` |
| Windows, x86-64 | `anything-windows-amd64.exe` |

On macOS or Linux, rename the download to `anything` and make it executable
with `chmod +x anything`. The shell examples below use `./anything`.
On Windows, run `.\anything-windows-amd64.exe` from a terminal; set environment
variables using your shell's syntax rather than the Unix syntax shown below.
See [troubleshooting](#troubleshooting) if macOS blocks the download.

With [Claude Code](https://claude.com/claude-code) installed and logged in,
and `ANTHROPIC_API_KEY` unset:

```bash
./anything
```

For a local model, use the [backend setup instructions](#backends) below.
Then open <http://127.0.0.1:8080/> — or any path you can think of. Leave the
terminal running; press Ctrl+C to stop the server.

## Backends

Choose one backend. In local examples, replace `MODEL` with the exact model
ID your server lists, and choose a model that fits your hardware.

| Backend | What you need | Start anything |
|---|---|---|
| Claude Code | `claude` on your `PATH`, with a working login | `./anything` |
| Anthropic API | `ANTHROPIC_API_KEY` | `./anything` after setting the key |
| Ollama | A running Ollama server and an installed model | `./anything -ollama MODEL` |
| oMLX | A running oMLX server on Apple Silicon and an installed model | `./anything -omlx MODEL` |
| LM Studio / Bionic | A running Anthropic-compatible local API and an installed model | `./anything -omlx MODEL` with `OMLX_HOST` set |

**Selection rules:** `-ollama` and `-omlx` are mutually exclusive and take
precedence over Anthropic credentials. Without either flag, ordinary requests
use the Anthropic API when `ANTHROPIC_API_KEY` is set, otherwise the Claude CLI.
`-session` always uses the Claude CLI; see [sessions](#sessions) for credentials
and local context settings.

### Claude Code or Anthropic API

The Claude CLI uses your configured credentials and the `opus` model alias.
To call the Anthropic API directly instead:

```bash
export ANTHROPIC_API_KEY="your-api-key"
./anything
```

Direct API requests use `claude-opus-5-5` and are billed to that key. Neither
of these paths needs a local model server.

### Ollama

Install [Ollama](https://ollama.com) **0.14.0 or later**, then start the app or
run `ollama serve` in another terminal. Install a model and use its name from
`ollama list`:

```bash
ollama pull MODEL
ollama list
./anything -ollama MODEL
```

Ordinary local generation needs no API key, Claude Code, or Claude login.
The default server URL is `http://127.0.0.1:11434`; override it if needed:

```bash
OLLAMA_HOST=http://127.0.0.1:11435 ./anything -ollama MODEL
```

Ollama must support the [Anthropic Messages API](https://docs.ollama.com/api/anthropic-compatibility).
The supplied model name is used as-is; anything does not download models.

### oMLX

Install and start [oMLX](https://github.com/jundot/omlx), add an MLX model
through its admin dashboard, then use a model ID listed by `/v1/models`:

```bash
./anything -omlx MODEL
```

anything reads the server host, port, API key and session context budget from
`~/.omlx/settings.json`. A working oMLX installation usually needs no extra
configuration. `OMLX_HOST`, `OMLX_API_KEY` and `-local-context` override those
settings. Without settings, the server URL falls back to
`http://127.0.0.1:8000`.

### LM Studio and LM Studio Bionic

The `-omlx` backend also works with **LM Studio** and **LM Studio Bionic**:
these servers expose the same Anthropic-compatible `/v1/messages` endpoint.
There is no separate LM Studio flag.

In Bionic, enable **Settings → Local Model API** and copy the displayed server
URL. In classic LM Studio, start the server from the **Developer** tab.
Classic LM Studio needs **0.4.1 or later** for the
[Anthropic-compatible API](https://lmstudio.ai/docs/developer/api-changelog).
See the [Bionic setup guide](https://github.com/meta-models/meta-oss-cookbook/blob/main/inference-server/lm-studio.md)
and [LM Studio server guide](https://lmstudio.ai/docs/developer/core/server).

For a server listening on port `1234`, with authentication disabled:

```bash
export OMLX_HOST="http://127.0.0.1:1234"
export OMLX_API_KEY="lmstudio"
curl "$OMLX_HOST/v1/models" -H "Authorization: Bearer $OMLX_API_KEY"
./anything -omlx MODEL
```

Pick a model `id` from the response. Use your server's displayed URL if its
port differs. `OMLX_HOST` is the base URL **without `/v1`**. If the server
requires authentication, replace the placeholder `lmstudio` with its API
token. The [API accepts both key and bearer authentication](https://lmstudio.ai/docs/developer/anthropic-compat).

Set `OMLX_API_KEY` explicitly so an unrelated key from oMLX's settings is not
inherited. For sessions, also set `-local-context` to match the context window
configured for the loaded model. A server that offers only OpenAI-compatible
chat endpoints cannot use this backend.

## Sessions

By default, each generation starts with the current request, along with the
site's prompt, theme, seed and any pending note. The `Referer` can hint at
where a visitor came from, but the model has no memory of the previous page's
content.

With `-session`, requests become turns in one long-running conversation:

```bash
./anything -session
./anything -session -recycle 20
./anything -ollama MODEL -session -local-context 65536 -recycle 20
./anything -omlx MODEL -session -local-context 32768
```

Every page is still generated fresh, but characters, rumours and past events
can carry across visits. **The memory is shared by every visitor**, and
requests run one at a time; other requests wait. `-recycle 20` forgets after
20 pages. Type `r` and Enter to reset it yourself. Stopping the server loses
its memory too; sessions are not saved to disk.

All session modes require `claude` on your `PATH`. Claude sessions use the
CLI's available login or API credentials, even when `ANTHROPIC_API_KEY` is set;
`-session` selects the CLI, not direct API calls. Local sessions use Claude
Code in `--bare` mode against the chosen local server. They need no Claude
login and replace inherited Anthropic credentials with local ones.

### Local session context

`-local-context N` tells Claude Code what context budget to assume. It
**does not change the model server's context window**. Configure the server
first, then keep this value within its supported window:

- Ollama defaults to a **65536-token client budget**. Set the matching window
  in the app's settings, or stop the running app/server before starting
  `OLLAMA_CONTEXT_LENGTH=65536 ollama serve` in another terminal; see
  [Ollama's context guide](https://docs.ollama.com/context-length).
- oMLX uses `sampling.max_context_window` from its settings, falling back to
  **32768**. When using LM Studio or Bionic, set the flag explicitly to match
  the loaded model rather than inheriting oMLX's settings.

`-ollama-context` remains a legacy alias; prefer `-local-context`.
The flag sets Claude Code's
[`CLAUDE_CODE_MAX_CONTEXT_TOKENS`](https://code.claude.com/docs/en/model-config#correct-the-window-for-a-gateway-or-custom-model-id).
Support depends on the CLI version and model ID; compaction at the declared
limit has not been verified. Use `-recycle` for predictable memory resets.

## Steer the site

Give every page a world to belong to, or add variety with your own seed pool:

```bash
./anything -theme "deep sea research station, 1970s"
./anything -seeds "neon,rain,vending machine,1987"
./anything -seed-file moby-dick.txt
./anything -seed-mix "neon,rain" -theme "deep sea research station, 1970s"
./anything -no-seeds
```

A seed is picked at random for each request and offered as inspiration; the
model may use it, twist it or ignore it. Seeds vary the prompt, helping the
same URL lead to different ideas. With `-no-seeds`, similar requests can
produce similar pages. Seeds are words in the prompt, not a model sampling
parameter.

`-seed-file` takes every distinct word of four or more letters from a text
file, all equally likely. `-seed-mix` pairs one of your words with a pick from
the active pool, including the built-in pool. `-seeds` and `-seed-file` cannot
be combined; `-no-seeds` cannot be combined with any other seed flag.

### Live terminal commands

Type a command into the server's terminal and press Enter:

| Command | Effect |
|---|---|
| `r` | Reset session memory after any page in progress finishes. |
| `s` | Show pages, skipped requests, timings, tokens, available cost/usage figures, session memory, seeds and theme. |
| `t` | Show the current theme. |
| `t <text>` | Change the theme for subsequent pages, preserving session memory. |
| `t off` | Remove the theme. |
| `n <text>` | Add a note for the next page, e.g. `n the station has begun to flood`. Multiple pending notes add up. |
| `n` / `n off` | Show / cancel the pending note. |
| `h` | List the commands. |

In session mode, a note's page enters the site's memory, so its effect can
live on: you narrate, the site remembers. These controls are available only
in the operator's terminal.

## Configuration

Run `./anything -h` for grouped command-line help.

| Flag | Default | Meaning |
|---|---|---|
| `-ollama MODEL` | unset | Generate with this installed Ollama model. |
| `-omlx MODEL` | unset | Generate with this oMLX or compatible server model. |
| `-session` | off | Keep one shared conversation through the Claude CLI. |
| `-recycle N` | `0` | With `-session`, reset memory after `N` pages; `0` means no automatic reset. |
| `-local-context N` | auto | Claude CLI context budget for local sessions; see [defaults](#local-session-context). |
| `-ollama-context N` | unset | Legacy alias, used when `-local-context` is unset. |
| `-theme "…"` | none | A theme for the whole site. |
| `-seeds a,b,c` | built-in pool | A comma-separated seed pool. |
| `-seed-file PATH` | none | A text file to use as the seed pool. |
| `-seed-mix a,b` | none | Pair one of these words with each pick from the pool. |
| `-no-seeds` | off | Generate without a seed. |

| Environment variable | Default | Meaning |
|---|---|---|
| `PORT` | `8080` | Website port; always bound to `127.0.0.1`. |
| `ANTHROPIC_API_KEY` | unset | Use direct Anthropic API calls when local flags and `-session` are off; Claude sessions may also use it through the CLI. |
| `OLLAMA_HOST` | `http://127.0.0.1:11434` | Ollama API base URL. |
| `OMLX_HOST` | oMLX settings, then `http://127.0.0.1:8000` | oMLX or compatible server API base URL. |
| `OMLX_API_KEY` | oMLX settings, then placeholder `omlx` | Key for the server selected by `-omlx`; use its real key when authentication is required. |

Both local host variables accept `host:port` or `http(s)://host:port`, with
no path, query, fragment or embedded credentials. Use the server's base URL,
not a `/v1` or `/v1/messages` URL. The website's port and model server's port
are separate; for example, `PORT=3000 ./anything` moves only the website.

## How pages work

The model builds primarily from the path and query, then the method and body,
`Referer`, cookies, `Accept-Language` and `User-Agent`. Technical headers such
as `Sec-*`, cache controls, `Accept` and `Accept-Encoding` are removed first.
Request bodies are limited to **64 KiB**; larger bodies are rejected.

The prompt asks for a complete HTML document with inline CSS and visuals.
Links and forms lead to another generated page; they don't create database
records or a persistent application. The server strips any output before the
first `<` and sends `Cache-Control: no-store`.

For **all local backends**, browser navigation immediately shows a loading
screen with elapsed time. It stays visible through the generated head and
CSS, then streams the page's content into the same tab. This requires
JavaScript. A single-use token connects the loader to the original navigation,
including form method and body. Clients such as `curl` receive raw HTML.
Claude-backed pages stream directly without the local loading screen.

Closing a tab cancels per-request generation. In session mode, the page
finishes even after the visitor leaves and becomes part of the site's memory.
Generation speed depends on the backend, model, hardware and request.

### Requests that are skipped

To avoid generating pages nobody will see, these return a 404 and are logged
once per path and reason:

- `/.well-known/…` probes, including Chrome DevTools checks.
- `/favicon.ico` and `/apple-touch-icon…`.
- Browser requests whose `Sec-Fetch-Dest` is not `document`, `iframe` or
  `frame`: images, scripts, styles, fonts and background `fetch()` calls.
- WebSocket upgrades.

Generated pages should therefore be self-contained. Clients without
`Sec-Fetch-Dest` can request pages, but the path and WebSocket exclusions
still apply.

### Reading the log

Each generation logs the request and its result:

```text
GET /observatory  [seed: casino]  [mode=navigate user-initiated]  from 127.0.0.1:61267
  done /observatory  2.6kB  first byte 2.5s  total 16.3s  · opus-5-5 · context 1.3k (624 cached)  out 1.2k · 79 tok/s · ≈$0.029 at API rates · plan 5h 1% · 7d 8%
```

`first byte` measures time until generated HTML starts; `total` includes the
whole generation and, in session mode, time waiting for earlier requests.
`context` counts input tokens, including cached input; it generally grows as
session memory grows. `out` and `tok/s` describe output tokens and generation
throughput. Available figures depend on what the backend reports.

Direct Anthropic API costs use the program's API-rate calculation. CLI costs
are shown as API-rate estimates; for subscription logins they are not a
separate charge. If the CLI uses an API key, API billing still applies despite
that estimate label. Subscription usage appears as `plan` when reported, with a
warning at 80% or when limited. Local backends omit Anthropic costs and
subscription usage.

Local backends also log when HTML starts and progress every 10 seconds.
Requests with no browser fetch metadata log all headers to help identify the
sender. `(visitor left before it finished)` records a disconnected visitor.
Type `s` for totals across the run.

## Troubleshooting

| Symptom | Check |
|---|---|
| macOS blocks the downloaded binary | Binaries are unsigned. After downloading from the release page and renaming it, run `xattr -d com.apple.quarantine ./anything`. |
| `no ANTHROPIC_API_KEY and no claude CLI on PATH` | Install and authenticate Claude Code, set an API key, or select a local backend. |
| `-session needs the claude CLI on PATH` | All sessions need Claude Code installed, including local sessions. Local sessions do not need a Claude login. |
| Local server connection fails | Start the model server, check its displayed URL and port, and omit `/v1` from the host variable. |
| Local API returns 404 for `/v1/messages` | Update Ollama to 0.14.0+ or classic LM Studio to 0.4.1+; the server must support the Anthropic Messages API. |
| Model not found | Use the exact ID from `ollama list` or the server's `/v1/models` response. |
| oMLX / LM Studio rejects authentication | Set `OMLX_API_KEY` to that server's token; check for settings inherited from oMLX. |
| Local session reaches its context limit | Match `-local-context` to the server's configured window; use `-recycle N` or `r` to clear memory. |
| Loading screen stays visible | Check terminal progress and server errors. Ensure JavaScript is enabled. Large models can take longer before producing visible HTML. |
| Local generation is unexpectedly slow on a Mac | Check model size, available memory and Low Power Mode, which can reduce performance. |

## Privacy and exposure

The website binds to **`127.0.0.1` only**. It is designed for local exploration:
each visitor chooses what the model generates, and each page uses inference.
There is no visitor authentication, rate limiting or HTTP timeout. Page and
iframe navigations that reach the port can trigger generation. Sessions
serialize requests, but ordinary requests have no concurrency cap.

Requests sent to a hosted backend leave your machine. Cookies, form bodies
and other retained headers enter the prompt; in session mode they can
influence pages served to later visitors. Avoid sensitive data, including in
logs, and consider that shared memory before exposing the server through a
proxy or tunnel.

The model is given the request, site instructions, seeds, themes, notes and
any session history. Claude Code is started with no tools, no MCP servers,
no user/project settings and no saved sessions, from a temporary working
directory. The restrictions are in `cliArgs()` in [`main.go`](main.go).
Local requests call the server directly; local sessions additionally use
`--bare`.

A Claude login can attach account and environment context, such as the
operator's email address. The prompt tells the model never to use it in a
page, but that instruction is not a guarantee. Direct API requests and bare
local sessions do not add this Claude-login context. If you change the CLI's
tool restrictions, remember that anyone reaching the website supplies input
to the model.

## Build and release

Building requires **Go 1.25+**:

```bash
make            # build ./anything for this machine
./anything -h   # inspect the available flags
go test ./...   # run the existing test suite
make dist       # cross-compile release binaries into dist/
```

`make dist` produces the three platform binaries listed above. Pushing a
`v*` tag runs [the release workflow](.github/workflows/release.yml), builds
those binaries and attaches them to a GitHub release. Maintainers can release
with:

```bash
git tag v0.1.0
git push origin v0.1.0
```
