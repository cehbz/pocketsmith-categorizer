#!/bin/sh
# Builds categorize into ~/.local/bin, installs its leak check as this clone's commit-msg hook
# and loads the daily agent that runs it with notices on.
set -eu
cd "$(dirname "$0")/.."
label=com.cehbz.pocketsmith-categorizer
go build -o "$HOME/.local/bin/categorize" ./cmd/categorize
ln -sf ../../scripts/commit-msg "$(git rev-parse --git-common-dir)/hooks/commit-msg"
mkdir -p "$HOME/.local/state/pocketsmith-categorizer"
plist="$HOME/Library/LaunchAgents/$label.plist"
sed "s#__HOME__#$HOME#g" "launchd/$label.plist" > "$plist"
launchctl bootout "gui/$(id -u)/$label" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$plist"
echo "loaded $label; run it now with: launchctl kickstart gui/$(id -u)/$label"
