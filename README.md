# BotReaper

```
▄▄▄▄    ▒█████  ▄▄▄█████▓ ██▀███  ▓█████ ▄▄▄       ██▓███  ▓█████  ██▀███  
▓█████▄ ▒██▒  ██▒▓  ██▒ ▓▒▓██ ▒ ██▒▓█   ▀▒████▄    ▓██░  ██▒▓█   ▀ ▓██ ▒ ██▒
▒██▒ ▄██▒██░  ██▒▒ ▓██░ ▒░▓██ ░▄█ ▒▒███  ▒██  ▀█▄  ▓██░ ██▓▒▒███   ▓██ ░▄█ ▒
▒██░█▀  ▒██   ██░░ ▓██▓ ░ ▒██▀▀█▄  ▒▓█  ▄░██▄▄▄▄██ ▒██▄█▓▒ ▒▒▓█  ▄ ▒██▀▀█▄  
░▓█  ▀█▓░ ████▓▒░  ▒██▒ ░ ░██▓ ▒██▒░▒████▒▓█   ▓██▒▒██▒ ░  ░░▒████▒░██▓ ▒██▒
░▒▓███▀▒░ ▒░▒░▒░   ▒ ░░   ░ ▒▓ ░▒▓░░░ ▒░ ░▒▒   ▓▒█░▒▓▒░ ░  ░░░ ▒░ ░░ ▒▓ ░▒▓░
▒░▒   ░   ░ ▒ ▒░     ░      ░▒ ░ ▒░ ░ ░  ░ ▒   ▒▒ ░░▒ ░      ░ ░  ░  ░▒ ░ ▒░
 ░    ░ ░ ░ ░ ▒    ░        ░░   ░    ░    ░   ▒   ░░          ░     ░░   ░ 
 ░          ░ ░              ░        ░  ░     ░  ░            ░  ░   ░     
      ░ 
```

High-performance Go port of `hermes-agent-python/hermes-agent` (v0.21.0),
rebranded as BotReaper.

See `ARCHITECTURE.md` for the Phase 1 inventory, 1:1 package map,
WebSocket frame contracts, and goroutine concurrency layout.

Status: Phases 1–6 complete. `go build ./...`, `go vet ./...`,
`go test ./...` (22 packages ok) and `go test -bench . -benchmem` all green.

```bash
go mod tidy
go build ./...
go test -bench . -benchmem ./...
```

Build the binary with `go build -o botreaper ./cmd/botreaper`.
Home directory defaults to `~/.botreaper` (`BOTREAPER_HOME` overrides it;
legacy `HERMES_HOME` is still honored).

## Install

```bash
./install.sh                  # build, install to ~/.local/bin, wire PATH in ~/.bashrc
./install.sh --prefix /usr/local/bin --skip-setup   # system-wide, no wizard
```

The installer appends one idempotent `PATH` block to `~/.bashrc`
(`$HOME/.bashrc` for root) — restart your shell or `source ~/.bashrc` after.

## Setup & gateway

```bash
botreaper setup               # guided: provider → token → model → messaging
botreaper setup gateway       # just the Telegram/Discord tokens
botreaper gateway             # show which channels are configured
botreaper gateway --run       # start the messaging gateway (Ctrl+C to stop)
```

Provider keys live in `~/.botreaper/.env` (`OPENAI_API_KEY`,
`OPENCODE_ZEN_API_KEY`, … — `opencode-free` needs none); channel tokens as
`TELEGRAM_BOT_TOKEN` / `DISCORD_BOT_TOKEN` with optional `*_ALLOWED_USERS`
and `*_HOME_CHANNEL` entries.

License: MIT — see [LICENSE](LICENSE).
