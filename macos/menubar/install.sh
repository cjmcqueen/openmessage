#!/bin/bash
# Installs OpenMessage as a background service and builds the menu bar app.
# Needs only the Command Line Tools, not Xcode. Run it again after rebuilding ./openmessage.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
SERVICE=com.cjmcqueen.openmessage
MENUBAR=com.cjmcqueen.openmessage-menubar
AGENTS="$HOME/Library/LaunchAgents"
LOGS="$HOME/Library/Logs/OpenMessage"
BIN="$HOME/.local/bin/openmessage"
DATA="$HOME/.local/share/openmessage"
APP="$HOME/Applications/OpenMessage Menu Bar.app"
DOMAIN="gui/$(id -u)"

if lsof -nP -iTCP:7007 -sTCP:LISTEN >/dev/null 2>&1 \
  && ! launchctl print "$DOMAIN/$SERVICE" >/dev/null 2>&1; then
  echo "Port 7007 is in use by a copy not run by the service. Stop it, then try again." >&2
  exit 1
fi

mkdir -p "$AGENTS" "$LOGS" "$(dirname "$BIN")" "$APP/Contents/MacOS"

launchctl bootout "$DOMAIN/$MENUBAR" 2>/dev/null || true
launchctl bootout "$DOMAIN/$SERVICE" 2>/dev/null || true

cp "$REPO/openmessage" "$BIN.new"
mv "$BIN.new" "$BIN"

cp "$HERE/Info.plist" "$APP/Contents/Info.plist"
swiftc -O -swift-version 5 -framework AppKit "$HERE/main.swift" -o "$APP/Contents/MacOS/OpenMessageMenuBar"
codesign --force --sign - "$APP"

cat > "$AGENTS/$SERVICE.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$SERVICE</string>
  <key>ProgramArguments</key>
  <array>
    <string>$BIN</string>
    <string>serve</string>
    <string>--web</string>
    <string>--mcp-sse</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>OPENMESSAGES_DATA_DIR</key><string>$DATA</string>
    <key>PATH</key><string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
  </dict>
  <key>WorkingDirectory</key><string>$DATA</string>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardOutPath</key><string>$LOGS/service.log</string>
  <key>StandardErrorPath</key><string>$LOGS/service.log</string>
</dict>
</plist>
EOF

cat > "$AGENTS/$MENUBAR.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$MENUBAR</string>
  <key>ProgramArguments</key>
  <array>
    <string>$APP/Contents/MacOS/OpenMessageMenuBar</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>LimitLoadToSessionType</key><string>Aqua</string>
</dict>
</plist>
EOF

launchctl bootstrap "$DOMAIN" "$AGENTS/$SERVICE.plist"
launchctl bootstrap "$DOMAIN" "$AGENTS/$MENUBAR.plist"
echo "Installed. OpenMessage is starting at http://127.0.0.1:7007"
