#!/bin/bash
set -e

echo "Installing system dependencies..."

sudo dnf install -y \
    python3-devel \
    gcc \
    gcc-c++ \
    make \
    cmake \
    git

echo "Bootstrap complete."
