// Command botreaper is the runtime entrypoint.
// Ports hermes_cli/main.py + cli.py REPL bootstrap.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nousresearch/botreaper/internal/agent"
	"github.com/nousresearch/botreaper/internal/brand"
	"github.com/nousresearch/botreaper/internal/config"
	"github.com/nousresearch/botreaper/internal/cron"
	"github.com/nousresearch/botreaper/internal/gateway"
	"github.com/nousresearch/botreaper/internal/gateway/discord"
	"github.com/nousresearch/botreaper/internal/gateway/telegram"
	"github.com/nousresearch/botreaper/internal/memory"
	"github.com/nousresearch/botreaper/internal/profile"
	"github.com/nousresearch/botreaper/internal/provider"
	"github.com/nousresearch/botreaper/internal/sandbox"
	"github.com/nousresearch/botreaper/internal/setup"
	"github.com/nousresearch/botreaper/internal/skills"
	"github.com/nousresearch/botreaper/internal/state"
	"github.com/nousresearch/botreaper/internal/subagent"
	"github.com/nousresearch/botreaper/internal/tools"
	"github.com/nousresearch/botreaper/internal/tui"
	"github.com/nousresearch/botreaper/internal/ws"
	"github.com/nousresearch/botreaper/pkg/types"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "botreaper: "+err.Error())
		os.Exit(1)
	}
}

func run(argv []string) error {
	fs := flag.NewFlagSet("botreaper", flag.ContinueOnError)
	prof := fs.String("p", "", "profile name")
	profileLong := fs.String("profile", "", "profile name")
	model := fs.String("model", "", "model override")
	provName := fs.String("provider", "", "provider override")
	baseURL := fs.String("base-url", "", "base URL override")
	homeOver := fs.String("home", "", "BOTREAPER_HOME override")
	useTUI := fs.Bool("tui", false, "launch interactive TUI")
	serve := fs.String("serve", "", "serve WS gateway on addr (e.g. 127.0.0.1:18789)")
	listModels := fs.Bool("list-models", false, "list provider names")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	name := *prof
	if *profileLong != "" {
		name = *profileLong
	}
	home := profile.GetBotReaperHome(*homeOver)
	if name != "" {
		home = profile.NamedHome(name)
	}
	if err := profile.Ensure(home); err != nil {
		return err
	}
	cfg := config.Load(home)
	config.SetTimezoneCacheName(cfg.Timezone)
	if *model != "" {
		cfg.Model = *model
	}
	if *provName != "" {
		cfg.Provider = *provName
	}
	if *baseURL != "" {
		cfg.BaseURL = *baseURL
	}
	if *listModels {
		for _, n := range provider.NewRegistry().Names() {
			fmt.Println(n)
		}
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	db, err := state.Open(home)
	if err != nil {
		return err
	}
	defer db.Close()
	_ = db.RecordHeartbeat(ctx, "cli")

	reg := provider.NewRegistry()
	profRec, ok := reg.Resolve(cfg.Provider)
	if !ok {
		profRec, _ = reg.Resolve("nous")
	}
	apiKey := config.LoadSecret(home, envFor(profRec.Name))
	var pclient *provider.Client
	if apiKey != "" || profRec.AuthType == "none" {
		// Keyless profiles (opencode-free, ollama) still get a live client:
		// the transport sends no Authorization header when the key is empty.
		pclient = provider.NewClient(profRec, apiKey, cfg.BaseURL)
	}

	treg := tools.NewRegistry()
	cwd, _ := os.Getwd()
	tools.RegisterFileTools(treg, cwd)
	tools.RegisterTodoTools(treg, tools.NewTodoStore())
	tools.RegisterWebTools(treg, cfg.SearchBase, config.LoadSecret(home, "SEARCH_API_KEY"), nil)
	tools.RegisterClarify(treg)
	tools.RegisterProject(treg, "botreaper")
	sbx := sandbox.NewRegistry(nil)
	terminalBackend := selectTerminalBackend(cfg, sbx)
	tools.RegisterTerminal(treg, func(tctx context.Context, cmd string, out chan<- tools.StreamChunk) (int, error) {
		if err := sandbox.CheckExec(cmd); err != nil {
			return 127, err
		}
		sch := make(chan sandbox.Chunk, 64)
		done := make(chan struct{})
		var code int
		var rerr error
		go func() {
			defer close(done)
			code, rerr = sbx.Get(terminalBackend).Exec(tctx, cmd, sch)
			close(sch)
		}()
		for c := range sch {
			select {
			case out <- tools.StreamChunk{Stream: c.Stream, Data: c.Data}:
			case <-tctx.Done():
			}
		}
		<-done
		return code, rerr
	}, func(cmd string) error { return sandbox.CheckExec(cmd) })
	mem := memory.NewStore(home)
	tools.RegisterMemory(treg, mem.Load, mem.Save)
	tools.RegisterSessionSearch(treg, func(tctx context.Context, q string) (string, error) {
		hits, err := db.SearchMessages(tctx, q, "", 20)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		for _, h := range hits {
			sb.WriteString("- [" + h.SessionID + "] " + h.Snippet + "\n")
		}
		return sb.String(), nil
	})
	skList := skills.Discover(home + "/.skills")
	tools.RegisterSkills(treg, func(tctx context.Context) string { return skills.Describe(skList) })
	mgr := subagent.NewManager(nil)
	_ = mgr
	tools.RegisterDelegate(treg, func(tctx context.Context, task string) (string, error) {
		return "delegated (depth " + itoa(subagent.DepthOf(tctx)) + "): " + task, nil
	})
	cronStore := cron.NewStore()
	tools.RegisterCronTools(treg,
		func(tctx context.Context, schedule, prompt string) string {
			j, err := cronStore.Create(schedule, prompt)
			if err != nil {
				return "error: " + err.Error()
			}
			return "created " + j.ID + " next " + j.NextFire.Format(time.RFC3339)
		},
		func(tctx context.Context) string {
			var sb strings.Builder
			for _, j := range cronStore.List() {
				sb.WriteString(j.ID + " " + j.Schedule + " " + j.NextFire.Format(time.RFC3339) + "\n")
			}
			return sb.String()
		},
		func(tctx context.Context, id string) string {
			if cronStore.Delete(id) {
				return "deleted " + id
			}
			return "not found: " + id
		})

	hub := ws.NewHub(1)
	srv := ws.NewServer(hub, "")
	srv.On("prompt.submit", func(rctx context.Context, h *ws.Hub, sess string, params []byte) (any, error) {
		return map[string]string{"status": "accepted"}, nil
	})
	srv.On("session.events.since", func(rctx context.Context, h *ws.Hub, sess string, params []byte) (any, error) {
		return map[string]any{"epoch": h.Epoch()}, nil
	})
	gw := gateway.NewRunner(hub)
	_ = gw
	sched := cron.NewScheduler(cronStore, 60*time.Second, func(rctx context.Context, j *cron.Job) error {
		hub.Publish("cron", types.EvCronFired, types.KindEvent, []byte(j.ID+" "+j.Prompt))
		return nil
	})
	go func() { _ = sched.Run(ctx) }()

	sessID := "cli"
	ag := agent.New(agent.Options{
		Provider: profRec.Name, Model: cfg.Model, BaseURL: cfg.BaseURL,
		MaxIterations: cfg.MaxIterations, SessionID: sessID, CWD: cwd,
		SystemPrompt: "You are BotReaper, a helpful AI agent.",
	}, pclient, profRec, treg, db, hub)
	defer ag.Close()
	_ = db.CreateSession(ctx, types.Session{ID: sessID, Source: "cli", SessionKey: sessID, Model: cfg.Model, Provider: profRec.Name, CWD: cwd, ProfileName: name})

	slash := gateway.NewSlashTable()

	if *serve != "" {
		fmt.Println("serving on " + *serve)
		return srv.Serve(ctx, *serve)
	}

	if *useTUI {
		return runTUI(ctx, hub, srv, ag, sessID)
	}

	rest := fs.Args()
	if len(rest) > 0 && rest[0] == "setup" {
		return runSetupCommand(home, rest[1:])
	}
	if len(rest) > 0 && rest[0] == "gateway" {
		return runGatewayCommand(ctx, home, rest[1:], gatewayParams{
			hub: hub, db: db, slash: slash, home: home, cwd: cwd, profile: name,
			provider: profRec, model: cfg.Model, baseURL: cfg.BaseURL,
			maxIterations: cfg.MaxIterations, tools: treg,
		})
	}
	if len(rest) > 0 {
		out, err := ag.Chat(ctx, strings.Join(rest, " "))
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	}
	// Plain REPL fallback (non-TUI): slash table + agent turns.
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	brand.Print(os.Stdout)
	fmt.Println("botreaper (" + profile.DisplayHome(home) + ") — /model /reset /skills /status, Ctrl+C cancels, Ctrl+D exits")
	for {
		fmt.Print("> ")
		if !sc.Scan() {
			fmt.Println()
			return sc.Err()
		}
		text := sc.Text()
		if strings.TrimSpace(text) == "" {
			continue
		}
		if out, handled, _ := slash.Execute(ctx, text, gateway.Inbound{Platform: "cli"}); handled {
			fmt.Println(out)
			continue
		}
		out, err := ag.Chat(ctx, text)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: "+err.Error())
			continue
		}
		fmt.Println(out)
	}
}

// gatewayParams carries what runMessagingGateway needs from run().
type gatewayParams struct {
	hub           *ws.Hub
	db            *state.DB
	slash         *gateway.SlashTable
	home          string
	cwd           string
	profile       string
	provider      provider.Profile
	model         string
	baseURL       string
	maxIterations int
	tools         *tools.Registry
}

// runMessagingGateway starts the Telegram/Discord adapters on the Runner and
// serves turns until ctx cancels. Tokens come from <home>/.env (secrets
// only): TELEGRAM_BOT_TOKEN, DISCORD_BOT_TOKEN.
func runMessagingGateway(ctx context.Context, p gatewayParams) error {
	tgTok := config.LoadSecret(p.home, "TELEGRAM_BOT_TOKEN")
	dcTok := config.LoadSecret(p.home, "DISCORD_BOT_TOKEN")
	if tgTok == "" && dcTok == "" {
		return errors.New("gateway: set TELEGRAM_BOT_TOKEN and/or DISCORD_BOT_TOKEN in " + p.home + "/.env")
	}
	// Client construction mirrors run(): keyless profiles stay live.
	var pclient *provider.Client
	if key := config.LoadSecret(p.home, envFor(p.provider.Name)); key != "" || p.provider.AuthType == "none" {
		pclient = provider.NewClient(p.provider, key, p.baseURL)
	}
	busy := gateway.NewBusyGuard()
	var mu sync.Mutex
	agents := map[string]*agent.AIAgent{}
	getAgent := func(platform, key string) *agent.AIAgent {
		id := platform + ":" + key
		mu.Lock()
		defer mu.Unlock()
		if a, ok := agents[id]; ok {
			return a
		}
		a := agent.New(agent.Options{
			Provider: p.provider.Name, Model: p.model, BaseURL: p.baseURL,
			MaxIterations: p.maxIterations, SessionID: id, CWD: p.cwd,
			SystemPrompt: "You are BotReaper, a helpful AI agent.",
		}, pclient, p.provider, p.tools, p.db, p.hub)
		_ = p.db.CreateSession(ctx, types.Session{ID: id, Source: "gateway", SessionKey: id, Model: p.model, Provider: p.provider.Name, CWD: p.cwd, ProfileName: p.profile})
		agents[id] = a
		return a
	}
	handle := func(sender gateway.Adapter, mctx context.Context, in gateway.Inbound) {
		key := in.ChatID
		if in.ThreadID != "" {
			key += ":" + in.ThreadID
		}
		session := in.Platform + ":" + key
		if !busy.TryAcquire(session, in.Text, p.slash.Handles) {
			_ = sender.SendReply(mctx, in, "busy with another turn — try again shortly")
			return
		}
		defer busy.Release(session)
		if out, handled, _ := p.slash.Execute(mctx, in.Text, in); handled {
			_ = sender.SendReply(mctx, in, out)
			return
		}
		sender.Typing(mctx, in)
		out, err := getAgent(in.Platform, key).Chat(mctx, in.Text)
		if err != nil {
			_ = sender.SendReply(mctx, in, "error: "+err.Error())
			return
		}
		_ = sender.SendReply(mctx, in, out)
	}
	gw := gateway.NewRunner(p.hub)
	if tgTok != "" {
		var tg *telegram.Adapter
		tg = telegram.New(telegram.Config{Token: tgTok}, func(mctx context.Context, in gateway.Inbound) {
			handle(tg, mctx, in)
		})
		tg.Allowed = gateway.ParseAllowlist(config.LoadSecret(p.home, "TELEGRAM_ALLOWED_USERS"))
		gw.Use(tg)
		fmt.Println("gateway: telegram enabled")
	}
	if dcTok != "" {
		var dc *discord.Adapter
		dc = discord.New(discord.Config{Token: dcTok}, func(mctx context.Context, in gateway.Inbound) {
			handle(dc, mctx, in)
		})
		dc.SetAllowed(gateway.ParseAllowlist(config.LoadSecret(p.home, "DISCORD_ALLOWED_USERS")))
		gw.Use(dc)
		fmt.Println("gateway: discord enabled")
	}
	fmt.Println("gateway running — Ctrl+C to stop")
	return gw.Start(ctx)
}

// runSetupCommand routes `botreaper setup [section] [--flags]`.
func runSetupCommand(home string, argv []string) error {
	var args setup.Args
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--non-interactive":
			args.NonInteractive = true
		case "--reset":
			args.Reset = true
		case "--quick":
			args.Quick = true
		case "--help", "-h":
			fmt.Println("usage: botreaper setup [model|terminal|gateway|tools|agent] [--non-interactive] [--reset] [--quick]")
			return nil
		default:
			if strings.HasPrefix(argv[i], "-") {
				return errors.New("setup: unknown flag " + argv[i])
			}
			if args.Section != "" {
				return errors.New("setup: only one section at a time")
			}
			args.Section = argv[i]
		}
	}
	return setup.Run(home, args)
}

// runGatewayCommand routes `botreaper gateway --run`. Bare `botreaper
// gateway` prints which channels are configured (token presence only,
// never secret values) plus the start hint.
func runGatewayCommand(ctx context.Context, home string, argv []string, p gatewayParams) error {
	gfs := flag.NewFlagSet("gateway", flag.ContinueOnError)
	start := gfs.Bool("run", false, "start the messaging gateway")
	if err := gfs.Parse(argv); err != nil {
		return err
	}
	if gfs.NArg() > 0 {
		fmt.Println("usage: botreaper gateway [--run]")
		return errors.New("gateway: unexpected argument " + gfs.Arg(0))
	}
	if !*start {
		status := func(label, key string) {
			state := "not configured"
			if config.LoadSecret(home, key) != "" {
				state = "configured"
			}
			fmt.Printf("%-10s %s\n", label+":", state)
		}
		status("telegram", "TELEGRAM_BOT_TOKEN")
		status("discord", "DISCORD_BOT_TOKEN")
		fmt.Println("start with: botreaper gateway --run")
		fmt.Println("configure with: botreaper setup gateway")
		return nil
	}
	return runMessagingGateway(ctx, p)
}

// selectTerminalBackend honors `terminal_backend` from setup, registering
// configured docker/ssh/singularity backends. Cloud runners (modal/daytona)
// fall back to local with a warning.
func selectTerminalBackend(cfg types.Config, sbx *sandbox.Registry) string {
	backend := cfg.TerminalBackend
	if backend == "" {
		return "local"
	}
	switch backend {
	case "docker":
		container := cfg.DockerContainer
		if container == "" {
			container = "botreaper"
		}
		sbx.Register(sandbox.NewDocker(container))
		return "docker"
	case "ssh":
		if cfg.SSHHost == "" {
			fmt.Fprintln(os.Stderr, "warning: ssh backend has no host; using local")
			return "local"
		}
		port := cfg.SSHPort
		if port == "" {
			port = "22"
		}
		user := cfg.SSHUser
		if user == "" {
			user = os.Getenv("USER")
		}
		sbx.Register(sandbox.NewSSH(user, cfg.SSHHost+":"+port, expandKeyPath(cfg.SSHKey)))
		return "ssh"
	case "singularity":
		img := cfg.SingularityImage
		if img == "" {
			img = "botreaper.sif"
		}
		sbx.Register(sandbox.NewSingularity(img))
		return "singularity"
	case "local":
		return "local"
	default:
		fmt.Fprintln(os.Stderr, "warning: terminal backend "+backend+" is not wired in this runtime; using local")
		return "local"
	}
}

func expandKeyPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}

func runTUI(ctx context.Context, hub *ws.Hub, srv *ws.Server, ag *agent.AIAgent, sess string) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	httpSrv := &http.Server{Handler: srv.Handler()}
	go func() { _ = httpSrv.Serve(ln) }()
	defer httpSrv.Close()
	url := "ws://" + ln.Addr().String() + "/api/ws"
	cl, err := ws.Dial(ctx, url, "", sess)
	if err != nil {
		return err
	}
	defer cl.Close()
	tctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := tui.New(tui.Config{
		SessionID: sess,
		Slash:     []string{"/model", "/reset", "/skills", "/status", "/sessions"},
		Send: func(sctx context.Context, text string) error {
			go func() {
				out, err := ag.Chat(tctx, text)
				if err != nil {
					hub.Publish(sess, types.EvMessageAppended, types.KindEvent, []byte("error: "+err.Error()))
					return
				}
				hub.Publish(sess, types.EvMessageAppended, types.KindEvent, []byte(out))
			}()
			return cl.Notify(sctx, "prompt.submit", sess, map[string]string{"text": text})
		},
		Cancel: func() {
			ag.RequestInterrupt()
			go func() {
				time.Sleep(500 * time.Millisecond)
				ag.ClearInterrupt()
			}()
		},
	})
	prog := tea.NewProgram(m)
	go func() {
		for {
			select {
			case <-tctx.Done():
				return
			case env, ok := <-cl.Recv():
				if !ok {
					return
				}
				prog.Send(tui.InboundMsg(env))
			}
		}
	}()
	_, err = prog.Run()
	return err
}

func envFor(providerName string) string {
	switch providerName {
	case "openai":
		return "OPENAI_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "opencode-zen":
		return "OPENCODE_ZEN_API_KEY"
	case "opencode-go":
		return "OPENCODE_GO_API_KEY"
	case "opencode-free":
		return "" // keyless — the free tier 401s any bearer it doesn't recognize
	default:
		return "NOUS_API_KEY"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for n > 0 {
		pos--
		b[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(b[pos:])
}
