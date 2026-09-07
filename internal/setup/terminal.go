package setup

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// terminalChoices mirrors the Python backend list (Vercel Sandbox has no Go
// backend yet and is omitted; plugin backends likewise).
func terminalChoices(current string) ([]string, []string, int) {
	rows := []string{
		"Local - run directly on this machine (default)",
		"Docker - isolated container with configurable resources",
		"Modal - serverless cloud sandbox",
		"SSH - run on a remote machine",
		"Daytona - persistent cloud development environment",
	}
	keys := []string{"local", "docker", "modal", "ssh", "daytona"}
	if runtime.GOOS == "linux" {
		rows = append(rows, "Singularity/Apptainer - HPC-friendly container")
		keys = append(keys, "singularity")
	}
	def := 0
	keep := "Keep current"
	if current != "" {
		keep += " (" + current + ")"
		for i, k := range keys {
			if k == current {
				def = i
			}
		}
	}
	rows = append(rows, keep)
	keys = append(keys, "")
	return rows, keys, def
}

// SetupTerminalBackend is the Terminal Backend section (verbatim chrome).
func SetupTerminalBackend(s *State) {
	Header("Terminal Backend", false)
	emit("Choose where BotReaper runs shell commands and code.")
	emit("This affects tool execution, file access, and isolation.")
	emit("   Guide: https://hermes-agent.nousresearch.com/docs/user-guide/configuration#terminal-backend-configuration")
	emit("")

	current := s.Get("terminal_backend", "")
	if current == "" {
		current = "local"
	}
	rows, keys, def := terminalChoices(current)
	idx := PromptChoice("Select terminal backend:", rows, def)
	if keys[idx] == "" {
		emit("Keeping current backend: %s", current)
		return
	}
	backend := keys[idx]
	switch backend {
	case "local":
		emit("Terminal backend: Local")
		emit("Commands run directly on this machine.")
	case "docker":
		setupDocker(s)
	case "modal":
		setupModal(s)
	case "ssh":
		setupSSH(s)
	case "daytona":
		setupDaytona(s)
	case "singularity":
		img := Prompt("Singularity image [botreaper.sif]", "")
		if img == "" {
			img = "botreaper.sif"
		}
		s.Set("terminal_singularity_image", img)
		emit("Terminal backend: Singularity/Apptainer")
	}
	s.Set("terminal_backend", backend)
	if err := s.Save(); err != nil {
		Error("Could not save config: %v", err)
		return
	}
	emit("Terminal backend set to: %s", backend)
}

func setupDocker(s *State) {
	emit("Terminal backend: Docker")
	if path, err := exec.LookPath("docker"); err == nil {
		emit("Docker found: %s", path)
	} else {
		emit("Docker not found in PATH!")
		emit("Install Docker: https://docs.docker.com/get-docker/")
	}
	cur := s.Get("terminal_docker_container", "")
	v := Prompt("Docker container ["+orDefault(cur, "botreaper")+"]", "")
	if v == "" {
		v = orDefault(cur, "botreaper")
	}
	s.Set("terminal_docker_container", v)
}

func setupModal(s *State) {
	emit("Terminal backend: Modal")
	emit("Serverless cloud sandboxes. Requires a Modal account: https://modal.com")
	tid := PromptSecret("    Modal Token ID")
	if tid == "" {
		emit("No token provided. Cancelled.")
		return
	}
	tsec := PromptSecret("    Modal Token Secret")
	if err := s.SetSecret("MODAL_TOKEN_ID", tid); err != nil {
		Error("Could not save: %v", err)
		return
	}
	if tsec != "" {
		_ = s.SetSecret("MODAL_TOKEN_SECRET", tsec)
	}
	Info("Cloud execution for Modal is not wired in this runtime yet — credentials saved for when it is.")
}

func setupSSH(s *State) {
	emit("Terminal backend: SSH")
	emit("Run commands on a remote machine via SSH.")
	host := Prompt("  SSH host (hostname or IP)", s.Get("terminal_ssh_host", ""))
	user := Prompt("  SSH user ["+orDefault(s.Get("terminal_ssh_user", ""), os.Getenv("USER"))+"]", "")
	if user == "" {
		user = orDefault(s.Get("terminal_ssh_user", ""), os.Getenv("USER"))
	}
	port := Prompt("  SSH port [22]", "")
	if port == "" {
		port = "22"
	}
	key := Prompt("  SSH private key path [~/.ssh/id_rsa]", "")
	if key == "" {
		key = s.Get("terminal_ssh_key", "")
		if key == "" {
			key = "~/.ssh/id_rsa"
		}
	}
	s.Set("terminal_ssh_host", host)
	s.Set("terminal_ssh_user", user)
	s.Set("terminal_ssh_port", port)
	s.Set("terminal_ssh_key", key)
	if host != "" && PromptYesNo("  Test SSH connection?", true) {
		emit("  Testing connection...")
		keyPath := key
		if strings.HasPrefix(keyPath, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				keyPath = home + keyPath[1:]
			}
		}
		cmd := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=8",
			"-i", keyPath, "-p", port, user+"@"+host, "true")
		if out, err := cmd.CombinedOutput(); err != nil {
			emit("  SSH connection failed: %s", strings.TrimSpace(string(out)))
			emit("  Check your SSH key and that the host accepts it.")
		} else {
			emit("  SSH connection successful!")
		}
	}
}

func setupDaytona(s *State) {
	emit("Terminal backend: Daytona")
	emit("Persistent cloud development environments. Sign up at https://daytona.io")
	k := PromptSecret("    Daytona API key")
	if k == "" {
		emit("No API key provided. Cancelled.")
		return
	}
	if err := s.SetSecret("DAYTONA_API_KEY", k); err != nil {
		Error("Could not save: %v", err)
		return
	}
	Info("Cloud execution for Daytona is not wired in this runtime yet — credentials saved for when it is.")
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}
