#!/usr/bin/env bash
set -euo pipefail

PREFIX="${PREFIX:-/opt/synora}"
BINDIR="${BINDIR:-${PREFIX}/bin}"
SERVICES_DIR="${SERVICES_DIR:-${PREFIX}/services}"
VISION_WORKER_DIR="${VISION_WORKER_DIR:-${SERVICES_DIR}/vision-worker}"
MEDIAMTX_DIR="${MEDIAMTX_DIR:-${PREFIX}/mediamtx}"
MODELS_DIR="${MODELS_DIR:-/var/lib/synora/models}"
CONFIG_DIR="${CONFIG_DIR:-/etc/synora}"
TEMPLATE_DIR="${TEMPLATE_DIR:-${PREFIX}/config-templates}"
DATA_DIR="${DATA_DIR:-/var/lib/synora}"
WEB_DIR="${WEB_DIR:-${PREFIX}/web}"
SYSTEMD_DIR="${SYSTEMD_DIR:-/etc/systemd/system}"
SERVICE_USER="${SERVICE_USER:-synora}"

printf 'Synora standard install plan (read-only)\n'
printf 'No filesystem, service, package manager, or config mutation is performed.\n\n'
printf 'Configuration policy: Git YAML files are safe templates; generated runtime configs are created by synora-bootstrap-config; secrets are generated locally and are never copied from Git. Existing /etc/synora files are preserved.\n\n'
printf '%-10s %-48s %-52s %-10s %-5s %-12s %-12s %s\n' \
	'TYPE' 'SOURCE' 'DESTINATION' 'MODE' 'OWNER' 'GROUP' 'STATUS' 'NOTES'
printf '%s\n' "$(printf '%0.s-' {1..180})"

emit() {
	local type="$1" source="$2" destination="$3" mode="$4" owner="$5" group="$6" notes="${7:-}"
	local status='planned'
	if [[ -n "$source" && ! -e "$source" ]]; then
		status='missing'
		if [[ "$source" == 'models/weapon.rknn' ]]; then
			status='optional-degraded'
		fi
	fi
	printf '%-10s %-48s %-52s %-10s %-5s %-12s %-12s %s\n' \
		"$type" "$source" "$destination" "$mode" "$owner" "$group" "$status" "$notes"
}

emit_dir() {
	local path="$1" mode="$2" owner="$3" group="$4" notes="${5:-}"
	emit 'directory' '' "$path" "$mode" "$owner" "$group" "$notes"
}

emit_file() {
	local source="$1" destination="$2" mode="$3" owner="$4" group="$5" notes="${6:-}"
	emit 'file' "$source" "$destination" "$mode" "$owner" "$group" "$notes"
}

emit_dir "$PREFIX" 0755 root root 'runtime prefix'
emit_dir "$BINDIR" 0755 root root 'runtime and admin binaries'
emit_dir "$SERVICES_DIR" 0755 "$SERVICE_USER" "$SERVICE_USER" 'service payloads'
emit_dir "$VISION_WORKER_DIR" 0755 "$SERVICE_USER" "$SERVICE_USER" 'runtime files only'
emit_dir "$MEDIAMTX_DIR" 0755 "$SERVICE_USER" "$SERVICE_USER" 'required media service'
emit_dir "$CONFIG_DIR" 0755 root synora 'runtime configs; existing files are preserved'
emit_dir "$CONFIG_DIR/secrets" 0750 root synora 'generated locally; never copied from Git'
emit_file '' "$CONFIG_DIR/secrets/api_token" 0600 root synora 'generated locally; never copied from Git'
emit_file '' "$CONFIG_DIR/secrets/session_secret" 0640 root synora 'generated locally; readable by synora-api'
emit_file '' "$CONFIG_DIR/secrets/synoranet_psk" 0600 root synora 'generated locally; never copied from Git'
emit_file '' "$CONFIG_DIR/secrets/admin_initial_password" 0600 root synora 'generated locally; never copied from Git'
emit_dir "$TEMPLATE_DIR" 0755 root root 'safe versioned templates'
emit_dir "$DATA_DIR" 0755 "$SERVICE_USER" "$SERVICE_USER" 'persistent data root'
emit_dir "$DATA_DIR/state" 0755 "$SERVICE_USER" "$SERVICE_USER" 'persistent state'
emit_dir "$DATA_DIR/clips" 0755 "$SERVICE_USER" "$SERVICE_USER" 'persistent clips'
emit_dir "$DATA_DIR/debug" 0755 "$SERVICE_USER" "$SERVICE_USER" 'runtime debug data'
emit_dir "$DATA_DIR/logs" 0755 "$SERVICE_USER" "$SERVICE_USER" 'runtime logs'
emit_dir "$DATA_DIR/connectivity" 0750 "$SERVICE_USER" "$SERVICE_USER" 'persistent across OTA; identity and local connectivity state'
emit_file '' "$DATA_DIR/connectivity/device-identity.key" 0600 "$SERVICE_USER" "$SERVICE_USER" 'generated locally; never copied from Git or rootfs'
emit_file '' "$DATA_DIR/connectivity/wireguard.key" 0600 "$SERVICE_USER" "$SERVICE_USER" 'generated locally; no interface created in this pass'
emit_file '' "$DATA_DIR/connectivity/state.json" 0640 "$SERVICE_USER" "$SERVICE_USER" 'public state only; persistent across OTA'
emit_dir "$DATA_DIR/vision" 0750 "$SERVICE_USER" "$SERVICE_USER" 'vision data'
emit_dir "$DATA_DIR/vision/face" 0750 "$SERVICE_USER" "$SERVICE_USER" 'persistent face data'
emit_dir "$DATA_DIR/auth" 0700 "$SERVICE_USER" "$SERVICE_USER" 'persistent sessions'
emit_dir "$WEB_DIR" 0755 root root 'rootfs static webapp; legacy data fallback is not copied'
emit_dir "$MODELS_DIR" 0755 "$SERVICE_USER" "$SERVICE_USER" 'runtime RKNN models'

while IFS=: read -r name _package; do
	[[ -z "$name" ]] && continue
	emit_file "bin/$name" "$BINDIR/$name" 0755 root root 'production runtime binary'
done <<'RUNTIME_BINS'
synora-bus:./cmd/synora-bus
synora-core:./cmd/synora-core
synora-api:./cmd/synora-api
synora-discovery:./cmd/synora-discovery
synora-actions:./cmd/synora-actions
synora-runtime-manager:./cmd/synora-runtime-manager
synora-network-config:./cmd/synora-network-config
synora-connect:./cmd/synora-connect
RUNTIME_BINS

emit_file 'tools/synora_check.sh' "$BINDIR/synora-check" 0755 root root 'admin diagnostic; installed by make install'
emit_file 'bin/synora-bootstrap-config' "$BINDIR/synora-bootstrap-config" 0755 root root 'local production config bootstrap'
emit_file 'bin/synora-boot-healthcheck' "$BINDIR/synora-boot-healthcheck" 0755 root root 'readonly post-boot healthcheck; exit 1 means rollback recommended'
emit_file 'build/version.json' "$PREFIX/version.json" 0644 root root 'non-secret image identity; generated during build'

if [[ -d synora-web/dist ]]; then
	while IFS= read -r source; do
		relative="${source#synora-web/dist/}"
		emit_file "$source" "$WEB_DIR/$relative" 0644 root root 'rootfs static webapp; source tree is not copied'
	done < <(find synora-web/dist -type f | sort)
else
	emit_file 'synora-web/dist/index.html' "$WEB_DIR/index.html" 0644 root root 'static webapp; run npm build first'
fi

for source in configs/*.yaml; do
	[[ -e "$source" ]] || continue
	name="$(basename "$source")"
	emit_file "$source" "$TEMPLATE_DIR/$name" 0644 root root 'safe template archive'
	case "$name" in
		models.yaml)
			emit 'rootfs-manifest' "$source" "$PREFIX/models-manifest.yaml" 0644 root root 'model manifest; not copied into /etc'
			continue
			;;
		security.yaml|auth.yaml|network.yaml|devices.yaml)
			emit 'generated' "$source" "$CONFIG_DIR/$name" 0640 root synora 'generated by synora-bootstrap-config; not copied directly'
			;;
		*)
			emit_file "$source" "$CONFIG_DIR/$name" 0640 root synora 'non-secret runtime config; existing destination preserved'
			;;
	esac
done

for source in configs/*.yaml.template; do
	[[ -e "$source" ]] || continue
	emit_file "$source" "$TEMPLATE_DIR/$(basename "$source")" 0644 root root 'safe bootstrap template'
done

for model in arcface_w600k_r50.rknn det_10g.rknn yolov8.rknn weapon.rknn; do
	source="models/$model"
	note='required RKNN model'
	if [[ "$model" == 'weapon.rknn' ]]; then note='optional RKNN model; missing means weapon_detection degraded'; fi
	emit_file "$source" "$MODELS_DIR/$model" 0644 "$SERVICE_USER" "$SERVICE_USER" "$note"
done

printf 'Legacy web compatibility: /var/lib/synora/web is a prototype fallback only and is not part of the OTA rootfs install plan.\n'

if [[ -d tools/mediamtx ]]; then
	while IFS= read -r source; do
		relative="${source#tools/mediamtx/}"
		mode=0644
		if [[ "$(basename "$source")" == 'mediamtx' ]]; then mode=0755; fi
		emit_file "$source" "$MEDIAMTX_DIR/$relative" "$mode" "$SERVICE_USER" "$SERVICE_USER" 'required media runtime'
	done < <(find tools/mediamtx -type f -not -path '*/.*' | sort)
fi

for source in worker.py requirements.txt; do
	if [[ -e "services/vision-worker/$source" ]]; then
		mode=0644
		if [[ "$source" == worker.py ]]; then mode=0755; fi
		emit_file "services/vision-worker/$source" "$VISION_WORKER_DIR/$source" "$mode" "$SERVICE_USER" "$SERVICE_USER" 'vision runtime allowlist'
	fi
done

for directory in core modules utils video; do
	if [[ -d "services/vision-worker/$directory" ]]; then
		while IFS= read -r source; do
			relative="${source#services/vision-worker/}"
			emit_file "$source" "$VISION_WORKER_DIR/$relative" 0644 "$SERVICE_USER" "$SERVICE_USER" 'vision runtime allowlist'
		done < <(find "services/vision-worker/$directory" -type f -not -path '*/__pycache__/*' -not -name '*.pyc' | sort)
	fi
done

for source in synora-bus.service synora-runtime-manager.service synora-core.service synora-actions.service synora-api.service synora-discovery.service synora-connect.service mediamtx.service; do
	emit_file "deployments/systemd/$source" "$SYSTEMD_DIR/$source" 0644 root root 'systemd unit'
done

emit_dir /run/synora 0770 "$SERVICE_USER" "$SERVICE_USER" 'runtime tmpfiles entry, not persistent'
printf '\nExternal runtime dependencies (installed by install-deps, not copied from this repo): ca-certificates curl rsync jq python3 python3-opencv python3-numpy python3-scipy python3-venv python3-pip (apt path; dnf path is smaller).\n'
printf 'Excluded by standard install: tools/dev, tools/diagnostics/vision, tests, archives, fixtures, scenarios, web node_modules, vision-worker docs/tests, developer simulators, and secrets from Git.\n'
