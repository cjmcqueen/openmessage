#!/bin/bash

# @raycast.schemaVersion 1
# @raycast.title Stop OpenMessage
# @raycast.mode compact
# @raycast.packageName OpenMessage
# @raycast.icon 💬
# @raycast.description Stops the OpenMessage background service until you start it or log in again

SERVICE=com.cjmcqueen.openmessage
DOMAIN="gui/$(id -u)"

if ! launchctl print "$DOMAIN/$SERVICE" >/dev/null 2>&1; then
  echo "OpenMessage is already stopped"
  exit 0
fi

if launchctl bootout "$DOMAIN/$SERVICE"; then
  echo "OpenMessage stopped"
else
  echo "OpenMessage didn't stop"
  exit 1
fi
