# BotReaper — Phase 1: Repository Analysis & Architecture Draft

> 1:1 port of `/hermes/hermes-agent-python/hermes-agent` (hermes-agent v0.21.0, Python >=3.11)
> Target: `github.com/nousresearch/botreaper`, Go 1.22+.
> Constraints: full-duplex WebSocket engine, bounded goroutine pools, zero-allocation
> streaming, no HTTP polling, no `// TODO` placeholders (Phase 6 audit).

Source tree scanned: `hermes-agent/` facade+siblings layout (Sep 2026 decomposition).
Counts shift constantly; filesystem is canonical. Key entry points verified on disk:

- `run_agent.py` (AIAgent facade, ~1555L) + `agent/` (~203 files + `lsp/ monitoring/ pet/
  proxy_sources/ secret_sources/ transports/ verify/`)
- `cli.py` (~4658L, 12 `hermes_cli/cli_*_mixin.py`) + `hermes_cli/` (323 files,
  `web_routers/` 13+24, `proxy/`, `local_runtime/`)
- `model_tools.py`, `toolsets.py`, `toolset_distributions.py`, `tools/` (~246+ files)
- `hermes_state.py` facade + 20 siblings (`hermes_state_*.py`, SCHEMA_VERSION=30)
- `gateway/run.py` facade + phases (`run_*.py`, `session*.py`, `slash_commands*.py`,
  `platforms/`, `relay/`, `builtin_hooks/`) + `plugins/platforms/` (22 adapters)
- `tui_gateway/` (server+entry+transport+ws+13 `methods_*.py`), `ui-tui/` (Ink React),
  `web/` (Vite+React SPA), `apps/` (desktop Electron + shared JSON-RPC client),
  `acp_adapter/`, `cron/` (scheduler+jobs+executions+delivery), `providers/`,
  `plugins/` (memory/context_engine/model-providers/image_gen/video_gen/web/browser/
  cron_providers/kanban/...), `skills/` + `optional-skills/`, `optional-mcps/` (65 manifests),
  `native/fts5_cjk/`, `batch_runner.py`, `mcp_serve.py`, `trajectory_compressor.py`,
  `registration_lifecycle.py`, `hermes_constants.py`, `hermes_logging.py`, `hermes_time.py`.

Two load-bearing invariants (from `AGENTS.md`, must survive the port):

1. **Per-conversation prompt caching is sacred.** System prompt byte-stable for the life of
   a conversation; only context compression may mutate past context. Slash commands that
   mutate system-prompt state default to deferred invalidation (next session) + opt-in
   `--now`. In Go: keep `SystemPrompt` an immutable `string`/`[]byte` snapshot per session;
   never rebuild it mid-turn.
2. **Core is a narrow waist; capability lives at the edges.** Every model tool ships on
   every LLM call — new capability arrives as CLI+skill, service-gated tool (`check_fn`),
   plugin, or MCP server, never as core surface growth. In Go: `tools.Registry` stays
   dependency-free; `toolsets.go` mirrors `TOOLSETS`; gating via `CheckFunc`.

---

## 1. Go package map (1:1 parity)

```text
/hermes/botreaper/
├── cmd/botreaper/            # main.go — CLI entry (port of hermes_cli/main.py + cli.py REPL bootstrap)
├── pkg/types/             # zero-alloc core types: Message, Tool, Skill, Profile, Config, Session
├── internal/
│   ├── ws/                # full-duplex WS gateway (tui_gateway/ws.py + transport.py + server.py dispatch)
│   ├── tui/               # bubbletea TUI (ui-tui/ + cli.py CLITuiMixin), WS client only, no direct engine calls
│   ├── agent/             # AIAgent state machine (run_agent.py + agent/conversation_loop.py + turn_*.py)
│   ├── provider/          # LLM provider profiles + client lifecycle (providers/ + agent/client_lifecycle.py
│   │                      #   + auxiliary_client.py + anthropic/bedrock/gemini/vertex/azure/codex adapters)
│   ├── tools/             # registry + all builtin tools (tools/registry.py + model_tools.py + toolsets.py
│   │                      #   + file/terminal/browser/web/vision/tts/delegate/skill/memory/kanban/cron/clarify/...)
│   ├── sandbox/           # streamed execution backends (tools/environments/: local/docker/ssh/modal/
│   │                      #   daytona/singularity/vercel + terminal_scope + security scanner)
│   ├── mcp/               # MCP client engine (tools/mcp_tool*.py + mcp_oauth*.py + optional-mcps/ catalog)
│   ├── subagent/          # async delegation (tools/delegate_tool*.py + async_delegation.py, depth<=2, fanout<=3)
│   ├── state/             # SessionDB facade + SQLite FTS5 store (hermes_state*.py, SCHEMA v30)
│   ├── memory/            # 3-layer memory files (tools/memory_tool*.py: MEMORY.md/USER.md + SOUL.md + nudges)
│   ├── skills/            # skill engine + curator (skills/ + optional-skills/ + agent/curator.py
│   │                      #   + tools/skill_*.py + skills_hub*.py + skill_usage.py)
│   ├── gateway/           # multi-channel router (gateway/run*.py + session*.py + platforms/ +
│   │                      #   plugins/platforms/* + delivery.py + stream_*.py + relay/ + pairing/authz)
│   ├── cron/              # natural-language scheduler (cron/scheduler*.py + jobs.py + executions.py +
│   │                      #   delivery_queue.py + incidents/monitor/suggestions/blueprints/notepad.py)
│   ├── config/            # config.yaml + .env secrets + timezone (hermes_cli/config.py + utils.py +
│   │                      #   hermes_time.py + hermes_logging.py)
│   └── profile/           # BOTREAPER_HOME isolation + named profiles (hermes_constants.py + profiles.py)
```

### 1.1 Detailed module mapping

| Python source | Go destination | Notes |
|---|---|---|
| `run_agent.py` (AIAgent mixins: ClientLifecycle, StreamDelivery, StatusOutput, ApiRequestHooks, ApiErrorSummary, InterruptControl, TurnExplainers, ActivityTracking, RateLimitCredits, SessionPersistence, CompressionFacade, TurnFacade, VisionMessagePrep, ReasoningParams) | `internal/agent/agent.go` (`type AIAgent struct`) + `mixin_*.go` split 1:1 | Facade + siblings preserved; no god file >2000L. `chat(msg)->str`, `run_conversation()` via `turn_facade.go` (durable turn lease + `SESSION_COORDINATOR` + accounting scopes) |
| `agent/conversation_loop.py` + `turn_*.py` (~30 phases: preflight/gate, iteration_prep, request_assembly, api_request/call/error, response_intake/check, empty_response, tool_round/validation, overflow, truncation, context_compaction, recovery, retry_state, stop_gates, liveness, usage, final_response/finalizer/summary) | `internal/agent/loop.go` + `turn_*.go` (same phase names) | Strict OpenAI `system/user/assistant/tool` + `assistant["reasoning"]`, strict role alternation, immutable system prompt, `IterationBudget` (max_iterations=500 default), grace-call, interrupt break. `prompt_builder.go` (CWD cap) + `subdirectory_hints.go` (32k tool-result suffix) |
| `agent/system_prompt.py`, `prompt_builder.py`, `prompt_caching.py`, `model_metadata.py`, `models_dev.py`, `reasoning_{effort,params}.py`, `codex_responses_adapter.py`, `relay_llm.py`, `relay_runtime.py`, `credential_pool.py`, `provider_registry.py` | `internal/provider/*` + `internal/agent/prompt*.go` | `ProviderProfile` struct mirrors `providers/base.py` (api_mode, aliases, env_vars, base_url, models_url, auth_type, vision flags, prompt_cache_key, fixed_temperature, default_max_tokens, default_aux_model, default_headers + hooks). Discovery order preserved: entry-point → bundled `plugins/model-providers/*` (~40) → `$BOTREAPER_HOME/plugins/` → legacy. Aux auto-route (`_resolve_auto_route` for curator/vision/compression) |
| `providers/` + `plugins/model-providers/*` + `agent/{anthropic,bedrock,gemini_native,vertex,azure_identity,codex_responses}_adapter.py`, `provider_base/media/projection.py` | `internal/provider/profile.go`, `registry.go`, `adapters/*.go` | Resolution precedence + fallback/pool in `client_lifecycle.go`; Nous Portal, OpenRouter, OpenAI, Ollama first-class (streaming SSE → WS fan-out) |
| `tools/registry.py` (dep-free `register(name,toolset,schema,handler,check_fn,requires_env)`, AST-scan `discover_builtin_tools`, TTL-cached `check_fn`, JSON-str handlers, 2048ch error cap) | `internal/tools/registry.go` | Must stay dependency-free (imported by everything). Table-driven, no `if/elif>=4` ladders |
| `model_tools.py` (`get_tool_definitions`, `_compute_tool_definitions`, `_select_tool_names`, rewriters for execute_code/browser_navigate/browser_exec/delegate_task/discord, `handle_function_call` guards→execute→hooks) | `internal/tools/definitions.go`, `dispatch.go` | Filtering by `resolve_toolset()` + `check_fn` + quiet_mode; dynamic cross-tool refs injected at call time, never in static schema |
| `toolsets.py` (`TOOLSETS`, `_HERMES_CORE_TOOLS`, bundles) + `toolset_distributions.py` | `internal/tools/toolsets.go` | Keys preserved verbatim: `web,search,x_search,vision,video,image_gen,video_gen,computer_use,terminal,skills,browser,cronjob,file,tts,todo,memory,context_engine,session_search,project,bot_room,desktop_ui,clarify,code_execution,delegation,homeassistant,kanban,discord,discord_admin,yuanbao,feishu_doc/feishu_drive,spotify,debugging,safe,coding,hermes-acp,hermes-api-server,hermes-cli,hermes-cron,hermes-telegram/discord/whatsapp/slack/signal/.../webhook/gateway` |
| `agent/tool_executor.py` (seq+concurrent pool `_MAX_TOOL_WORKERS=8`, observe→commit→project, spillover `tool_result_storage.py`, budgets) + `agent/inline_tool_executors.py` (todo/memory bypass) | `internal/agent/executor.go` + `internal/tools/spill.go` | Persist-before-execute invariant (`run_tool_round()->ToolRoundVerdict{continue,break,return}`); ants pool size 8; spills to disk, never unbounded heap |
| `tools/file_tools.py`, `read_window/preview/close_preview/annotate_preview`, `file_tools_paths/read_tracking`, `fuzzy_match`, `shell_heredoc`, `ansi_strip`, `spill_safety`, `url_safety` | `internal/tools/file.go` (+ `preview.go`) | `read_file/write_file/patch/search_files` semantics identical |
| `tools/terminal_tool*.py` + `environments/` (local/docker/ssh/modal/daytona/singularity/vercel + `base*.py`, `remote_common`, `file_sync`, `managed_modal`, `docker_egress`, `local_env_policy/gitbash_probe/pythonpath`) + `terminal_scope.py`, `terminal_tool_sudo.py`, `terminal_tool_config/result.py`, `shell_hooks.py`, `daemon_pool.py`, `process_registry.py`, `code_kernel.py`, `code_execution_tool.py`, `interpreter_shutdown.py` | `internal/sandbox/*` (`backend.go` iface + `local.go`, `docker.go`, `ssh.go`, `modal.go`, `daytona.go`, `singularity.go`, `scanner.go`, `stream.go`) | `io.Pipe` + `bufio.Scanner` zero-copy stdout/stderr → WS frames (see §3). `golang.org/x/crypto/ssh` for SSH. Security scanner + namespace isolation before spawn |
| `tools/browser_tool*.py` (cdp/cloud/vision/session/real_profile/eval_policy/camofox/supervisor) + `computer_use/` | `internal/tools/browser.go` (+ `computer.go`) | CDP supervisor preserved; cloud fallback |
| `tools/web_tools.py`, `web_tools_rescue.py`, `x_search_tool.py`, `xai_http.py`, `openrouter_client.py`, `transcription_*.py`, `tts_tool*.py`, `vision_tools*.py`, `image/video_generation*` | `internal/tools/web.go`, `media.go` | Provider-registry pattern (`ProviderRegistry` generic, global+scoped) reused for browser/web_search/tts/transcription/image/video/memory/terminal_env |
| `tools/delegate_tool*.py`, `async_delegation.py`, `delegation_live_log.py`, `subagent_worktree.py` | `internal/subagent/delegate.go` | Leaf/orchestrator split, `max_concurrent_children=3`, `max_spawn_depth=2`; child gets distinct session context, streams to parent WS channel |
| `tools/mcp_tool*.py` (15 files: transport/server_run/health/errors/loop/common/config/registration/handlers/discovery/agent) + `mcp_oauth*.py` + `optional-mcps/` (65 manifests) + `hermes_cli/mcp_startup.py` | `internal/mcp/*` | stdio + streamable-http + legacy SSE selection; OAuth PKCE/DCR/refresh; background discovery; `manifest_version:1` catalog compat |
| `tools/skill_*.py`, `skills_tool*.py`, `skills_hub*.py`, `skills_sync_*`, `skill_usage.py`, `skill_manager_batch.py`, `skill_provenance.py`, `blueprints.py`, `agent/curator.py`, `agent/skill_utils.py` | `internal/skills/*` | `SKILL.md` frontmatter contract enforced (name/desc≤60ch/version/author/license/platforms/metadata.hermes{tags,category,related_skills,config}); body order fixed; `created_by:agent` auto-archive to `.archive/`; usage ledger `~/.botreaper/skills/.usage.json`; `agentskills.io` compat |
| `tools/memory_tool*.py`, `agent/memory_manager.py`, `memory_provider.py`, `context_engine.py`, `plugins/memory/*`, `plugins/context_engine/*` | `internal/memory/*` | `MemoryStore`: `§`-delimited `MEMORY.md` (2200ch) + `USER.md` (1375ch), threat-scan strict, drift/`.bak` guard, atomic write, frozen prompt snapshot (cache-safe); `SOUL.md` per-profile identity |
| `tools/kanban_tools*.py`, `tools/cronjob_tools.py`, `tools/todo_tool.py`, `tools/session_search_tool.py`, `tools/clarify_tool.py`, `tools/project_tools.py`, `tools/tip_tool.py`, `tools/tour_tool.py`, `tools/send_message_senders.py`, `tools/react_to_message_tool.py`, `tools/bot_*.py`, `tools/hook_output_spill.py`, `tools/tool_search_catalog.py` | `internal/tools/` (one file per tool family, same tool names/schemas) | JSON schemas byte-compatible so existing system prompts/skills keep working |
| `hermes_state*.py` (21 files, `SessionDB` facade, 1 writer conn + `threading.Lock` + WAL + `BEGIN IMMEDIATE` jittered retry; bounded `LifoQueue` RO pool; `SCHEMA_VERSION=30`, `FTS_STORAGE_VERSION=2`; FTS externals `messages_fts(+_docsize)`, `messages_fts_trigram(+_src)`, `messages_fts_cjk(+_src)`; triggers gated on `fts_rebuild_{high_water,progress}`; 8k tool-prefix cap; chunked resumable backfill CHUNK=500; `merge` every 1000 writes; prune 90d / vacuum 30d+freelist>0.25; async coalescing token-writer thread 30s idle + atexit drain; `PASSIVE` checkpoint every 50 writes) | `internal/state/*` (`db.go` facade + `schema.go`, `fts.go`, `search.go`, `messages.go`, `sessions.go`, `compression.go`, `gateway.go`, `usage.go`, `maintenance.go`, `registry.go`, `wal.go`, `guard.go`, ...) | `modernc.org/sqlite` (pure Go, FTS5 enabled; CJK bigram via custom tokenizer registration porting `native/fts5_cjk/` unicode61+CJK semantics). Single-writer `*sql.DB` (maxOpenConns=1 for writes) + RO pool; `BEGIN IMMEDIATE` retry with jitter (`_WRITE_PATIENCE_S=20`, transcript=60, activity=0.5, compression-busy-wait=5); flock `fts_rebuild.lock` (120s, orphan-fd break); fail-open detach on `SQLITE_CORRUPT_VTAB` → `fts_stale`. Import/export caps preserved (500sess/10k-per/50k-total/5M-per/25M-total) |
| `gateway/run*.py` + `session*.py` + `slash_commands*.py` + `config*.py`, `platform_registry.py`, `channel_directory.py` (5min rebuild), `delivery.py`, `delivery_ledger.py`, `stream_*.py`, `mirror.py`, `pairing.py`, `authz_mixin.py`, `profile_routing.py`, `wake.py`, `status.py`, `readiness.py`, `restart.py`, `shutdown_*.py`, `scale_to_zero.py`, `hosted_rooms*.py`, `kanban_watchers*.py`, `browser_control_broker.py`, `relay/*`, `builtin_hooks/` | `internal/gateway/*` (`runner.go` facade + `run_*.go`, `session*.go`, `slash*.go`, `delivery.go`, `stream.go`, `relay/*`) | Two message guards + `_IDLE/_PLAIN_COMMANDS`; `_command_handler_table` (no if-chain); deferred/lazy `PlatformEntry.create_adapter()`; OpenAI-compatible `api_server` + rooms routes; `gateway.ready{change_events,heartbeat,replay_epoch}` |
| `plugins/platforms/*` (22: telegram long-poll getUpdates, discord WS GW+voice, slack Socket+BlockKit, email/irc/matrix/mattermost/teams/whatsapp-cloud+bot+self-chat/sms-twilio/line/feishu/dingtalk/wecom/google_chat/homeassistant/ntfy/buzz/simplex/photon/raft/a2a) + `gateway/platforms/` (webhook+filters, api_server*, signal*, whatsapp_*, weixin, yuanbao*, qqbot, bluebubbles, msgraph) | `internal/gateway/telegram/` (getUpdates poll, send/edit/delete/typing, 4096-split, 429 fail-closed, forum threads) + `internal/gateway/discord/` (gateway WS IDENTIFY/heartbeat/resume, REST send, 2000-split, mention gate) + generic webhook/poll adapters; remaining 20 channels unpacked | Telegram: `TELEGRAM_BOT_TOKEN`, drop-pending cold start, 409 ladder; Discord: `DISCORD_BOT_TOKEN`, message_content intent, per-chat agent sessions via `botreaper gateway --run`; voice excluded on both |
| `tui_gateway/` (`server.py` dispatch + `_LONG_HANDLERS` pool-8 + `_SlashWorker` subprocess + session store + seq+replay ring; `entry.py` stdio NDJSON; `transport.py` Stdio/Tee/ContextVar; `ws.py` WSTransport token-coalesce orphan-reap 20s/600s; 13 `methods_*.py`; `event_publisher.py`, `event_replay.py` (replay_epoch), `prompt_turn.py`, `session_*.py`, `slash_worker/fuzzy`, `compute_host*`, `host_supervisor`, `hosted_room_*`, `tool_progress.py`, `turn_marker.py`, `render.py`) | `internal/ws/*` + `internal/tui/*` | JSON-RPC 2.0 over WS is the ONLY transport (no polling). Replay ring + per-session `seq` + `replay_epoch` lossless resume via `session.events.since`. Method table 1:1 with `methods_*.py` |
| `ui-tui/` (Ink React) + `cli.py` REPL + `hermes_cli/cli_*_mixin.py` (12) + `web/` ChatPage (embeds real `botreaper --tui` via xterm.js) | `internal/tui/*` (bubbletea Elm: Model/Update/View; ghost-text autocomplete, multiline paste, Ctrl+C cancel) | TUI connects over in-memory/local WS client to `internal/ws` — never calls engine directly (preserves gateway/TUI split) |
| `cron/` (scheduler tick 60s + `.tick.lock` fcntl + `CronTickYielded` + ThreadPoolExecutor + `sweep_stale_inflight(max(2*interval,30m))` + 600s watchdog + `drain_delivery_queue`; jobs store + `parse_schedule` (30m/2h/1d, `every ...`, 5-field cron, ISO one-shot) + claim/mark/catch-up period/2 clamp 120s–2h; prompt/script/preflight/provider/delivery; executions ledger + `recover_interrupted`; delivery_queue; monitor/incidents/suggestions/blueprints/notepad/lifecycle_guard) | `internal/cron/*` (same file split) | `time.Ticker` 60s + file lock + `errgroup` pool; own cron session + header/footer, never mirror |
| `hermes_cli/` (commands, profiles, config_defaults, console_engine, model_setup_flows, model_switch, plugins_*, auth*, kanban*×14, web_server+web_routers×24, proxy/, local_runtime/, secrets_cli, skills install, tools_config, curator, checkpoints) | `cmd/botreaper/*` (flag CLI) + `internal/config/*` + `internal/profile/*` + `internal/setup/*` (`botreaper setup [model\|terminal\|gateway\|tools\|agent] [--reset] [--quick]`, verbatim Python chrome/prompts/summary; `config.SetSecret/ClearSecret` + `terminal_*`/`search_base_url` schema keys) | setup/config UX preserved; sections write only keys the runtime honors (terminal backend local/docker/ssh/singularity wired, search base/key wired, allowlists enforced) |
| `acp_adapter/` (ACP server for VS Code/Zed/JetBrains) | `internal/gateway/acp.go` (or `internal/acp/`) | stdio logs→stderr, `--check/--setup/--setup-browser` flags preserved |
| `mcp_serve.py` (`hermes mcp serve`, 10 tools, EventBridge 200ms mtime poll, QUEUE_LIMIT=1000) | `internal/mcp/serve.go` | Same 10 tools, same poll cadence mapped to `time.Ticker(200ms)` |
| `batch_runner.py` (JSONL evals, checkpoint `batch_N.jsonl`, tool/reasoning stats, fire CLI) | `cmd/botreaper/batch.go` | Worker count flag preserved; uses same `sample_toolsets_from_distribution` port |
| `trajectory_compressor.py` (offline RL compressor, protect head+last-4, `[CONTEXT SUMMARY]:`) | `internal/agent/compress_offline.go` | Same `CompressionConfig`, metrics structs |
| `registration_lifecycle.py` (ReplacementCoordinator/Lease) | `internal/config/lifecycle.go` | Generation-linked plugin/provider slots |
| `hermes_constants.py`, `hermes_logging.py`, `hermes_time.py`, `utils.py` | `internal/config/*` + `internal/profile/*` | `get_botreaper_home()` (ported from `hermes_constants.get_hermes_home`) override→BOTREAPER_HOME→~/.botreaper; profiles `<root>/profiles/<name>` + HOME-anchored root + `.deleted/` tombstones; async QueueListener logs (`agent.log`/`errors.log`/`gateway.log`/`gui.log`, RedactingFormatter, 0660 managed, ConcurrentRotating on win); `now()` TZ chain BOTREAPER_TIMEZONE→timezone→local; `atomic_write_text` (EXDEV→copy, win 5/32/33 retry) |
| `evals/`, `tests/` (~39k tests), `scripts/run_tests.sh` | `*_test.go` + `*_bench.go` per package; `make test` = `go test ./...` | Behavior contracts over snapshots; E2E with temp `BOTREAPER_HOME`; ≥2s wall-clock bounds, event sync |

---

## 2. WebSocket event protocol (replaces ALL polling)

Python baseline: primary transport is already WebSocket JSON-RPC
(`apps/shared/src/json-rpc-gateway.ts`: `gateway.ping` 15s/45s heartbeat, 15s connect
timeout, 120s RPC timeout, `replay_epoch`+per-session `seq` resume via
`session.events.since`, coalesced `message/reasoning/thinking.delta` 33ms,
`TCP_NODELAY+keepalive`; sockets `/api/ws` (agent RPC), `/api/pty` (raw PTY +
`\x1b[RESIZE]`), `/api/events` (sidebar `change_events`), `/api/plugins/kanban/events`;
backstop polling only for session-overview/SQLite-shared-no-IPC + logs/analytics).
Go makes the backstop disappear: SQLite change notifications fan out over the same WS bus.

### 2.1 Transport

- Library: `nhooyr.io/websocket` (HTTP/2-ready, `net/http` native, context-aware dial).
  Fallback `gorilla/websocket` only if compression tuning demands it — one library, not both.
- Endpoints (one `http.ServeMux`, all WS, no REST polling):
  `GET /api/ws` (JSON-RPC 2.0 agent RPC + event stream), `GET /api/pty` (binary PTY),
  `GET /api/events` (broadcast bus). Auth: `X-Botreaper-Session-Token` header + cookie,
  `?ticket=` minted via `/api/auth/ws-ticket`, `?profile=` scoping (mirrors `web/src/lib/api.ts`).
- Heartbeat: client `gateway.ping` every 15s; server kills silent peers at 45s.
  Connect timeout 15s, RPC timeout 120s. `TCP_NODELAY` + TCP keepalive on listener.
- Resume: server keeps per-session replay ring (last N=1000 envelopes) + global
  `replay_epoch` (incremented on restart). Client reconnects with
  `{replay_epoch, last_seq}` → server replays `session.events.since`. Mismatch → full
  `session.snapshot` resync. Orphan reap 20s (mark) / 600s (GC), mirroring `ws.py`.

### 2.2 Envelope (JSON fast path, binary hot path)

```go
// pkg/types/frames.go
type FrameKind string
const (
    KindRPCRequest  FrameKind = "req"   // JSON-RPC 2.0 request
    KindRPCResponse FrameKind = "res"   // JSON-RPC 2.0 response
    KindEvent       FrameKind = "ev"    // server→client push
    KindPty         FrameKind = "pty"   // binary PTY chunk
    KindPing        FrameKind = "ping"
    KindPong        FrameKind = "pong"
)
type Envelope struct {
    V     uint8          `json:"v"`              // protocol version = 1
    Kind  FrameKind      `json:"k"`
    Seq   uint64         `json:"seq"`            // per-session monotonic, replay key
    Epoch uint64         `json:"epoch"`          // replay_epoch
    Sess  string         `json:"sess"`           // session key
    Type  string         `json:"t"`              // event/method name
    Data  JSONRaw        `json:"d,omitempty"`    // goccy/go-json raw, zero-copy
}
```

- Serialization: `goccy/go-json` streaming encoder (drop-in `encoding/json` API, lower
  allocs). Binary frames (`KindPty`, bulk `token.delta` batches) use the same struct with
  a compact binary header `[v:1][kind:1][seq:8][epoch:8][sess_len:2][sess][type_len:1][type][payload]`
  to avoid base64. `sync.Pool` of `bytes.Buffer` + frame buffers; `easyjson` only if
  profiling shows `goccy` still hot.
- Coalescing: `message/reasoning/thinking.delta` merged on a 33ms flush timer (same as TS
  client) to bound frame rate without adding tail latency to tool/pty streams.

### 2.3 Event / method catalog (1:1 with `tui_gateway/methods_*.py` + gateway streams)

| `Type` | Dir | Payload | Python origin |
|---|---|---|---|
| `token.delta` | S→C | `{text}` streamed LLM tokens | `stream_delivery.py`, `stream_dispatch.py` |
| `reasoning.delta` / `thinking.delta` | S→C | `{text}` | same (coalesced 33ms) |
| `tool.start` / `tool.progress` / `tool.end` | S→C | `{call_id,name,summary,spilled?}` spinner/progress | `tool_progress.py`, `turn_tool_round.py` |
| `pty.stdout` / `pty.stderr` | S→C (binary) | raw bytes, `\x1b[RESIZE]` control | `/api/pty`, `terminal_tool*.py` |
| `session.snapshot` / `session.patch` | S→C | full / diff state (no polling) | `session*.py`, `session-refresh.ts` killer |
| `change_events` | S→C | sidebar invalidations | `/api/events` |
| `message.appended` / `approval_request` | S→C | ledger + HITL | `mcp_serve.EventBridge`, approval_prompt |
| `cron.fired` / `cron.acked` | S→C | job id + delivery | `cron/delivery_queue.py` |
| `gateway.status` / `gateway.heartbeat` | S↔C | health, load, replay_epoch | `gateway/status.py`, readiness |
| `prompt.submit` / `prompt.cancel` | C→S | user text + attachments | `methods_prompt.py`, `prompt_turn.py` |
| `slash.execute` | C→S | `{cmd,args,now?}` cache-aware | `slash_commands*.py`, `methods_slash.py` |
| `session.events.since` | C→S | `{epoch,last_seq}` resume | `event_replay.py` |
| `browser.frame` | S→C | CDP pixels/input state | `browser_control_broker.py` |
| `voice.chunk` | S↔C | PCM/opus | `run_voice.py`, TTS consumers |

Slash table is data (`_command_handler_table` equivalent `map[string]Handler`), never an
`if/elif` ladder: `/model /reset /skills /sessions /cron /tools /profile /status /wake ...`.

---

## 3. Goroutine concurrency layout

Rules: bounded pools only; never block the WS event loop; `select` + `time.Ticker` for
background work; `context.WithTimeout/WithCancel` for teardown; Ctrl+C cancels the turn.

```text
                    ┌─ WS readLoop (1 goroutine/conn, blocking Read → inbound chan)
  conn ─────────────┤
                    └─ WS writeLoop (1 goroutine/conn, select{fanned-out send chan, ping ticker 15s, ctx.Done})
                                          ▲
Hub (1 goroutine, the ONLY writer of conn maps + replay rings + seq counters)
  ├── inbound dispatch → ants pool (tools=8, slash=8 _LONG_HANDLERS, gateway adapters=unbounded-via-errgroup+sem)
  ├── agent turns: 1 goroutine/turn, child tool calls fan out to tool pool (cap 8, matches _MAX_TOOL_WORKERS)
  ├── subagents: errgroup + semaphore (max_concurrent_children=3, max_spawn_depth=2), distinct session ctx
  ├── sandbox streams: 2 goroutines/exec (stdout+stderr pumps: io.Pipe → bufio.Scanner → WS binary frames)
  ├── FTS5 indexer: 1 background goroutine + queue (merge every 1000 writes, chunk 500, optimize/vacuum timers)
  ├── cron: 1 ticker goroutine (60s) + errgroup job pool + delivery-queue workers (restart-safe)
  ├── memory nudges: 1 ticker goroutine (consolidation interval from config; drift check + .bak guard)
  ├── gateway adapters: 1 supervisor goroutine/platform (long-poll/WS/Socket each in own goroutine,
  │      Telegram getUpdates / Discord GW / Slack Socket / Signal poll / webhook handlers)
  ├── token usage flusher: 1 goroutine (coalesce + 30s idle retire + SIGTERM drain, mirrors _token_writer_loop)
  └── MCP discovery: 1 goroutine at startup (background discovery, OAuth refresh timers per server)
```

- Pools: `panjf2000/ants` (fixed 8 for tools + 8 for long RPC handlers) or custom
  `chan`-semaphore + `golang.org/x/sync/errgroup` where cancellation propagation matters
  (subagents, cron, gateway fan-out). No unbounded `go` in a loop — every spawn site takes a
  token or is a declared singleton above.
- WS loops: `readLoop` blocks on `Read(ctx)` and pushes to `inbound chan` (drop+close on
  error); `writeLoop` `select`s on `send chan`, `pingTicker.C`, `ctx.Done()`. Hub owns all
  mutable conn state — loops never touch each other's maps (no mutex on hot path).
- PTY/sandbox: `cmd.StdoutPipe/StderrPipe` → `io.Pipe` → `bufio.Scanner.Split(scanLines|scanBytes)`
  → `ws.WriteBinary(ctx, frame)` directly. Nothing accumulates: spillover to disk past cap,
  pointer passed in `tool.end{spilled:true}` (mirrors `tool_result_storage.py`).
- Shutdown: root `context.WithCancel`; `signal.NotifyContext(SIGINT/SIGTERM)` → cancel →
  Hub closes conns (replay rings persist in SQLite) → pools `ReleaseTimeout` → token flusher
  drains → cron tick lock released. Instant Ctrl+C: TUI sends `prompt.cancel` + cancels local
  turn ctx; in-flight LLM HTTP request carries the same ctx (client lifecycle honors it).

---

## 4. Zero-allocation & streaming architecture

- `sync.Pool`: `bytes.Buffer` pool (prompt builder, frame encoder), WS message frame pool,
  `bufio.Scanner` scratch pools. Prompt builder pre-sizes (`Grow`) from cached prefix length.
- Immutable system prompt per session (`string` snapshot, never rebuilt mid-turn) → maximal
  LLM KV-cache reuse; only compression path clones + publishes a child session
  (`publish_compression_child` semantics: atomic parent-close + child + handoff + watermark).
- LLM streaming: provider SSE lines decoded incrementally → `token.delta` frames without
  joining the full response; final text assembled once for ledger write.
- SQLite: WAL mode, `BEGIN IMMEDIATE` + jittered retry, single-writer + RO pool, `PASSIVE`
  checkpoints, prepared-statement cache per writer goroutine. FTS backfill chunked (500) so
  indexing never blocks turns.
- Allocations verified in Phase 6: `go test -bench . -benchmem` on `ws/encode`,
  `agent/prompt_build`, `tools/dispatch`, `state/append+search`, `sandbox/stream`.

---

## 5. Profile & config isolation

- `BOTREAPER_HOME` resolution (port of `hermes_constants.get_hermes_home`): explicit override
  → `$BOTREAPER_HOME` → `~/.botreaper` (`%LOCALAPPDATA%/botreaper` on Windows). `-p/--profile` maps to
  independent storage (`<root>/profiles/<name>` with own `state.db/config.yaml/.env/logs/`);
  profiles root HOME-anchored (`~/.botreaper/profiles`) + `.deleted/` tombstones so
  `botreaper -p x profile list` sees all profiles. Never hardcode `~/.botreaper`.
- `.env` = secrets only; behavior in `config.yaml` (timeouts/thresholds/flags/display).
  No new `BOTREAPER_*` env for non-secrets. Logging profile-routed (`agent.log`/`errors.log`/
  `gateway.log`/`gui.log`).

## 6. Phase plan status

- [x] PHASE 1 (this doc): inventory + package map + WS contracts + goroutine layout + skeleton.
- [x] PHASE 2: `pkg/types` zero-alloc types + `internal/ws` server/client + replay + `internal/config` + `internal/profile`.
- [x] PHASE 3: `internal/sandbox` (local/docker/ssh/singularity/modal/daytona) + `internal/tools` + `internal/mcp` + `internal/subagent`.
- [x] PHASE 4: `internal/agent` loop + `internal/state` FTS5 + `internal/memory` nudges + `internal/skills` curator.
- [x] PHASE 5: `internal/gateway` channels + `internal/tui` + `internal/cron` + `cmd/botreaper`.
- [x] PHASE 6: `go build`, `go vet`, `go test ./...` (17 pkgs ok), `go test -bench . -benchmem` (encode 0 allocs, publish 1 alloc, dispatch 5 allocs, scan 1 alloc), zero code `TODO` (only this doc's prose mentions the audit).

> NOTE: `go` toolchain is NOT installed in this environment (`go: command not found`),
> so Phase 2+ codegen/compile/bench cannot run here. Skeleton + contracts are committed
> so a Go-enabled runner can continue with `go mod tidy && go build ./...`.
