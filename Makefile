SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c

.PHONY: build test install update start stop restart doctor delete clean help \
	check-go install-deps install-dirs install-bins install-config install-models \
	build-web install-web restart-web web-status rotate-api-token generate-local-cert generate-discovery-cert install-mediamtx install-vision-worker install-face-data install-systemd enable-services \
	system-test-smoke system-test-full system-test-readonly system-test-stress-lite

PREFIX ?= /opt/synora
BINDIR ?= $(PREFIX)/bin
SERVICES_DIR ?= $(PREFIX)/services
VISION_WORKER_DIR ?= $(SERVICES_DIR)/vision-worker
MEDIAMTX_DIR ?= $(PREFIX)/mediamtx
MODELS_DIR ?= $(PREFIX)/models
CONFIG_DIR ?= /etc/synora
DATA_DIR ?= /var/lib/synora
FACE_DATA_DIR ?= $(DATA_DIR)/vision/face
WEBAPP_DIR ?= synora-web
WEB_DIR ?= /var/lib/synora/web
TLS_DIR ?= /etc/synora/tls
DISCOVERY_CERT_DIR ?= /etc/synora/certs
DISCOVERY_CERT_FILE ?= $(DISCOVERY_CERT_DIR)/server.crt
DISCOVERY_KEY_FILE ?= $(DISCOVERY_CERT_DIR)/server.key
TLS_IP ?= 100.80.170.47
TLS_DNS ?= rock-5-itx
SYNORA_SECURITY ?= $(CONFIG_DIR)/security.yaml
SESSION_STORE ?= $(DATA_DIR)/auth/sessions.json
SYSTEMD_DIR ?= /etc/systemd/system
RUN_DIR ?= /run/synora
BUS_SOCKET ?= $(RUN_DIR)/bus.sock
SERVICE_USER ?= synora
GO ?= $(shell if command -v go >/dev/null 2>&1; then command -v go; elif [ -x /usr/local/go/bin/go ]; then printf '%s' /usr/local/go/bin/go; else printf '%s' go; fi)
PYTHON ?= python3
GOCACHE ?= /tmp/synora-gocache

EXPECTED_RKNN_MODELS := arcface_w600k_r50.rknn det_10g.rknn yolov8.rknn weapon.rknn

ifeq ($(shell id -u),0)
SUDO :=
else
SUDO := sudo
endif

GO_BINS := \
	synora-bus:./cmd/synora-bus \
	synora-core:./cmd/synora-core \
	synora-actions:./cmd/synora-actions \
	synora-api:./cmd/synora-api \
	synora-discovery:./cmd/synora-discovery \
	synora-runtime-manager:./cmd/synora-runtime-manager

RUNTIME_SERVICES := \
	synora-bus \
	synora-runtime-manager \
	synora-core \
	synora-actions \
	synora-api \
	synora-discovery

START_ORDER := mediamtx $(RUNTIME_SERVICES)
STOP_ORDER := synora-discovery synora-api synora-actions synora-core synora-runtime-manager synora-bus mediamtx
SYSTEMD_UNITS := $(addsuffix .service,$(RUNTIME_SERVICES)) mediamtx.service

help:
	@printf '%s\n' \
		'Targets:' \
		'  make build                 Build Go runtime binaries into ./bin' \
		'  make build-web             Build the React/Vite webapp statically' \
		'  make test                  Run Go tests and Python compileall' \
		'  make install               Fresh runtime install to /opt, /etc, /var/lib and systemd' \
		'  make install-web           Copy the static webapp to $(WEB_DIR)' \
		'  persistent face data     Keep resident face files in $(FACE_DATA_DIR)' \
		'  make web-status            Show static webapp files and API reachability' \
		'  make rotate-api-token      Rotate security.yaml token and restart synora-api' \
		'  make generate-local-cert  Generate a local self-signed TLS certificate' \
		'  make generate-discovery-cert  Generate the Discovery vision ingress certificate' \
		'  make hash-password PASSWORD=... Generate a bcrypt hash for auth.yaml' \
		'  make update                Rebuild and update deployed runtime without dependency install' \
		'  make start                 Start runtime services' \
		'  make stop                  Stop runtime services and remove bus socket' \
		'  make restart               Stop then start runtime services' \
		'  make doctor                Check local runtime health without modifying state' \
		'  make delete CONFIRM=YES    Destructive removal from the proto machine' \
		'  make clean                 Remove local repo build artifacts'

check-go:
	@if ! command -v "$(GO)" >/dev/null 2>&1 && [ ! -x "$(GO)" ]; then \
		echo "FAIL: Go not found. Install Go or run make GO=/path/to/go <target>."; \
		exit 1; \
	fi
	@"$(GO)" version

build: check-go
	@mkdir -p bin
	@for item in $(GO_BINS); do \
		name="$${item%%:*}"; pkg="$${item#*:}"; \
		echo "Building $$name from $$pkg"; \
		GOCACHE=$(GOCACHE) "$(GO)" build -o "bin/$$name" "$$pkg"; \
		done

hash-password: check-go
	@if [ -z "$(PASSWORD)" ]; then echo "FAIL: PASSWORD is required" >&2; exit 1; fi
	@GOCACHE=$(GOCACHE) "$(GO)" run ./cmd/synora-auth-tool hash-password "$(PASSWORD)"

test: check-go
	GOCACHE=$(GOCACHE) "$(GO)" test ./...
	$(PYTHON) -m compileall -q services/vision-worker

install: install-deps build install-dirs install-bins install-config install-models install-web install-mediamtx install-vision-worker install-face-data install-systemd enable-services
	@echo "Synora runtime installation complete."
	@echo "Run 'make start' to start services or 'make doctor' to inspect the install."

update: build install-bins install-config install-models install-mediamtx install-vision-worker install-systemd
	$(SUDO) systemctl daemon-reload
	@for service in $(START_ORDER); do \
		if systemctl list-unit-files "$$service.service" >/dev/null 2>&1; then \
			echo "Restarting $$service"; \
			$(SUDO) systemctl restart "$$service.service" || true; \
		else \
			echo "Skipping $$service: unit not installed"; \
		fi; \
	done
	@echo "Synora runtime update complete."

install-deps:
	@if command -v apt-get >/dev/null 2>&1; then \
		$(SUDO) apt-get update; \
		$(SUDO) apt-get install -y ca-certificates curl rsync jq python3 python3-opencv python3-numpy python3-scipy python3-venv python3-pip; \
	elif command -v dnf >/dev/null 2>&1; then \
		$(SUDO) dnf install -y ca-certificates curl rsync jq python3 python3-pip; \
	else \
		echo "WARN: no supported package manager found; install ca-certificates curl rsync jq python3 manually."; \
	fi

install-dirs:
	@$(SUDO) getent group $(SERVICE_USER) >/dev/null 2>&1 || \
		$(SUDO) groupadd --system $(SERVICE_USER)
	@$(SUDO) id -u $(SERVICE_USER) >/dev/null 2>&1 || \
		$(SUDO) useradd --system --gid $(SERVICE_USER) --home $(DATA_DIR) --shell /usr/sbin/nologin $(SERVICE_USER)
	$(SUDO) install -d -m 0755 $(PREFIX) $(BINDIR) $(SERVICES_DIR) $(VISION_WORKER_DIR) $(MEDIAMTX_DIR) $(MODELS_DIR)
	$(SUDO) install -d -m 0755 $(CONFIG_DIR) $(DATA_DIR) $(DATA_DIR)/state $(DATA_DIR)/clips $(DATA_DIR)/debug $(DATA_DIR)/logs
	$(SUDO) install -d -m 0750 -o $(SERVICE_USER) -g $(SERVICE_USER) $(DATA_DIR)/vision $(FACE_DATA_DIR)
	$(SUDO) install -d -m 0700 -o $(SERVICE_USER) -g $(SERVICE_USER) $(DATA_DIR)/auth
	$(SUDO) install -d -m 0770 -o $(SERVICE_USER) -g $(SERVICE_USER) $(RUN_DIR)
	$(SUDO) chown -R $(SERVICE_USER):$(SERVICE_USER) $(PREFIX) $(DATA_DIR) $(RUN_DIR)

install-bins: install-dirs
	@for item in $(GO_BINS); do \
		name="$${item%%:*}"; \
		if [ ! -x "bin/$$name" ]; then echo "FAIL: bin/$$name missing; run make build first."; exit 1; fi; \
		echo "Installing $$name"; \
		$(SUDO) install -m 0755 "bin/$$name" "$(BINDIR)/$$name"; \
	done

install-config:
	$(SUDO) install -d -m 0755 -o $(SERVICE_USER) -g $(SERVICE_USER) $(CONFIG_DIR)
	@for src in configs/*.yaml; do \
		name="$$(basename "$$src")"; dst="$(CONFIG_DIR)/$$name"; \
		if [ -e "$$dst" ] || [ -L "$$dst" ]; then \
			echo "Keeping existing $$dst"; \
		else \
			echo "Installing $$dst"; \
			$(SUDO) install -m 0640 -o $(SERVICE_USER) -g $(SERVICE_USER) "$$src" "$$dst"; \
		fi; \
	done

install-models: install-dirs
	@if [ -d models ]; then \
		echo "Installing RKNN models only"; \
		$(SUDO) rsync -a --delete --prune-empty-dirs \
			--include='*/' \
			--include='*.rknn' \
			--exclude='*' \
			models/ $(MODELS_DIR)/; \
		$(SUDO) find $(MODELS_DIR) -type f \( -name '*.onnx' -o -name '*.pt' -o -name '*.torchscript' -o -name '*.engine' \) -delete; \
		$(SUDO) chown -R $(SERVICE_USER):$(SERVICE_USER) $(MODELS_DIR); \
		$(SUDO) find $(MODELS_DIR) -type f -exec chmod 0644 {} \;; \
	else \
		echo "WARN: models directory missing; no RKNN models installed."; \
	fi
	@for model in $(EXPECTED_RKNN_MODELS); do \
		if [ -f "$(MODELS_DIR)/$$model" ]; then echo "Model present: $(MODELS_DIR)/$$model"; \
		else echo "WARN: expected RKNN model missing: $(MODELS_DIR)/$$model"; fi; \
	done

install-vision-worker: install-dirs
	$(PYTHON) -m compileall -q services/vision-worker
	$(SUDO) rsync -a --delete \
		--exclude='__pycache__/' \
		--exclude='*.pyc' \
		--exclude='.pytest_cache/' \
		--exclude='.venv/' \
		--exclude='venv/' \
		services/vision-worker/ $(VISION_WORKER_DIR)/
	$(SUDO) chown -R $(SERVICE_USER):$(SERVICE_USER) $(VISION_WORKER_DIR)
	@if [ -f "$(VISION_WORKER_DIR)/worker.py" ]; then $(SUDO) chmod 0755 "$(VISION_WORKER_DIR)/worker.py"; fi

install-face-data: install-dirs
	@echo "Keeping persistent face data in $(FACE_DATA_DIR)"
	$(SUDO) install -d -m 0750 -o $(SERVICE_USER) -g $(SERVICE_USER) $(FACE_DATA_DIR)
	$(SUDO) chmod 0750 $(FACE_DATA_DIR)
	$(SUDO) chown $(SERVICE_USER):$(SERVICE_USER) $(FACE_DATA_DIR)

build-web:
	@if [ -f "$(WEBAPP_DIR)/package.json" ]; then \
		if ! command -v npm >/dev/null 2>&1; then \
			echo "FAIL: npm is required to build $(WEBAPP_DIR)."; \
			exit 1; \
		fi; \
		echo "Building webapp in $(WEBAPP_DIR)"; \
		if [ -f "$(WEBAPP_DIR)/package-lock.json" ]; then \
			echo "Installing webapp dependencies with npm ci"; \
			( cd "$(WEBAPP_DIR)" && npm ci ); \
		else \
			echo "Installing webapp dependencies with npm install"; \
			( cd "$(WEBAPP_DIR)" && npm install ); \
		fi; \
		echo "Building static webapp"; \
		( cd "$(WEBAPP_DIR)" && npm run build ); \
	else \
		echo "WARN: $(WEBAPP_DIR)/package.json not found; skipping webapp build."; \
	fi

system-test-smoke:
	@./tools/synora_system_test.sh --target local --base-url "$${BASE_URL:-http://127.0.0.1:8080}" --mode smoke $${SYNORA_SYSTEM_TEST_EXTRA_ARGS:-}

system-test-full:
	@./tools/synora_system_test.sh --target local --base-url "$${BASE_URL:-http://127.0.0.1:8080}" --mode full $${SYNORA_SYSTEM_TEST_EXTRA_ARGS:-}

system-test-readonly:
	@./tools/synora_system_test.sh --target local --base-url "$${BASE_URL:-http://127.0.0.1:8080}" --mode readonly $${SYNORA_SYSTEM_TEST_EXTRA_ARGS:-}

system-test-stress-lite:
	@./tools/synora_system_test.sh --target local --base-url "$${BASE_URL:-http://127.0.0.1:8080}" --mode stress-lite $${SYNORA_SYSTEM_TEST_EXTRA_ARGS:-}

install-web: build-web
	@if [ ! -d "$(WEBAPP_DIR)" ]; then \
		echo "WARN: $(WEBAPP_DIR) directory not found; skipping webapp install."; \
		exit 0; \
	fi
	@if [ ! -f "$(WEBAPP_DIR)/dist/index.html" ]; then \
		echo "FAIL: $(WEBAPP_DIR)/dist/index.html is missing; cannot install webapp."; \
		exit 1; \
	fi
	@echo "Copying static webapp to $(WEB_DIR)"
	$(SUDO) install -d -m 0755 -o $(SERVICE_USER) -g $(SERVICE_USER) $(WEB_DIR)
	$(SUDO) rsync -a --delete $(WEBAPP_DIR)/dist/ $(WEB_DIR)/
	$(SUDO) chown -R $(SERVICE_USER):$(SERVICE_USER) $(WEB_DIR)
	@echo "Static webapp copied to $(WEB_DIR)"

restart-web:
	$(MAKE) install-web
	$(SUDO) systemctl restart synora-api

web-status:
	@echo "WEBAPP_DIR=$(WEBAPP_DIR)"
	@echo "WEB_DIR=$(WEB_DIR)"
	@if [ -f "$(WEB_DIR)/index.html" ]; then \
		echo "index.html: present ($(WEB_DIR)/index.html)"; \
	else \
		echo "index.html: missing ($(WEB_DIR)/index.html)"; \
	fi
	@echo "Assets:"
	@if [ -d "$(WEB_DIR)/assets" ]; then \
		find "$(WEB_DIR)/assets" -maxdepth 1 -type f -printf '  %p\n' | sort | head -20; \
	else \
		echo "  assets directory missing"; \
	fi
	@if systemctl is-active --quiet synora-api 2>/dev/null; then \
		curl -I http://127.0.0.1:8080/; \
	else \
		 echo "synora-api is not active; skipping HTTP check"; \
	fi

rotate-api-token: check-go
	@if [ ! -f "$(SYNORA_SECURITY)" ]; then \
		echo "FAIL: $(SYNORA_SECURITY) not found"; \
		exit 1; \
	fi
	@backup="$(SYNORA_SECURITY).bak.$$(date +%Y%m%d-%H%M%S)"; \
	echo "Backing up $(SYNORA_SECURITY) to $$backup"; \
	$(SUDO) cp -a "$(SYNORA_SECURITY)" "$$backup"
	$(SUDO) "$(GO)" run ./cmd/synora-token-rotate -path "$(SYNORA_SECURITY)"
	$(SUDO) systemctl restart synora-api

generate-local-cert:
	@if ! command -v openssl >/dev/null 2>&1; then echo "FAIL: openssl is required" >&2; exit 1; fi
	@if [ -e "$(TLS_DIR)/synora.crt" ] || [ -e "$(TLS_DIR)/synora.key" ]; then \
		echo "FAIL: TLS files already exist in $(TLS_DIR); refusing to overwrite them." >&2; \
		exit 1; \
	fi
	$(SUDO) install -d -m 0750 -o root -g $(SERVICE_USER) $(TLS_DIR)
	@san="IP:127.0.0.1,DNS:localhost,DNS:$(TLS_DNS),DNS:synora.local"; \
	if [ -n "$(TLS_IP)" ]; then san="IP:127.0.0.1,IP:$(TLS_IP),DNS:localhost,DNS:$(TLS_DNS),DNS:synora.local"; fi; \
	$(SUDO) openssl req -x509 -nodes -newkey rsa:2048 -days 825 \
		-keyout "$(TLS_DIR)/synora.key" \
		-out "$(TLS_DIR)/synora.crt" \
		-subj "/CN=$(TLS_DNS)" \
		-addext "subjectAltName=$$san"
	$(SUDO) chown root:$(SERVICE_USER) "$(TLS_DIR)/synora.crt" "$(TLS_DIR)/synora.key"
	$(SUDO) chmod 0644 "$(TLS_DIR)/synora.crt"
	$(SUDO) chmod 0640 "$(TLS_DIR)/synora.key"
	@echo "Generated $(TLS_DIR)/synora.crt and $(TLS_DIR)/synora.key"

generate-discovery-cert:
	@if ! command -v openssl >/dev/null 2>&1; then echo "FAIL: openssl is required" >&2; exit 1; fi
	@if [ -e "$(DISCOVERY_CERT_FILE)" ] || [ -e "$(DISCOVERY_KEY_FILE)" ]; then echo "FAIL: Discovery TLS files already exist; refusing to overwrite them." >&2; exit 1; fi
	$(SUDO) install -d -m 0750 -o root -g $(SERVICE_USER) $(DISCOVERY_CERT_DIR)
	$(SUDO) openssl req -x509 -nodes -newkey rsa:2048 -days 825 \
		-keyout "$(DISCOVERY_KEY_FILE)" \
		-out "$(DISCOVERY_CERT_FILE)" \
		-subj "/CN=$(TLS_DNS)" \
		-addext "subjectAltName=DNS:$(TLS_DNS),IP:$(TLS_IP)"
	$(SUDO) chown root:$(SERVICE_USER) "$(DISCOVERY_CERT_FILE)" "$(DISCOVERY_KEY_FILE)"
	$(SUDO) chmod 0644 "$(DISCOVERY_CERT_FILE)"
	$(SUDO) chmod 0640 "$(DISCOVERY_KEY_FILE)"
	@echo "Generated $(DISCOVERY_CERT_FILE) and $(DISCOVERY_KEY_FILE)"

install-mediamtx: install-dirs
	@if [ -d tools/mediamtx ]; then \
		$(SUDO) rsync -a --delete tools/mediamtx/ $(MEDIAMTX_DIR)/; \
		$(SUDO) chown -R $(SERVICE_USER):$(SERVICE_USER) $(MEDIAMTX_DIR); \
		if [ -f "$(MEDIAMTX_DIR)/mediamtx" ]; then $(SUDO) chmod 0755 "$(MEDIAMTX_DIR)/mediamtx"; fi; \
	else \
		echo "WARN: tools/mediamtx missing; mediamtx unit may not start."; \
	fi

install-systemd:
	$(SUDO) install -d -m 0755 $(SYSTEMD_DIR)
	@for unit in $(SYSTEMD_UNITS); do \
		if [ -f "deployments/systemd/$$unit" ]; then \
			echo "Installing $$unit"; \
			$(SUDO) install -m 0644 "deployments/systemd/$$unit" "$(SYSTEMD_DIR)/$$unit"; \
		else \
			echo "WARN: deployments/systemd/$$unit missing"; \
		fi; \
	done
	@printf 'd %s 0770 %s %s -\n' "$(RUN_DIR)" "$(SERVICE_USER)" "$(SERVICE_USER)" | $(SUDO) tee /etc/tmpfiles.d/synora.conf >/dev/null
	-$(SUDO) systemd-tmpfiles --create /etc/tmpfiles.d/synora.conf
	$(SUDO) systemctl daemon-reload

enable-services:
	@for service in $(START_ORDER); do \
		if systemctl list-unit-files "$$service.service" >/dev/null 2>&1; then \
			echo "Enabling $$service"; \
			$(SUDO) systemctl enable "$$service.service" || true; \
		else \
			echo "Skipping enable for $$service: unit not installed"; \
		fi; \
	done

start:
	@for service in $(START_ORDER); do \
		if systemctl list-unit-files "$$service.service" >/dev/null 2>&1; then \
			echo "Starting $$service"; \
			$(SUDO) systemctl start "$$service.service" || true; \
		else \
			echo "Skipping $$service: unit not installed"; \
		fi; \
	done

stop:
	@for service in $(STOP_ORDER); do \
		if systemctl list-unit-files "$$service.service" >/dev/null 2>&1; then \
			echo "Stopping $$service"; \
			$(SUDO) systemctl stop "$$service.service" || true; \
		else \
			echo "Skipping $$service: unit not installed"; \
		fi; \
	done
	$(SUDO) rm -f $(BUS_SOCKET)

restart: stop start

doctor: check-go
	@echo "== Synora Doctor =="
	@fail=0; warn=0; \
	ok() { echo "OK   $$*"; }; \
	warnf() { echo "WARN $$*"; warn=$$((warn+1)); }; \
	failf() { echo "FAIL $$*"; fail=$$((fail+1)); }; \
	command -v $(PYTHON) >/dev/null 2>&1 && ok "Python: $$($(PYTHON) --version 2>&1)" || failf "Python missing"; \
	command -v jq >/dev/null 2>&1 && ok "jq: $$(jq --version)" || warnf "jq missing"; \
	for bin in synora-bus synora-core synora-actions synora-api synora-discovery synora-runtime-manager; do \
		[ -x "$(BINDIR)/$$bin" ] && ok "binary $(BINDIR)/$$bin" || failf "binary missing $(BINDIR)/$$bin"; \
	done; \
	for service in $(START_ORDER); do \
		if systemctl list-unit-files "$$service.service" >/dev/null 2>&1; then \
			status="$$(systemctl is-active "$$service.service" 2>/dev/null || true)"; \
			ok "unit $$service.service known ($$status)"; \
			systemctl --no-pager --lines=0 status "$$service.service" >/dev/null 2>&1 || true; \
		else \
			[ "$$service" = "mediamtx" ] && warnf "unit $$service.service missing" || failf "unit $$service.service missing"; \
		fi; \
	done; \
	if systemctl is-active synora-bus.service >/dev/null 2>&1; then \
		if [ -S "$(BUS_SOCKET)" ]; then \
			ok "bus socket $(BUS_SOCKET)"; \
			[ -r "$(BUS_SOCKET)" ] && [ -w "$(BUS_SOCKET)" ] && ok "bus socket accessible by current user" || warnf "bus socket not accessible by current user; add user to group $(SERVICE_USER)"; \
		else \
			failf "bus socket missing"; \
		fi; \
	else \
		warnf "synora-bus inactive; skipping active socket requirement"; \
	fi; \
	getent group $(SERVICE_USER) >/dev/null 2>&1 && ok "group $(SERVICE_USER) exists" || failf "group $(SERVICE_USER) missing"; \
	id -nG "$$(id -un)" | grep -qw "$(SERVICE_USER)" && ok "current user is in group $(SERVICE_USER)" || warnf "current user is not in group $(SERVICE_USER); run: sudo usermod -aG $(SERVICE_USER) $$(id -un)"; \
	$(PYTHON) -c 'import importlib.util,sys; missing=[m for m in ("cv2","numpy","scipy") if importlib.util.find_spec(m) is None]; rk=importlib.util.find_spec("rknnlite"); print("missing="+",".join(missing)); print("rknnlite="+("present" if rk else "missing")); sys.exit(1 if missing else 0)' >/tmp/synora-python-deps.log 2>&1 && ok "vision Python deps import" || { warnf "vision Python deps missing"; cat /tmp/synora-python-deps.log; }; \
	[ -d "$(CONFIG_DIR)" ] && ok "$(CONFIG_DIR) exists" || failf "$(CONFIG_DIR) missing"; \
	[ -f "$(CONFIG_DIR)/cge_critical_chains.yaml" ] && ok "CGE critical chains config present" || failf "CGE critical chains config missing"; \
	[ -d "$(DATA_DIR)" ] && ok "$(DATA_DIR) exists" || failf "$(DATA_DIR) missing"; \
	[ -d "$(DATA_DIR)/state" ] && ok "$(DATA_DIR)/state exists" || failf "$(DATA_DIR)/state missing"; \
	if [ -d "$(MODELS_DIR)" ]; then \
		find "$(MODELS_DIR)" -type f -name '*.rknn' | grep -q . && ok "RKNN models present" || warnf "no RKNN models found"; \
		for model in $(EXPECTED_RKNN_MODELS); do \
			[ -f "$(MODELS_DIR)/$$model" ] && ok "model $$model present" || warnf "model $$model missing; capability will be degraded"; \
		done; \
		find "$(MODELS_DIR)" -type f \( -name '*.onnx' -o -name '*.pt' -o -name '*.torchscript' -o -name '*.engine' \) | grep -q . && failf "forbidden model files installed" || ok "no forbidden model files"; \
	else \
		warnf "$(MODELS_DIR) missing"; \
	fi; \
	[ -f "$(DISCOVERY_CERT_FILE)" ] && [ -f "$(DISCOVERY_KEY_FILE)" ] && ok "vision ingress TLS certificates present" || warnf "vision ingress TLS cert missing: $(DISCOVERY_CERT_FILE)"; \
	code="$$(curl -s -o /tmp/synora-health.json -w '%{http_code}' http://127.0.0.1:8080/api/system/health || true)"; \
	if [ "$$code" = "200" ]; then ok "API health 200"; elif [ "$$code" = "401" ]; then warnf "API protected, provide token"; else warnf "API health unavailable ($$code)"; fi; \
	vision_code="$$(curl -s -o /tmp/synora-vision-capabilities.json -w '%{http_code}' http://127.0.0.1:8094/capabilities || true)"; \
	[ "$$vision_code" = "200" ] && ok "vision capabilities 200" || warnf "vision capabilities unavailable ($$vision_code)"; \
	if [ -n "$${SYNORA_API_TOKEN:-}" ]; then \
		code="$$(curl -s -o /tmp/synora-state.json -w '%{http_code}' -H "Authorization: Bearer $$SYNORA_API_TOKEN" http://127.0.0.1:8080/api/state || true)"; \
		[ "$$code" = "200" ] && ok "API state 200" || warnf "API state returned $$code"; \
		code="$$(curl -s -o /tmp/synora-runtime-diagnostics.json -w '%{http_code}' -H "Authorization: Bearer $$SYNORA_API_TOKEN" http://127.0.0.1:8080/api/runtime/diagnostics || true)"; \
		[ "$$code" = "200" ] && ok "runtime diagnostics 200" || warnf "runtime diagnostics returned $$code"; \
	fi; \
	find . -name '*_test.go' | grep -q . && ok "Go tests present" || failf "no Go tests found"; \
		go_test_log="$$(mktemp /tmp/synora-go-test.XXXXXX.log)"; \
		compileall_log="$$(mktemp /tmp/synora-compileall.XXXXXX.log)"; \
		pkgs="$$( $(GO) list ./... | grep -v '/node_modules/' )"; \
		GOCACHE=$(GOCACHE) "$(GO)" test $$pkgs >"$$go_test_log" 2>&1 && ok "go test ./..." || { failf "go test ./... failed"; tail -n 40 "$$go_test_log"; }; \
		$(PYTHON) -m compileall -q services/vision-worker >"$$compileall_log" 2>&1 && ok "vision worker compileall" || { failf "vision worker compileall failed"; cat "$$compileall_log"; }; \
		rm -f "$$go_test_log" "$$compileall_log"; \
		echo "Summary: $$fail fail(s), $$warn warning(s)"; \
		[ "$$fail" -eq 0 ]

delete:
	@if [ "$(CONFIRM)" != "YES" ]; then \
		echo "Refusing destructive delete. Run: make delete CONFIRM=YES"; \
		exit 1; \
	fi
	$(MAKE) stop
	@stamp="$$(date +%Y%m%d-%H%M%S)"; \
	backup_dir="$(DATA_DIR)/backups"; \
	if [ ! -d "$(DATA_DIR)" ]; then backup_dir="/tmp"; fi; \
	$(SUDO) mkdir -p "$$backup_dir"; \
	if [ -d "$(CONFIG_DIR)" ] || [ -d "$(DATA_DIR)" ]; then \
		echo "Creating backup $$backup_dir/pre-delete-$$stamp.tar.gz"; \
		$(SUDO) tar -czf "$$backup_dir/pre-delete-$$stamp.tar.gz" --ignore-failed-read $(CONFIG_DIR) $(DATA_DIR) 2>/dev/null || true; \
	fi
	$(SUDO) rm -rf $(PREFIX) $(CONFIG_DIR) $(DATA_DIR) $(RUN_DIR)
	$(SUDO) rm -f \
		$(SYSTEMD_DIR)/synora-bus.service \
		$(SYSTEMD_DIR)/synora-core.service \
		$(SYSTEMD_DIR)/synora-actions.service \
		$(SYSTEMD_DIR)/synora-api.service \
		$(SYSTEMD_DIR)/synora-discovery.service \
		$(SYSTEMD_DIR)/synora-runtime-manager.service \
		$(SYSTEMD_DIR)/mediamtx.service \
		$(SYSTEMD_DIR)/synora-{web,vision,action}.service \
		$(SYSTEMD_DIR)/mqtt"_bridge".service
	$(SUDO) rm -f /etc/tmpfiles.d/synora.conf
	$(SUDO) systemctl daemon-reload
	@echo "Synora runtime files removed."

clean:
	rm -rf bin
	rm -rf /tmp/synora-go-test*.log /tmp/synora-compileall*.log /tmp/synora-health.json /tmp/synora-state.json
