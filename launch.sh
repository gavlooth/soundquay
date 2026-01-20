#!/bin/bash
# Launch Fyne Audio Indicator
# Ensures only one instance is running at a time

PIDFILE="$HOME/.cache/audio-indicator.pid"

# Check if already running
if [ -f "$PIDFILE" ]; then
    OLD_PID=$(cat "$PIDFILE")
    if ps -p "$OLD_PID" > /dev/null 2>&1; then
        # Already running, bring to front
        kill -USR1 "$OLD_PID" 2>/dev/null
        exit 0
    fi
fi

# Save PID
echo $$ > "$PIDFILE"

# Clean up PID file on exit
trap "rm -f $PIDFILE" EXIT

# Launch application
exec /home/heefoo/audio-indicator/audio-indicator
