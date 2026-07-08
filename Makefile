.PHONY: \
	build \
	build-web \
	install \
	update \
	restart \
	logs \
	clean \
	dirs \
	install-config \
	install-systemd \
	install-web \
	install-bins \
	install-mediamtx \
	install-certs \
	install-python \
	doctor \
	user

# ------------------------------------------------
# CONFIG
# ------------------------------------------------

PYTHON := python3.11

SERVICE_USER := synora

PREFIX := /opt/synora

BIN_DIR := $(PREFIX)/bin
WEB_DIR := $(PREFIX)/web
MEDIA_DIR := $(PREFIX)/mediamtx
VENV_DIR := $(PREFIX)/venv

SERVICES_DIR := $(PREFIX)/services
VISION_WORKER_DIR := $(SERVICES_DIR)/vision-worker

DATA_DIR := /var/lib/synora
CONFIG_DIR := /etc/synora
LOG_DIR := /var/log/synora
RUN_DIR := /run/synora

LOG_FILE := $(LOG_DIR)/events.log

PYTHON_REQUIREMENTS := services/vision-worker/requirements.txt

SERVICES := \
	synora-bus \
	synora-core \
	synora-actions \
	synora-api \
	synora-discovery \
	mediamtx

GREEN := \033[0;32m
RED := \033[0;31m
YELLOW := \033[0;33m
NC := \033[0m

# ------------------------------------------------
# BUILD GO
# ------------------------------------------------

build:
	@echo "Building Go binaries..."

	mkdir -p bin

	go build -o bin/synora-core ./cmd/synora-core
	go build -o bin/synora-actions ./cmd/synora-actions
	go build -o bin/synora-bus ./cmd/synora-bus
	go build -o bin/synora-api ./cmd/synora-api
	go build -o bin/synora-discovery ./cmd/synora-discovery

# ------------------------------------------------
# BUILD WEBAPP
# ------------------------------------------------

build-web:
	@echo "Building webapp..."

	cd webapp && npm ci
	cd webapp && npm run build

# ------------------------------------------------
# USER
# ------------------------------------------------

user:
	@echo "Ensuring service user exists..."

	sudo id -u $(SERVICE_USER) >/dev/null 2>&1 || \
	sudo useradd \
		--system \
		--home $(DATA_DIR) \
		--shell /usr/sbin/nologin \
		$(SERVICE_USER)

# ------------------------------------------------
# DIRECTORIES
# ------------------------------------------------

dirs: user

	@echo "Creating directory structure..."

	sudo mkdir -p \
		$(PREFIX) \
		$(BIN_DIR) \
		$(WEB_DIR) \
		$(MEDIA_DIR) \
		$(VENV_DIR) \
		$(DATA_DIR) \
		$(CONFIG_DIR) \
		$(LOG_DIR) \
		$(SERVICES_DIR) \
		$(VISION_WORKER_DIR) \
		$(RUN_DIR) 

	sudo chown -R $(SERVICE_USER):$(SERVICE_USER) \
		$(PREFIX) \
		$(DATA_DIR) \
		$(LOG_DIR) \
		$(RUN_DIR)

	sudo chmod 755 $(PREFIX)
	sudo chmod 755 $(BIN_DIR)
	sudo chmod 755 $(MEDIA_DIR)
	sudo chmod 755 $(VENV_DIR)
	sudo chmod 750 $(LOG_DIR)

# ------------------------------------------------
# CONFIG
# ------------------------------------------------

install-config:

	@echo "Installing configuration..."

	sudo mkdir -p $(CONFIG_DIR)

	sudo rsync -a configs/ $(CONFIG_DIR)/

	sudo chown -R $(SERVICE_USER):$(SERVICE_USER) \
		$(CONFIG_DIR)

	sudo chmod 640 $(CONFIG_DIR)/*.yaml 2>/dev/null || true

# ------------------------------------------------
# TLS CERTIFICATES
# ------------------------------------------------

install-certs:

	@echo "Installing TLS certificates..."

	sudo mkdir -p $(CONFIG_DIR)/certs

	@if [ ! -f $(CONFIG_DIR)/certs/server.crt ] || [ ! -f $(CONFIG_DIR)/certs/server.key ]; then \
		echo "Generating self-signed TLS certificate..."; \
		sudo openssl req \
			-x509 \
			-nodes \
			-days 3650 \
			-newkey rsa:4096 \
			-keyout $(CONFIG_DIR)/certs/server.key \
			-out $(CONFIG_DIR)/certs/server.crt \
			-subj "/CN=synora.local"; \
	else \
		echo "TLS certificates already exist."; \
	fi

	sudo chown -R $(SERVICE_USER):$(SERVICE_USER) \
		$(CONFIG_DIR)/certs

	sudo chmod 600 $(CONFIG_DIR)/certs/server.key
	sudo chmod 644 $(CONFIG_DIR)/certs/server.crt
# ------------------------------------------------
# MODELS
# ------------------------------------------------

MODELS_DIR := /var/lib/synora/models

install-models:

	@echo "Installing AI models..."

	sudo mkdir -p $(MODELS_DIR)

	sudo rsync -a --delete --delete-excluded \
		--include='*/' \
		--include='*.rknn' \
		--exclude='*' \
		models/ \
		$(MODELS_DIR)/

	sudo chown -R $(SERVICE_USER):$(SERVICE_USER) \
		$(MODELS_DIR)

	sudo find $(MODELS_DIR) -type f -exec chmod 644 {} \;

	@echo "Models installed"



# ------------------------------------------------
# NETWORK DEPENDENCIES
# ------------------------------------------------

install-network-deps:

	@echo "Installing network dependencies..."

	@if command -v apt >/dev/null 2>&1; then \
		echo "Detected APT-based system"; \
		sudo apt update; \
		sudo apt install -y \
			hostapd \
			dnsmasq \
			nftables \
			iproute2 \
			wireless-tools; \
	elif command -v dnf >/dev/null 2>&1; then \
		echo "Detected DNF-based system"; \
		sudo dnf install -y \
			hostapd \
			dnsmasq \
			nftables \
			iproute \
			wireless-tools; \
	else \
		echo "Unsupported package manager"; \
		exit 1; \
	fi

# ------------------------------------------------
# PYTHON AI ENV
# ------------------------------------------------

install-python:

	@echo "Installing Python AI environment..."

	@if command -v apt >/dev/null 2>&1; then \
		sudo apt install -y \
			python3 \
			python3-venv \
			python3-pip \
			ffmpeg; \
	elif command -v dnf >/dev/null 2>&1; then \
		sudo dnf install -y \
			python3 \
			python3-pip \
			ffmpeg; \
	fi

	sudo rm -rf $(VENV_DIR)

	sudo $(PYTHON) -m venv $(VENV_DIR)

	sudo $(VENV_DIR)/bin/pip install --upgrade \
		pip \
		wheel \
		setuptools

	@if [ -f $(PYTHON_REQUIREMENTS) ]; then \
		sudo $(VENV_DIR)/bin/pip install \
			-r $(PYTHON_REQUIREMENTS); \
	else \
		echo "No requirements.txt found"; \
	fi

	sudo chown -R $(SERVICE_USER):$(SERVICE_USER) \
		$(VENV_DIR)

	@echo "Python AI environment ready"

# ------------------------------------------------
# INSTALL VISION WORKER
# ------------------------------------------------

install-vision-worker:

	@echo "Installing vision worker..."

	sudo mkdir -p $(VISION_WORKER_DIR)

	sudo rsync -a --delete \
		services/vision-worker/ \
		$(VISION_WORKER_DIR)/

	sudo chmod +x \
		$(VISION_WORKER_DIR)/worker.py

	sudo chown -R $(SERVICE_USER):$(SERVICE_USER) \
		$(VISION_WORKER_DIR)

	sudo $(VENV_DIR)/bin/pip install \
		-r $(PYTHON_REQUIREMENTS)

	@echo "Vision worker installed"



# ------------------------------------------------
# SYSTEMD
# ------------------------------------------------

install-systemd:

	@echo "Installing systemd services..."

	sudo rm -f /etc/systemd/system/synora-*.service
	sudo rm -f /etc/systemd/system/mediamtx.service

	sudo install -m 0644 \
		deployments/systemd/*.service \
		/etc/systemd/system/

	@echo "Installing tmpfiles config..."

	echo "d $(RUN_DIR) 0755 $(SERVICE_USER) $(SERVICE_USER) -" | \
		sudo tee /etc/tmpfiles.d/synora.conf

	sudo systemd-tmpfiles --create

	sudo systemctl daemon-reload

# ------------------------------------------------
# INSTALL BINARIES
# ------------------------------------------------

install-bins:

	@echo "Installing binaries..."

	sudo install -m 0755 \
		bin/synora-core \
		$(BIN_DIR)/

	sudo install -m 0755 \
		bin/synora-actions \
		$(BIN_DIR)/

	sudo install -m 0755 \
		bin/synora-bus \
		$(BIN_DIR)/

	sudo install -m 0755 \
		bin/synora-api \
		$(BIN_DIR)/

	sudo install -m 0755 \
		bin/synora-discovery \
		$(BIN_DIR)/

# ------------------------------------------------
# INSTALL WEB
# ------------------------------------------------

install-web:

	@echo "Installing webapp..."

	sudo mkdir -p $(WEB_DIR)

	sudo rsync -a --delete \
		webapp/dist/ \
		$(WEB_DIR)/

	sudo chown -R $(SERVICE_USER):$(SERVICE_USER) \
		$(WEB_DIR)

# ------------------------------------------------
# INSTALL MEDIAMTX
# ------------------------------------------------

install-mediamtx:

	@echo "Installing MediaMTX..."

	sudo mkdir -p $(MEDIA_DIR)

	sudo rsync -a --delete \
		tools/mediamtx/ \
		$(MEDIA_DIR)/

	sudo chown -R $(SERVICE_USER):$(SERVICE_USER) \
		$(MEDIA_DIR)

	sudo chmod +x $(MEDIA_DIR)/mediamtx

# ------------------------------------------------
# INSTALL
# ------------------------------------------------

install: \
	build \
	build-web \
	dirs \
	install-network-deps \
	install-models \
	install-python \
	install-config \
	install-certs \
	install-bins \
	install-web \
	install-mediamtx \
	install-vision-worker \
	install-systemd

	@echo "Stopping services..."

	@for s in $(SERVICES); do \
		sudo systemctl stop $$s 2>/dev/null || true; \
	done

	@echo "Preparing logs..."

	sudo mkdir -p $(LOG_DIR)

	sudo touch $(LOG_FILE)

	sudo chown $(SERVICE_USER):$(SERVICE_USER) \
		$(LOG_FILE)

	sudo chmod 640 $(LOG_FILE)

	@echo "Enabling services..."

	@for s in $(SERVICES); do \
		sudo systemctl enable $$s; \
	done

	@echo "Starting services..."

	@for s in $(SERVICES); do \
		sudo systemctl restart $$s; \
	done

	@echo ""
	@echo "================================="
	@echo "   Synora installation complete"
	@echo "================================="

# ------------------------------------------------
# UPDATE
# ------------------------------------------------

update: \
	build \
	install-bins \
	install-mediamtx \
	install-vision-worker

	@echo "Restarting services..."

	sudo systemctl daemon-reload

	@for s in $(SERVICES); do \
		sudo systemctl restart $$s; \
	done

	@echo ""
	@echo "Update complete."

# ------------------------------------------------
# MANAGEMENT
# ------------------------------------------------

restart:
	@for s in $(SERVICES); do \
		sudo systemctl restart $$s; \
	done

logs:
	journalctl -u synora-* -u mediamtx -f

# ------------------------------------------------
# CLEAN
# ------------------------------------------------

clean:
	@echo "Cleaning build artifacts..."

	rm -rf bin
	rm -rf webapp/dist

# ------------------------------------------------
# DOCTOR
# ------------------------------------------------

doctor:

	@echo ""
	@echo "==============================="
	@echo "        SYNORA DOCTOR"
	@echo "==============================="
	@echo ""

	@echo "[1] Checking user..."

	@if id $(SERVICE_USER) >/dev/null 2>&1; then \
		echo "$(GREEN)OK user exists$(NC)"; \
	else \
		echo "$(RED)FAIL user missing$(NC)"; \
	fi

	@echo ""
	@echo "[2] Checking binaries..."

	@for b in \
		synora-core \
		synora-actions \
		synora-bus \
		synora-api \
		synora-discovery; do \
		if [ -x $(BIN_DIR)/$$b ]; then \
			echo "$(GREEN)OK $$b$(NC)"; \
		else \
			echo "$(RED)FAIL $$b$(NC)"; \
		fi; \
	done

	@echo ""
	@echo "[3] Checking Python AI env..."

	@if [ -x $(VENV_DIR)/bin/python ]; then \
		echo "$(GREEN)OK python venv$(NC)"; \
	else \
		echo "$(RED)FAIL python venv missing$(NC)"; \
	fi

	@echo ""
	@echo "[4] Checking MediaMTX..."

	@if [ -x $(MEDIA_DIR)/mediamtx ]; then \
		echo "$(GREEN)OK mediamtx binary$(NC)"; \
	else \
		echo "$(RED)FAIL mediamtx missing$(NC)"; \
	fi

	@echo ""
	@echo "[5] Checking services..."

	@for s in $(SERVICES); do \
		systemctl is-active $$s >/dev/null 2>&1 && \
		echo "$(GREEN)OK $$s running$(NC)" || \
		echo "$(RED)WARN $$s stopped$(NC)"; \
	done

	@echo ""
	@echo "[6] Checking socket..."

	@if [ -S $(RUN_DIR)/bus.sock ]; then \
		echo "$(GREEN)OK bus.sock exists$(NC)"; \
	else \
		echo "$(RED)FAIL bus.sock missing$(NC)"; \
	fi

	@echo ""
	@echo "[7] Checking MediaMTX API..."

	@curl -s http://localhost:9997/v3/paths/list >/dev/null 2>&1 && \
		echo "$(GREEN)OK mediamtx API$(NC)" || \
		echo "$(RED)FAIL mediamtx API unavailable$(NC)"

	@echo ""
	@echo "[8] Checking webapp..."

	@if [ -f $(WEB_DIR)/index.html ]; then \
		echo "$(GREEN)OK webapp installed$(NC)"; \
	else \
		echo "$(RED)FAIL webapp missing$(NC)"; \
	fi

	@echo ""
	@echo "==============================="
	@echo "        DOCTOR COMPLETE"
	@echo "==============================="
