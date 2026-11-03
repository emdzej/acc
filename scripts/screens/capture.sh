#!/usr/bin/env bash
# capture.sh regenerates the screenshots in site/public/screens from the
# made-up sessions of scripts/demo.sh: the TUI in a real terminal (Ghostty,
# captured with screencapture), the web dashboard in headless Chromium.
# macOS only. Needs Ghostty, tmux, swiftc, node and playwright
# (PW=<dir with node_modules/playwright>, e.g. an npx cache entry).
#
#   scripts/screens/capture.sh
#
# A Ghostty window opens and comes to the front for each TUI shot: macOS
# doesn't repaint windows that are covered. The TUI runs on a private tmux
# socket (-L accshot), so your own tmux server is untouched.
set -euo pipefail
cd "$(dirname "$0")/../.."
OUT=site/public/screens
WORK=$(mktemp -d /tmp/acc-screens.XXXX)
TITLE="acc tui (demo)"
SOCK=accshot

go build -o "$WORK/acc" ./cmd/acc
swiftc -O scripts/screens/winid.swift -o "$WORK/winid"
swiftc -O scripts/screens/activate.swift -o "$WORK/activate"

bash scripts/demo.sh "$WORK/acc" >"$WORK/demo.out" 2>&1 &
DEMO=$!
GHOSTTY=""
cleanup() {
  [ -n "$GHOSTTY" ] && kill "$GHOSTTY" 2>/dev/null
  tmux -L $SOCK kill-server 2>/dev/null || true
  kill $DEMO 2>/dev/null; wait $DEMO 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT
until grep -q "tui:" "$WORK/demo.out" 2>/dev/null; do sleep 0.3; done
ACC_HOME=$(sed -n 's/.*(ACC_HOME=\(.*\))$/\1/p' "$WORK/demo.out")
ACC_SERVER=http://127.0.0.1:${ACC_DEMO_PORT:-7399}
DEMO_HOME=$(dirname "$ACC_HOME")/home # History paths show as ~/…

tmux -L $SOCK -f /dev/null new-session -d -s shot -x 168 -y 36 \
  "env HOME=$DEMO_HOME ACC_HOME=$ACC_HOME ACC_SERVER=$ACC_SERVER $WORK/acc tui --serve never"
tmux -L $SOCK set -g status off
open -na Ghostty --args --title="$TITLE" --theme="Atom One Dark" --font-size=13 \
  --window-width=168 --window-height=36 --window-padding-x=14 --window-padding-y=12 \
  --confirm-close-surface=false --window-save-state=never \
  --command="$(command -v tmux) -L $SOCK attach -t shot"
for _ in $(seq 50); do GHOSTTY=$(pgrep -f "title=acc tui \\(demo\\)" | head -1 || true); [ -n "$GHOSTTY" ] && break; sleep 0.2; done
sleep 3

keys() { for k in "$@"; do tmux -L $SOCK send-keys -t shot "$k"; sleep 0.35; done; }
shot() {
  "$WORK/activate" "$GHOSTTY" >/dev/null
  sleep 1.2
  screencapture -l"$("$WORK/winid" "$TITLE")" -o -x "$OUT/$1.png"
  echo "$OUT/$1.png"
}

keys / api-server Enter; sleep 2; shot tui
keys Escape H; sleep 0.8; keys Down Down; shot tui-history
keys Escape

node scripts/screens/web.mjs "$ACC_SERVER" "$(cat "$ACC_HOME/token")" "$OUT"
