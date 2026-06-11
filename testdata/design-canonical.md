# jsonpp — Interface Contract

## Charter

`jsonpp` is a self-contained CLI that pretty-prints JSON for a developer at the terminal who has compact/minified JSON and wants it readable — its headline use case is `cat data.json | jsonpp`, which reads minified JSON on stdin and writes indented JSON to stdout.

## Command Surface

`jsonpp` is a single command that reads JSON from stdin and writes formatted JSON to stdout. It exposes the following flags:

| Flag | Argument | Effect |
| --- | --- | --- |
| (no flag) | — | Read JSON from stdin, write JSON re-indented with 2 spaces to stdout. This is the headline behavior. |
| `-i N`, `--indent N` | integer `N` ≥ 0 | Indent nested levels by `N` spaces instead of the default 2. `N=0` puts each element on its own line with no indentation. |
| `-t`, `--tab` | — | Indent using a single tab character per level instead of spaces. Mutually exclusive with `--indent`; supplying both is a usage error. |
| `-c`, `--compact` | — | Minify instead of expand: emit the JSON on a single line with no insignificant whitespace (inverse of the default). |
| `-s`, `--sort-keys` | — | Emit object keys in lexicographic (byte-wise) order at every nesting level. Without it, original key order is preserved. |
| `-h`, `--help` | — | Print usage text to stdout and exit 0. No stdin is read. |
| `-V`, `--version` | — | Print `jsonpp <version>` to stdout and exit 0. No stdin is read. |

There are no subcommands and no positional arguments. Input is always stdin; output is always stdout. Flags may be combined except `--tab` with `--indent`.

## Output schema/format

`jsonpp` emits **human-readable text** (formatted JSON), not a structured envelope. Behavior is defined by streams and exit code:

- **stdout** — the reformatted JSON document, followed by exactly one trailing newline. The output is semantically identical to the input (same values, same structure); only whitespace and — with `--sort-keys` — key order change. Default indentation is 2 spaces per nesting level.
- **stderr** — empty on success. On error, a single diagnostic line of the form `jsonpp: <message>` (for parse errors, includes line and column: `jsonpp: parse error at line L, column C: <reason>`).
- **exit code**:
  - `0` — valid JSON read and formatted successfully (also for `--help` / `--version`).
  - `1` — input was not valid JSON (parse error).
  - `2` — usage error (unknown flag, bad `--indent` value, or `--tab` combined with `--indent`).

Concrete sample (default invocation):

```
$ printf '{"b":1,"a":[2,3]}' | jsonpp
{
  "b": 1,
  "a": [
    2,
    3
  ]
}
```

## Default no-flag behavior

With no flags, `jsonpp` reads a JSON document from stdin and writes it to stdout indented with 2 spaces per level, preserving key order, with one trailing newline. This is the Charter's headline use case. Worked example:

```
$ cat data.json
{"name":"ada","langs":["ada","forth"],"active":true}

$ cat data.json | jsonpp
{
  "name": "ada",
  "langs": [
    "ada",
    "forth"
  ],
  "active": true
}
```

## Canonical invocations

1. **Headline — expand minified JSON (default):**
   ```
   $ printf '{"a":1,"b":2}' | jsonpp
   {
     "a": 1,
     "b": 2
   }
   ```

2. **Indent with 4 spaces (`--indent`):**
   ```
   $ printf '{"a":[1]}' | jsonpp --indent 4
   {
       "a": [
           1
       ]
   }
   ```

3. **Tab indentation (`--tab`):**
   ```
   $ printf '{"a":1}' | jsonpp --tab
   {
   	"a": 1
   }
   ```

4. **Minify and sort keys (`--compact`, `--sort-keys`):**
   ```
   $ printf '{"b":1,"a":2}' | jsonpp --compact --sort-keys
   {"a":2,"b":1}
   ```

5. **Parse error on invalid input (exit 1):**
   ```
   $ printf '{"a":}' | jsonpp; echo "exit=$?"
   jsonpp: parse error at line 1, column 6: unexpected token '}'
   exit=1
   ```

6. **Version (`--version`):**
   ```
   $ jsonpp --version
   jsonpp 1.0.0
   ```

## Acceptance criteria

- [ ] **(no flag)** `printf '{"a":1,"b":2}' | jsonpp` exits 0 and prints the document indented 2 spaces per level, key order preserved, with one trailing newline.
- [ ] **`-i N` / `--indent N`** `printf '{"a":[1]}' | jsonpp --indent 4` indents nested levels by 4 spaces; `--indent 0` places each element on its own line with no leading indentation.
- [ ] **`-t` / `--tab`** `printf '{"a":1}' | jsonpp --tab` indents each level with one tab character; combining `--tab` with `--indent` exits 2 with a `jsonpp:` usage diagnostic on stderr.
- [ ] **`-c` / `--compact`** `printf '{ "a" : 1 }' | jsonpp --compact` emits the JSON on a single line with no insignificant whitespace.
- [ ] **`-s` / `--sort-keys`** `printf '{"b":1,"a":2}' | jsonpp --sort-keys` emits object keys in lexicographic order at every nesting level.
- [ ] **`-h` / `--help`** `jsonpp --help` prints usage text to stdout, reads no stdin, and exits 0.
- [ ] **`-V` / `--version`** `jsonpp --version` prints `jsonpp <version>` to stdout and exits 0.
- [ ] **invalid input** `printf '{"a":}' | jsonpp` exits 1 with a single `jsonpp: parse error at line L, column C: ...` line on stderr and nothing on stdout.
- [ ] **success contract** every successful run writes the reformatted document plus exactly one trailing newline to stdout, leaves stderr empty, and produces output semantically identical to the input.

## Coherence requirements (your design is REJECTED unless all hold)

(1) exactly one tool / one contract
(2) every criterion exercises THIS contract
(3) the default invocation demonstrates the headline
(4) NO second/competing contract or mode hiding

## Problem

Minified or single-line JSON — from API responses, log lines, or config dumps — is unreadable at a
glance. Developers reach for ad-hoc one-liners (`python -m json.tool`, `jq .`) that each carry their
own quirks, dependencies, or surprising defaults. There is room for a single, dependency-light,
predictable formatter that does exactly one thing well.

## Goals / Non-goals

**Goals:**
- Read JSON on stdin, write re-indented JSON on stdout (the headline).
- Configurable indentation (`--indent`, `--tab`), minification (`--compact`), and key sorting (`--sort-keys`).
- Deterministic, predictable output; clear exit codes; precise parse-error diagnostics.

**Non-goals (explicitly out of scope):**
- JSON5, comments, or trailing-comma tolerance — input must be strict RFC 8259 JSON.
- In-place file editing or reading files by path (stdin only).
- Streaming/incremental formatting of documents larger than memory.
- Schema validation, querying, or transformation (that is `jq`'s job, not this tool's).

## Hermetic build constraints

`jsonpp` is a self-contained CLI built with a single toolchain (Go), using only standard-library or
permissive-licensed dependencies. It builds and tests fully offline — no secrets, no network, no paid
services. It ships an OSI license (MIT) and a README. The repo's **Makefile** honors the canonical
contract: `make check` (lint + test), `make test` (the suite), `make build` (compile the `jsonpp`
binary), and `make run` — which invokes the real entrypoint on the bundled fixture
`examples/sample.json` (a small minified document), producing real pretty-printed output. The
completion smoke-run judges WORKS vs HOLLOW from this; a `make run` that only printed `--help` would
be hollow. `examples/sample.json` ships in the repo.

## Test expectations

- **Unit** — the formatter core: empty object/array, nested structures, all scalar types, Unicode
  strings, deep nesting; `--indent N` (incl. `0`), `--tab`, `--compact`, `--sort-keys` each produce
  the specified shape; the parser reports line/column on malformed input.
- **Integration** — the CLI surface: each flag end-to-end over stdin→stdout; exit codes 0/1/2 for
  success / parse-error / usage-error; `--tab` + `--indent` rejected; `--help` / `--version` read no stdin.
- **E2e** — `make run` formats `examples/sample.json` and emits real indented output (the WORKS smoke
  signal); the README's documented invocation reproduces.

Every acceptance criterion above is covered by at least one named test.
