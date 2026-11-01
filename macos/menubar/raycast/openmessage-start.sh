#!/bin/bash

# @raycast.schemaVersion 1
# @raycast.title Start OpenMessage
# @raycast.mode compact
# @raycast.packageName OpenMessage
# @raycast.icon 💬
# @raycast.description Starts the OpenMessage background service

SERVICE=com.cjmcqueen.openmessage
DOMAIN="gui/$(id -u)"

if launchctl print "$DOMAIN/$SERVICE" >/dev/null 2>&1; then
  echo "OpenMessage is already running"
  exit 0
fi

if ! launchctl bootstrap "$DOMAIN" "$HOME/Library/LaunchAgents/$SERVICE.plist"; then
  echo "OpenMessage didn't start"
  exit 1
fi

for _ in {1..20}; do
  if curl -s -m 1 -o /dev/null http://127.0.0.1:7007/api/status; then
    echo "OpenMessage started"
    exit 0
  fi
  sleep 1
done

echo "OpenMessage is still starting"
