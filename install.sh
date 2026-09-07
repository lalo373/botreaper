#!/usr/bin/env bash
# BotReaper installer: builds ./cmd/botreaper, installs it to PREFIX
# (default ~/.local/bin), wires PREFIX into your shell PATH via ~/.bashrc,
# and optionally launches the interactive `botreaper setup` wizard.
#
#   ./install.sh [--prefix DIR] [--yes] [--skip-setup] [--no-bashrc] [--help]
set -euo pipefail

PREFIX="${HOME}/.local/bin"
ASSUME_YES=0
SKIP_SETUP=0
NO_BASHRC=0
BASHRC="${BASHRC:-${HOME}/.bashrc}"
MARKER_BEGIN="# >>> botreaper (added by install.sh) >>>"
MARKER_END="# <<< botreaper <<<"

usage() {
	cat <<'EOF'
usage: ./install.sh [--prefix DIR] [--yes] [--skip-setup] [--no-bashrc] [--help]

  --prefix DIR   install directory for the botreaper binary (default ~/.local/bin)
  --yes          assume defaults, never prompt (implies --skip-setup)
  --skip-setup   do not offer the interactive `botreaper setup` wizard at the end
  --no-bashrc    do not touch ~/.bashrc
  --help         this text
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--prefix)
		PREFIX="${2:?--prefix needs a directory}"; shift 2 ;;
	--prefix=*)
		PREFIX="${1#--prefix=}"; shift ;;
	--yes) ASSUME_YES=1; SKIP_SETUP=1; shift ;;
	--skip-setup) SKIP_SETUP=1; shift ;;
	--no-bashrc) NO_BASHRC=1; shift ;;
	--help|-h) usage; exit 0 ;;
	*) echo "install.sh: unknown flag $1 (see --help)" >&2; exit 1 ;;
	esac
done

# --- pretty output -------------------------------------------------------
if [ -t 1 ] && [ "${TERM:-dumb}" != "dumb" ] && [ "${NO_COLOR:-}" = "" ]; then
	C_BOLD=$'\033[1m'; C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'
	C_CYAN=$'\033[36m'; C_RED=$'\033[31m'; C_DIM=$'\033[2m'; C_RESET=$'\033[0m'
else
	C_BOLD=""; C_GREEN=""; C_YELLOW=""; C_CYAN=""; C_RED=""; C_DIM=""; C_RESET=""
fi

step() { printf '%s==>%s %s\n' "$C_CYAN" "$C_RESET" "$*"; }
ok() { printf '%s✓%s %s\n' "$C_GREEN" "$C_RESET" "$*"; }
warn() { printf '%s!%s %s\n' "$C_YELLOW" "$C_RESET" "$*" >&2; }
die() { printf '%s✗ %s%s\n' "$C_RED" "$*" "$C_RESET" >&2; exit 1; }

banner() {
	cat <<'EOF'
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
EOF
}

ver_ge() { # ver_ge HAVE NEED — true if HAVE >= NEED (major.minor)
	local have_major="${1%%.*}" need_major="${2%%.*}"
	local have_minor="${1#*.}" need_minor="${2#*.}"
	have_minor="${have_minor%%.*}"; need_minor="${need_minor%%.*}"
	[ "$have_major" -gt "$need_major" ] && return 0
	[ "$have_major" -lt "$need_major" ] && return 1
	[ "$have_minor" -ge "$need_minor" ]
}

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

banner
echo "${C_BOLD}BotReaper installer${C_RESET}${C_DIM} — ${REPO}${C_RESET}"
echo

# --- 1. Go toolchain ------------------------------------------------------
step "Checking Go toolchain..."
command -v go >/dev/null 2>&1 || die "go not found — install Go first: https://go.dev/dl/"
GOTRIM="$(go version | awk '{print $3}')"
GOVER="${GOTRIM#go}"
NEEDVER="$(awk '/^go [0-9]/ {print $2; exit}' "$REPO/go.mod")"
NEEDVER="${NEEDVER:-1.26.0}"
ver_ge "$GOVER" "$NEEDVER" || die "go $GOVER too old — need >= $NEEDVER"
ok "go $GOVER (>= $NEEDVER)"

# --- 2. Build + install ----------------------------------------------------
step "Building botreaper..."
mkdir -p "$PREFIX"
(cd "$REPO" && go build -trimpath -o "$PREFIX/botreaper" ./cmd/botreaper)
ok "installed ${PREFIX}/botreaper"

# --- 3. Shell PATH wiring ---------------------------------------------------
if [ "$NO_BASHRC" -eq 0 ]; then
	step "Wiring PATH in ${BASHRC}..."
	touch "$BASHRC"
	if grep -qF "$MARKER_BEGIN" "$BASHRC"; then
		ok "PATH block already present in ${BASHRC}"
	else
		if [ "$PREFIX" = "${HOME}/.local/bin" ]; then
			EXPORT_LINE='export PATH="$HOME/.local/bin:$PATH"'
		else
			EXPORT_LINE="export PATH=\"$PREFIX:\$PATH\""
		fi
		{
			echo "$MARKER_BEGIN"
			echo "$EXPORT_LINE"
			echo "$MARKER_END"
		} >>"$BASHRC"
		ok "appended PATH block to ${BASHRC}"
	fi
	case ":$PATH:" in
	*":$PREFIX:"*) ok "$PREFIX already on PATH in this shell" ;;
	*) export PATH="$PREFIX:$PATH"
		warn "$PREFIX not on PATH yet in this shell — restart it or run: source $BASHRC" ;;
	esac
else
	export PATH="$PREFIX:$PATH"
	warn "--no-bashrc: shell left untouched (added $PREFIX to PATH for this run only)"
fi

# --- 4. Smoke test ----------------------------------------------------------
step "Smoke-testing the binary..."
"$PREFIX/botreaper" --list-models >/dev/null || die "binary failed to run"
ok "$(command -v botreaper) --list-models works"

# --- 5. Interactive setup ---------------------------------------------------
if [ "$SKIP_SETUP" -eq 0 ] && [ "$ASSUME_YES" -eq 0 ] && [ -t 0 ]; then
	echo
	printf '%sRun interactive setup now? [Y/n]:%s ' "$C_YELLOW" "$C_RESET"
	IFS= read -r answer || answer=""
	case "$answer" in
	[Nn]*) warn "skipped — run it later with: botreaper setup" ;;
	*) "$PREFIX/botreaper" setup ;;
	esac
elif [ "$SKIP_SETUP" -eq 1 ]; then
	warn "skipped setup wizard (--skip-setup) — run it later with: botreaper setup"
fi

echo
ok "${C_BOLD}Done.${C_RESET} Next steps:"
echo "  ${C_GREEN}botreaper setup${C_RESET}              configure provider, keys and messaging"
echo "  ${C_GREEN}botreaper gateway --run${C_RESET}      start the Telegram/Discord gateway"
echo "  ${C_GREEN}botreaper --tui${C_RESET}              full-screen chat"
