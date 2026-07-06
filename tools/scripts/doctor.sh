#!/bin/bash

echo "===== ORION SYSTEM CHECK ====="

check_service () {
    if systemctl is-active --quiet $1; then
        echo "✔ $1 running"
    else
        echo "✘ $1 NOT running"
    fi
}

check_file () {
    if [ -f "$1" ]; then
        echo "✔ file exists: $1"
    else
        echo "✘ missing file: $1"
    fi
}

echo ""
echo "Checking services..."
check_service mosquitto
check_service orion-core
check_service orion-bus
check_service orion-vision
check_service mediamtx

echo ""
echo "Checking directories..."
[ -d "/opt/orion" ] && echo "✔ /opt/orion exists" || echo "✘ /opt/orion missing"
[ -d "/opt/orion/services" ] && echo "✔ services installed" || echo "✘ services missing"

echo ""
echo "Checking models..."
check_file /opt/orion/models/yolov8n.onnx

echo ""
echo "Checking Python environment..."
if [ -f "/opt/orion/venv/bin/python" ]; then
    echo "✔ python venv exists"
else
    echo "✘ python venv missing"
fi

echo ""
echo "Checking MQTT broker..."

if nc -z localhost 1883; then
    echo "✔ MQTT port open"
else
    echo "✘ MQTT port closed"
fi

echo ""
echo "Checking MediaMTX..."

if nc -z localhost 8554; then
    echo "✔ RTSP server listening"
else
    echo "✘ RTSP server not reachable"
fi

echo ""
echo "===== CHECK COMPLETE ====="
