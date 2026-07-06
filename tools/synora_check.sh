#!/usr/bin/env bash

set -e

echo "==============================="
echo "SYNORA SYSTEM CHECK"
echo "==============================="
echo

check_service () {
    service=$1

    if systemctl is-active --quiet "$service"; then
        echo "✅ $service running"
    else
        echo "❌ $service NOT running"
        systemctl status "$service" --no-pager
        exit 1
    fi
}

echo "Checking systemd services..."
check_service synora-bus
check_service synora-core
check_service synora-discovery
check_service synora-vision
check_service mediamtx
check_service synora-api

echo
echo "Checking sockets..."

if [ -S /run/synora/bus.sock ]; then
    echo "✅ UNIX bus socket exists"
else
    echo "❌ UNIX bus socket missing"
    exit 1
fi

echo
echo "Checking ports..."

check_port () {
    port=$1

    if ss -lnt | grep -q ":$port"; then
        echo "✅ port $port open"
    else
        echo "❌ port $port NOT open"
        exit 1
    fi
}

check_port 8554
check_port 1935
check_port 8888

echo
echo "Checking python vision dependencies..."

sudo -u synora /var/lib/synora/services/vision-worker/venv/bin/python - <<EOF
import cv2
import onnxruntime
print("✅ python dependencies OK")
EOF

echo
echo "Checking mediamtx process..."

pgrep mediamtx >/dev/null && echo "✅ mediamtx process running" || (echo "❌ mediamtx missing" && exit 1)

echo
echo "==============================="
echo "SYNORA SYSTEM OK"
echo "==============================="
