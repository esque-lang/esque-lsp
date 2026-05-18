# esque-lsp

A Language Server Protocol implementation for the
[esque](https://github.com/esque-lang/esquec) programming language.

`esque-lsp` runs as a child process of your editor, speaks LSP 3.17
over stdio, and provides:

- Diagnostics (lex errors, unterminated comments/strings, etc.)
- Document symbols (top-level `fn` declarations)
- Hover docs for keywords, primitive types, builtins, intrinsics, and
  user-defined functions
- Go to definition for top-level functions
- Completion for keywords, primitive types, builtins, attributes, and
  in-scope user functions

The implementation is intentionally lightweight — it operates on a
token stream rather than a full AST — so it tolerates partial / broken
files gracefully while you are typing.

## Requirements

- Go 1.25 or newer.

That is the only build dependency. The server uses only the Go
standard library.

## Build & install

```bash
git clone https://github.com/esque-lang/esque-lsp.git
cd esque-lsp

make build              # produces ./esque-lsp
make install            # copies it to ~/.local/bin (override with PREFIX=...)
make install PREFIX=/usr/local
```

Or with raw `go`:

```bash
go install ./...
```

After install, verify:

```bash
esque-lsp --version
```

## Editor configuration

### Neovim (nvim-lspconfig)

```lua
local lspconfig = require('lspconfig')
local configs = require('lspconfig.configs')

if not configs.esque_lsp then
  configs.esque_lsp = {
    default_config = {
      cmd = { 'esque-lsp' },
      filetypes = { 'esque' },
      root_dir = lspconfig.util.root_pattern('go.mod', '.git'),
      single_file_support = true,
    },
  }
end

lspconfig.esque_lsp.setup({})

-- Recognise .esq files as the `esque` filetype.
vim.filetype.add({ extension = { esq = 'esque' } })
```

### Helix

In `~/.config/helix/languages.toml`:

```toml
[[language]]
name = "esque"
scope = "source.esque"
file-types = ["esq"]
roots = ["go.mod", ".git"]
language-servers = ["esque-lsp"]
comment-token = "#"

[language-server.esque-lsp]
command = "esque-lsp"
```

### VS Code

Use a generic LSP client extension (e.g. *generic-lsp*) and point it
at `esque-lsp`, or write a thin extension. A reference extension is
out of scope of this repository — the server itself is editor agnostic.

### Logging

Pass `--log <path>` to write server logs (incoming/outgoing messages
and internal errors) to a file:

```bash
esque-lsp --log /tmp/esque-lsp.log
```

## Capabilities

| Capability                      | Status |
|---------------------------------|--------|
| `textDocument/didOpen/Change`   | Full sync |
| `textDocument/publishDiagnostics` | Lex errors, "no `fn main`" hint |
| `textDocument/hover`            | Keywords, types, operators, builtins, user fns |
| `textDocument/completion`       | Keywords, types, builtins, in-file fns |
| `textDocument/documentSymbol`   | Top-level `fn` declarations (hierarchical) |
| `textDocument/definition`       | Jump to top-level function definitions |

Type-aware diagnostics (matching what `esquec check` reports) are
*not* yet implemented — they require running the actual type-checker.
A future revision can shell out to `esquec check --json` and
overlay the results.

## Project layout

```
.
├── main.go        — entry point, flag handling
├── jsonrpc.go     — Content-Length framed JSON-RPC 2.0 transport
├── protocol.go    — LSP type definitions (subset used here)
├── lexer.go       — esque lexer (independent re-implementation)
├── symbols.go     — token-stream symbol indexer
├── lang.go        — keyword/type/builtin documentation tables
├── server.go      — LSP message dispatch
├── Makefile
└── README.md
```

## License

MIT (matching the upstream esque project).
