#!/bin/bash

set -e

echo "Stopping Orion services..."

sudo systemctl stop orion-core 2>/dev/null || true
sudo systemctl stop orion-bus 2>/dev/null || true
sudo systemctl stop orion-vision 2>/dev/null || true
sudo systemctl stop mediamtx 2>/dev/null || true

echo "Disabling services..."

sudo systemctl disable orion-core 2>/dev/null || true
sudo systemctl disable orion-bus 2>/dev/null || true
sudo systemctl disable orion-vision 2>/dev/null || true
sudo systemctl disable mediamtx 2>/dev/null || true

echo "Removing systemd services..."

sudo rm -f /etc/systemd/system/orion-core.service
sudo rm -f /etc/systemd/system/orion-bus.service
sudo rm -f /etc/systemd/system/orion-vision.service
sudo rm -f /etc/systemd/system/mediamtx.service

sudo systemctl daemon-reload

echo "Removing Orion directories..."

sudo rm -rf /opt/orion
sudo rm -rf /etc/orion
sudo rm -rf /var/lib/orion
sudo rm -rf /var/opt/orion

echo "Removing binaries..."

sudo rm -f /usr/local/bin/orion-core
sudo rm -f /usr/local/bin/orion-bus

echo "Removing Orion user..."

sudo userdel orion 2>/dev/null || true

echo "Reset complete."
