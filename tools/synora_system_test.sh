#!/usr/bin/env bash

# Reproducible, read-mostly system checks for Synora. The full mode performs
# only bounded, administrator-scoped cleanup-safe mutations. It never starts,
# stops, restarts, installs, or deploys services.

set -uo pipefail

TARGET="local"
TARGET_LABEL="local"
HOST="rock@100.80.170.47"
BASE_URL="http://127.0.0.1:8080"
TOKEN="${SYNORA_API_TOKEN:-}"
MODE="smoke"
REPORT_DIR="artifacts/system-test"
STRICT_SERVICES=0
REPORT_TARGET="local"
REPORT_HOST=""

TMP_DIR=""
CHECKS_FILE=""
LOG_FINDINGS_FILE=""
STARTED_AT=""
START_EPOCH=0
RUN_ID=""
FAILURE_DIR=""
PASS_COUNT=0
WARN_COUNT=0
FAIL_COUNT=0
PRECHECK_FAILED=0
FULL_MUTATION_STARTED=0
CLEANUP_FAILED=0
FINALIZED=0

LAST_BODY=""
LAST_CODE=""
LAST_DURATION=""

usage() {
  cat <<'EOF'
Usage:
  tools/synora_system_test.sh [options]

Options:
  --target local|ssh          Execution target (default: local)
  --host user@host            SSH host for --target ssh
  --base-url URL              Synora API URL (default: http://127.0.0.1:8080)
  --token TOKEN               API bearer token; defaults to SYNORA_API_TOKEN
  --mode smoke|full|readonly|stress-lite
  --report-dir DIR            JSON report directory (default: artifacts/system-test)
  --strict-services           Make degraded optional services blocking
  -h, --help                  Show this help

Examples:
  ./tools/synora_system_test.sh --target local --mode smoke
  ./tools/synora_system_test.sh --target local --base-url http://127.0.0.1:8080 --token "$SYNORA_API_TOKEN" --mode full
  ./tools/synora_system_test.sh --target ssh --host rock@100.80.170.47 --mode full
EOF
}

die_usage() {
  echo "ERROR: $*" >&2
  usage >&2
  exit 2
}

while (($# > 0)); do
  case "$1" in
    --target)
      (($# >= 2)) || die_usage "--target requires a value"
      TARGET="$2"
      shift 2
      ;;
    --host)
      (($# >= 2)) || die_usage "--host requires a value"
      HOST="$2"
      shift 2
      ;;
    --base-url)
      (($# >= 2)) || die_usage "--base-url requires a value"
      BASE_URL="${2%/}"
      shift 2
      ;;
    --token)
      (($# >= 2)) || die_usage "--token requires a value"
      TOKEN="$2"
      shift 2
      ;;
    --mode)
      (($# >= 2)) || die_usage "--mode requires a value"
      MODE="$2"
      shift 2
      ;;
    --report-dir)
      (($# >= 2)) || die_usage "--report-dir requires a value"
      REPORT_DIR="$2"
      shift 2
      ;;
    --strict-services)
      STRICT_SERVICES=1
      shift
      ;;
    --target-label)
      (($# >= 2)) || die_usage "--target-label requires a value"
      TARGET_LABEL="$2"
      shift 2
      ;;
    --report-target)
      (($# >= 2)) || die_usage "--report-target requires a value"
      REPORT_TARGET="$2"
      shift 2
      ;;
    --report-host)
      (($# >= 2)) || die_usage "--report-host requires a value"
      REPORT_HOST="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die_usage "unknown option: $1"
      ;;
  esac
done

case "$TARGET" in
  local|ssh) ;;
  *) die_usage "--target must be local or ssh" ;;
esac
case "$MODE" in
  smoke|full|readonly|stress-lite) ;;
  *) die_usage "unsupported mode: $MODE" ;;
esac

# SSH mode sends this same script to the remote repository. The remote token
# is read there so it never needs to be interpolated into the SSH command.
if [[ "$TARGET" == "ssh" ]]; then
  command -v ssh >/dev/null 2>&1 || {
    echo "ERROR: ssh is required for --target ssh" >&2
    exit 2
  }

  remote_command='cd ~/Synora && SYNORA_API_TOKEN="$(sudo -n awk -F'\''[: ]*'\'' '\''/^api_token:/ {gsub(/"/, "", $2); print $2}'\'' /etc/synora/security.yaml)" bash -s --'
  remote_args=(--target local --target-label ssh --report-target ssh --report-host "$HOST" --base-url "$BASE_URL" --mode "$MODE" --report-dir "$REPORT_DIR")
  if ((STRICT_SERVICES)); then
    remote_args+=(--strict-services)
  fi
  for arg in "${remote_args[@]}"; do
    printf -v quoted '%q' "$arg"
    remote_command+=" $quoted"
  done

  echo "Synora System Test"
  echo "Target: ssh ($HOST)"
  echo "Remote repository: ~/Synora"
  echo "Mode: $MODE"
  exec ssh "$HOST" "$remote_command" < "$0"
fi

mkdir -p "$REPORT_DIR" 2>/dev/null || {
  echo "ERROR: cannot create report directory: $REPORT_DIR" >&2
  exit 2
}

if ! command -v curl >/dev/null 2>&1; then
  echo "ERROR: curl is required" >&2
  exit 2
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "ERROR: jq is required to validate responses and write the JSON report" >&2
  exit 2
fi
if [[ -z "${TOKEN//[[:space:]]/}" ]]; then
  echo "ERROR: API token missing; set SYNORA_API_TOKEN or use --token" >&2
  exit 2
fi

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/synora-system-test.XXXXXX")" || exit 2
CHECKS_FILE="$TMP_DIR/checks.ndjson"
LOG_FINDINGS_FILE="$TMP_DIR/log-findings.ndjson"
: > "$CHECKS_FILE"
: > "$LOG_FINDINGS_FILE"
STARTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
START_EPOCH="$(date +%s)"
RUN_ID="$(date -u +%Y%m%d-%H%M%S)"
FAILURE_DIR="$REPORT_DIR/synora-system-test-$RUN_ID-failures"

cleanup_tmp() {
  [[ -n "$TMP_DIR" && -d "$TMP_DIR" ]] && rm -rf "$TMP_DIR"
}

preserve_failure_artifacts() {
  local name="$1"
  local body="${2:-}"
  local stderr="${3:-}"
  local headers="${4:-}"
  local destination
  destination="$FAILURE_DIR/$name"
  mkdir -p "$destination" 2>/dev/null || true
  [[ -f "$body" ]] && cp "$body" "$destination/body" 2>/dev/null || true
  [[ -f "$stderr" ]] && cp "$stderr" "$destination/stderr" 2>/dev/null || true
  [[ -f "$headers" ]] && cp "$headers" "$destination/headers" 2>/dev/null || true
  printf '%s' "$destination"
}

json_details() {
  local raw="${1:-}"
  jq -cn --arg raw "$raw" 'try ($raw | fromjson) catch {}'
}

record_check() {
  local status="$1"
  local name="$2"
  local code="${3:-}"
  local duration="${4:-}"
  local message="${5:-}"
  local details="${6:-{}}"
  local blocking="${7:-false}"
  local prefix="${status^^}"

  case "$status" in
    pass) PASS_COUNT=$((PASS_COUNT + 1)); prefix="PASS" ;;
    warn) WARN_COUNT=$((WARN_COUNT + 1)); prefix="WARN" ;;
    fail) FAIL_COUNT=$((FAIL_COUNT + 1)); prefix="FAIL" ;;
    *) echo "ERROR: invalid check status $status" >&2; return 1 ;;
  esac

  if [[ -n "$duration" ]]; then
    printf '[%s] %s %ss' "$prefix" "$name" "$duration"
  else
    printf '[%s] %s' "$prefix" "$name"
  fi
  [[ -n "$message" ]] && printf ' — %s' "$message"
  printf '\n'

  jq -cn \
    --arg name "$name" \
    --arg status "$status" \
    --arg code "$code" \
    --arg duration "$duration" \
    --arg message "$message" \
    --arg details "$details" \
    --argjson blocking "$blocking" \
    '{name:$name,status:$status,http_code:(if $code == "" then null else ($code|tonumber?) end),duration_seconds:(if $duration == "" then null else ($duration|tonumber?) end),message:$message,details:(try ($details|fromjson) catch {}),blocking:$blocking}' >> "$CHECKS_FILE"
}

status_allowed() {
  local actual="$1"
  local expected="$2"
  local value
  IFS=',' read -r -a expected_values <<< "$expected"
  for value in "${expected_values[@]}"; do
    [[ "$actual" == "$value" ]] && return 0
  done
  return 1
}

duration_under_limit() {
  awk -v value="${1:-0}" 'BEGIN { exit !(value + 0 < 3) }'
}

request() {
  local name="$1"
  local method="$2"
  local path="$3"
  local expected="$4"
  local json_response="$5"
  local data="${6:-}"
  local optional="${7:-false}"
  local safe_name body meta stderr_file curl_rc code duration message failure_path

  safe_name="$(printf '%s' "$name" | tr -cs '[:alnum:]_-' '_')"
  body="$TMP_DIR/${safe_name}.body"
  meta="$TMP_DIR/${safe_name}.meta"
  stderr_file="$TMP_DIR/${safe_name}.stderr"
  LAST_BODY="$body"
  LAST_CODE="000"
  LAST_DURATION=""

  local -a args=(curl -sS --connect-timeout 3 --max-time 3 -o "$body" -w '%{http_code}\t%{time_total}' -H "Authorization: Bearer $TOKEN" -H 'Accept: application/json' -X "$method" "$BASE_URL$path")
  if [[ -n "$data" ]]; then
    args+=(-H 'Content-Type: application/json' --data-raw "$data")
  fi

  "${args[@]}" > "$meta" 2> "$stderr_file"
  curl_rc=$?
  if [[ -s "$meta" ]]; then
    IFS=$'\t' read -r code duration < "$meta"
  else
    code="000"
    duration=""
  fi
  LAST_CODE="$code"
  LAST_DURATION="$duration"

  if ((curl_rc != 0)); then
    failure_path="$(preserve_failure_artifacts "$safe_name" "$body" "$stderr_file")"
    record_check fail "$name" "$code" "$duration" "curl failed; artifacts saved: $failure_path" '{}' true
    return 1
  fi
  if ! duration_under_limit "$duration"; then
    failure_path="$(preserve_failure_artifacts "$safe_name" "$body" "$stderr_file")"
    record_check fail "$name" "$code" "$duration" "response exceeded 3 seconds; artifacts saved: $failure_path" '{}' true
    return 1
  fi
  if [[ "$code" == "500" ]]; then
    failure_path="$(preserve_failure_artifacts "$safe_name" "$body" "$stderr_file")"
    record_check fail "$name" "$code" "$duration" "HTTP 500; artifacts saved: $failure_path" '{}' true
    return 1
  fi
  if ! status_allowed "$code" "$expected"; then
    if [[ "$optional" == "true" && "$code" == "404" ]]; then
      record_check warn "$name" "$code" "$duration" "optional route is absent" '{}' false
      return 0
    fi
    failure_path="$(preserve_failure_artifacts "$safe_name" "$body" "$stderr_file")"
    record_check fail "$name" "$code" "$duration" "expected HTTP $expected; artifacts saved: $failure_path" '{}' true
    return 1
  fi
  if [[ "$json_response" == "true" ]] && ! jq -e . "$body" >/dev/null 2>&1; then
    failure_path="$(preserve_failure_artifacts "$safe_name" "$body" "$stderr_file")"
    record_check fail "$name" "$code" "$duration" "response is not valid JSON; artifacts saved: $failure_path" '{}' true
    return 1
  fi

  message="HTTP $code"
  record_check pass "$name" "$code" "$duration" "$message" '{}' false
  return 0
}

request_html() {
  local name="$1"
  local path="$2"
  local safe_name body meta headers stderr_file curl_rc code duration failure_path
  safe_name="$(printf '%s' "$name" | tr -cs '[:alnum:]_-' '_')"
  body="$TMP_DIR/${safe_name}.body"
  meta="$TMP_DIR/${safe_name}.meta"
  headers="$TMP_DIR/${safe_name}.headers"
  stderr_file="$TMP_DIR/${safe_name}.stderr"
  LAST_BODY="$body"
  LAST_CODE="000"
  LAST_DURATION=""

  curl -sS --connect-timeout 3 --max-time 3 -D "$headers" -o "$body" -w '%{http_code}\t%{time_total}' \
    -H "Authorization: Bearer $TOKEN" "$BASE_URL$path" > "$meta" 2> "$stderr_file"
  curl_rc=$?
  if [[ -s "$meta" ]]; then
    IFS=$'\t' read -r code duration < "$meta"
  else
    code="000"
    duration=""
  fi
  LAST_CODE="$code"
  LAST_DURATION="$duration"

  if ((curl_rc != 0)) || [[ "$code" != "200" ]]; then
    failure_path="$(preserve_failure_artifacts "$safe_name" "$body" "$stderr_file" "$headers")"
    record_check fail "$name" "$code" "$duration" "expected HTTP 200; artifacts saved: $failure_path" '{}' true
    return 1
  fi
  if ! duration_under_limit "$duration"; then
    failure_path="$(preserve_failure_artifacts "$safe_name" "$body" "$stderr_file" "$headers")"
    record_check fail "$name" "$code" "$duration" "response exceeded 3 seconds; artifacts saved: $failure_path" '{}' true
    return 1
  fi
  if ! grep -Eiq '^content-type: *text/html' "$headers" || ! grep -Eiq '<html|<!doctype html' "$body"; then
    failure_path="$(preserve_failure_artifacts "$safe_name" "$body" "$stderr_file" "$headers")"
    record_check fail "$name" "$code" "$duration" "SPA response is not HTML; artifacts saved: $failure_path" '{}' true
    return 1
  fi
  record_check pass "$name" "$code" "$duration" "HTML served" '{}' false
  return 0
}

check_command() {
  local command_name="$1"
  if command -v "$command_name" >/dev/null 2>&1; then
    record_check pass "preflight $command_name" '' '' "available" '{}' false
  else
    record_check fail "preflight $command_name" '' '' "missing" '{}' true
    PRECHECK_FAILED=1
  fi
}

check_services() {
  if ! command -v systemctl >/dev/null 2>&1; then
    record_check warn "services systemctl" '' '' "systemctl unavailable; service inspection skipped" '{}' false
    return
  fi

  local service active substate restarts required
  for service in synora-bus synora-core synora-actions synora-api synora-discovery mediamtx; do
    if ! systemctl list-unit-files "${service}.service" >/dev/null 2>&1; then
      [[ "$service" == "mediamtx" ]] && continue
      if [[ "$service" == synora-bus || "$service" == synora-core || "$service" == synora-api || "$STRICT_SERVICES" == 1 ]]; then
        record_check fail "service $service" '' '' "unit not installed" '{}' true
      else
        record_check warn "service $service" '' '' "unit not installed" '{}' false
      fi
      continue
    fi
    active="$(systemctl show "$service.service" -p ActiveState --value 2>/dev/null || true)"
    substate="$(systemctl show "$service.service" -p SubState --value 2>/dev/null || true)"
    restarts="$(systemctl show "$service.service" -p NRestarts --value 2>/dev/null || true)"
    required=false
    if [[ "$service" == synora-bus || "$service" == synora-core || "$service" == synora-api ]]; then
      required=true
    elif ((STRICT_SERVICES)); then
      required=true
    fi
    if [[ "$active" == active ]]; then
      record_check pass "service $service" '' '' "ActiveState=$active SubState=$substate NRestarts=$restarts" "{\"active_state\":\"$active\",\"sub_state\":\"$substate\",\"restarts\":\"$restarts\"}" false
    elif [[ "$required" == true ]]; then
      record_check fail "service $service" '' '' "ActiveState=$active SubState=$substate NRestarts=$restarts" "{\"active_state\":\"$active\",\"sub_state\":\"$substate\",\"restarts\":\"$restarts\"}" true
    else
      record_check warn "service $service" '' '' "degraded or inactive: ActiveState=$active SubState=$substate NRestarts=$restarts" "{\"active_state\":\"$active\",\"sub_state\":\"$substate\",\"restarts\":\"$restarts\"}" false
    fi
  done
}

critical_endpoints() {
  local prefix="${1:-critical}"
  request "${prefix} GET /api/system/health" GET /api/system/health 200 true || true
  request "${prefix} GET /api/cge/runtime-status" GET /api/cge/runtime-status 200 true || true
  request "${prefix} GET /api/security/mode" GET /api/security/mode 200 true || true
  request "${prefix} GET /api/state" GET /api/state 200 true || true
  request "${prefix} GET /api/devices" GET /api/devices 200 true || true
  request "${prefix} GET /api/residents" GET /api/residents 200 true || true
  request "${prefix} GET /api/automations" GET /api/automations 200 true || true
  request "${prefix} GET /api/topology" GET /api/topology 200 true || true
  request "${prefix} GET /api/events/chains" GET /api/events/chains 200 true || true
}

check_synoranet() {
  local prefix="${1:-SynoraNet}" health_body enabled status band
  request "${prefix} GET /api/system/health" GET /api/system/health 200 true || true
  health_body="$LAST_BODY"
  if [[ ! -s "$health_body" ]] || ! jq -e . "$health_body" >/dev/null 2>&1; then
    record_check warn "${prefix} health" "$LAST_CODE" "$LAST_DURATION" "health response unavailable; AP diagnostics skipped" '{}' false
    return
  fi
  enabled="$(jq -r '.network.enabled // false' "$health_body")"
  status="$(jq -r '.network.synoranet.status // .network.status // "unknown"' "$health_body")"
  band="$(jq -r '.network.active_band // ""' "$health_body")"
  if [[ "$enabled" != "true" ]]; then
    record_check warn "${prefix}" 200 "" "disabled; AP-specific checks skipped" "{\"status\":\"$status\"}" false
    request "${prefix} GET /api/streams" GET /api/streams 200 true '' true || true
    return
  fi
  if [[ "$status" != "ok" && "$status" != "degraded" ]]; then
    record_check fail "${prefix} enabled health" 200 "" "enabled SynoraNet is unavailable" "{\"status\":\"$status\",\"active_band\":\"$band\"}" true
  else
    record_check pass "${prefix} enabled health" 200 "" "status=$status band=${band:-unknown}" "{\"status\":\"$status\",\"active_band\":\"$band\"}" false
    check_synoranet_local "$prefix"
  fi
  request "${prefix} GET /api/streams" GET /api/streams 200 true '' true || true
}

check_synoranet_local() {
  local prefix="${1:-SynoraNet}" port
  [[ "$TARGET" == "local" ]] || return 0
  if command -v ip >/dev/null 2>&1; then
    if ip -4 addr show synorabr0 2>/dev/null | grep -q '10\.77\.0\.1/24'; then
      record_check pass "${prefix} gateway IP" '' '' "10.77.0.1/24 on synorabr0" '{}' false
    else
      record_check fail "${prefix} gateway IP" '' '' "10.77.0.1/24 missing on synorabr0" '{}' true
    fi
  else
    record_check warn "${prefix} gateway IP" '' '' "ip command unavailable" '{}' false
  fi
  for port in 8554 8443; do
    if command -v nc >/dev/null 2>&1 && nc -z -w 2 127.0.0.1 "$port" >/dev/null 2>&1; then
      record_check pass "${prefix} TCP port $port" '' '' "listening on localhost" '{}' false
    elif timeout 2 bash -c "</dev/tcp/127.0.0.1/$port" >/dev/null 2>&1; then
      record_check pass "${prefix} TCP port $port" '' '' "listening on localhost" '{}' false
    else
      record_check fail "${prefix} TCP port $port" '' '' "enabled SynoraNet service port is not reachable" '{}' true
    fi
  done
  for process in hostapd dnsmasq; do
    if command -v pgrep >/dev/null 2>&1 && pgrep -x "$process" >/dev/null 2>&1; then
      record_check pass "${prefix} process $process" '' '' "process present" '{}' false
    else
      record_check fail "${prefix} process $process" '' '' "enabled SynoraNet process is not present" '{}' true
    fi
  done
}

check_runtime_status() {
  local body="$1"
  local mode armed occupancy danger score state score_present
  if [[ ! -s "$body" ]]; then
    record_check fail "runtime status fields" '' '' "runtime response body missing" '{}' true
    return
  fi
  mode="$(jq -r 'if has("security_mode") then .security_mode elif ((.security // null)|type)=="object" and (.security|has("mode")) then .security.mode else null end' "$body")"
  armed="$(jq -r 'if has("security_armed") then .security_armed elif ((.security // null)|type)=="object" and (.security|has("armed")) then .security.armed else null end' "$body")"
  occupancy="$(jq -r 'if has("expected_occupancy") then .expected_occupancy elif ((.security // null)|type)=="object" and (.security|has("expected_occupancy")) then .security.expected_occupancy else null end' "$body")"
  danger="$(jq -r 'if has("danger_level") then .danger_level else null end' "$body")"
  score="$(jq -r 'if has("danger_score") then .danger_score else null end' "$body")"
  score_present="$(jq -r 'if has("danger_score") then "yes" else "no" end' "$body")"
  state="$(jq -r 'if has("current_state") then .current_state else null end' "$body")"

  if [[ "$mode" == null || "$armed" == null || "$occupancy" == null || "$danger" == null || "$score_present" != yes || "$state" == null ]]; then
    record_check fail "runtime status fields" 200 "" "null or missing security/runtime field" "{\"security_mode\":$mode,\"security_armed\":$armed,\"expected_occupancy\":$occupancy,\"danger_level\":$danger,\"danger_score\":$score,\"current_state\":$state}" true
    return
  fi
  if ! jq -e '(.danger_level | IN("none","low","medium","medium_high","high","critical"))' "$body" >/dev/null 2>&1; then
    record_check fail "runtime danger level" 200 "" "unsupported danger_level=$danger" '{}' true
    return
  fi
  record_check pass "runtime status fields" 200 "" "security_mode=$mode security_armed=$armed expected_occupancy=$occupancy danger_level=$danger" "{\"security_mode\":\"$mode\",\"security_armed\":$armed,\"expected_occupancy\":\"$occupancy\",\"danger_level\":\"$danger\",\"danger_score\":$score,\"current_state\":\"$state\"}" false
}

check_chain_collection() {
  local body="$1"
  local valid chains
  valid="$(jq -e 'if type == "array" then true elif ((.chains // null)|type) == "array" then true elif ((.items // null)|type) == "array" then true else false end' "$body" 2>/dev/null || echo false)"
  if [[ "$valid" != true ]]; then
    record_check fail "event chains format" 200 "" "expected array, .chains array, or .items array" '{}' true
    return
  fi
  chains="$(jq -c 'if type == "array" then . elif ((.chains // null)|type) == "array" then .chains elif ((.items // null)|type) == "array" then .items else [] end' "$body")"
  if ! jq -e 'all(.[]; ((.id // null)|type) == "string" and ((.status // null)|type) == "string" and ((.danger_level // null)|type) == "string" and ((.events_count // null)|type) == "number")' <<< "$chains" >/dev/null 2>&1; then
    record_check fail "event chains fields" 200 "" "chain item has incompatible fields" '{}' true
    return
  fi
  record_check pass "event chains format" 200 "" "normalized array length=$(jq 'length' <<< "$chains")" "{\"count\":$(jq 'length' <<< "$chains")}" false
}

check_runtime_and_chains() {
  local runtime_body="$1"
  local chains_body="$2"
  check_runtime_status "$runtime_body"
  check_chain_collection "$chains_body"
}

check_catalog() {
  local body="$1"
  local value
  for value in danger.level security.mode security.armed occupancy.expected manual_risk.active medium_high; do
    if jq -e --arg value "$value" '[.. | strings] | index($value) != null' "$body" >/dev/null 2>&1; then
      record_check pass "automation catalog $value" 200 "" "present" '{}' false
    else
      record_check fail "automation catalog $value" 200 "" "missing" '{}' true
    fi
  done
}

run_security_mode_tests() {
  local payload='{"mode":"high_security","duration_seconds":60,"reason":"system_test_security_mode"}'
  request "POST /api/security/arm" POST /api/security/arm 200 true "$payload" || return
  request "GET /api/security/mode after arm" GET /api/security/mode 200 true || return
  if ! jq -e '(.mode == "high_security" and .armed == true and .expected_occupancy == "empty")' "$LAST_BODY" >/dev/null 2>&1; then
    record_check fail "security mode arm state" 200 "" "expected high_security/armed/empty" '{}' true
  else
    record_check pass "security mode arm state" 200 "" "high_security armed" '{}' false
  fi
  request "POST /api/security/disarm" POST /api/security/disarm 200 true '{}' || return
  request "GET /api/security/mode after disarm" GET /api/security/mode 200 true || return
  if ! jq -e '(.mode == "home" and .armed == false)' "$LAST_BODY" >/dev/null 2>&1; then
    record_check fail "security mode disarm state" 200 "" "expected home/disarmed" '{}' true
  else
    record_check pass "security mode disarm state" 200 "" "home disarmed" '{}' false
  fi
}

run_manual_risk_tests() {
  local payload='{"danger_level":"high","duration_seconds":30,"test":false,"reason":"system_test_manual_risk"}'
  request "POST /api/cge/manual-risk" POST /api/cge/manual-risk 200,202 true "$payload" || return
  request "GET /api/cge/runtime-status after manual risk" GET /api/cge/runtime-status 200 true || return
  if ! jq -e '(.manual_risk_active == true)' "$LAST_BODY" >/dev/null 2>&1; then
    record_check fail "manual risk active state" 200 "" "manual_risk_active is not true" '{}' true
  else
    record_check pass "manual risk active state" 200 "" "manual risk active" '{}' false
  fi
  request "POST /api/cge/manual-risk/clear" POST /api/cge/manual-risk/clear 200 true '{}' || return
  request "GET /api/cge/runtime-status after manual risk clear" GET /api/cge/runtime-status 200 true || return
  if ! jq -e '(.manual_risk_active == false)' "$LAST_BODY" >/dev/null 2>&1; then
    record_check fail "manual risk clear state" 200 "" "manual_risk_active is not false" '{}' true
  else
    record_check pass "manual risk clear state" 200 "" "manual risk cleared" '{}' false
  fi
}

run_validation_tests() {
  local single_payload sequence_payload unsupported_payload
  single_payload='{"event_type":"vision.unknown","device_id":"cam_03","node_id":"zoneA.L0.entree","confidence":0.78,"danger_level_hint":"medium","learn":false,"reason":"system_test_validation_single"}'
  request "POST validation single event" POST /api/cge/validation/events 200,202 true "$single_payload" || true
  if [[ "$LAST_CODE" == 200 || "$LAST_CODE" == 202 ]] && jq -e '(.status == "queued" and (.validation_id|type) == "string" and (.events[0].source_type == "validation") and (.events[0].learn == false))' "$LAST_BODY" >/dev/null 2>&1; then
    record_check pass "validation single event contract" "$LAST_CODE" "$LAST_DURATION" "queued validation event" '{}' false
  else
    record_check fail "validation single event contract" "$LAST_CODE" "$LAST_DURATION" "expected queued validation response; body saved: $LAST_BODY" '{}' true
  fi

  sequence_payload='{"learn":true,"events":[{"event_type":"vision.unknown","device_id":"cam_03","node_id":"zoneA.L0.entree","confidence":0.82,"danger_level_hint":"medium"},{"event_type":"vision.motion","device_id":"cam_03","node_id":"zoneA.L0.entree"},{"event_type":"vision.weapon","device_id":"cam_03","node_id":"zoneA.L0.salon","confidence":0.91,"danger_level_hint":"critical"}]}'
  request "POST validation chain sequence" POST /api/cge/validation/chain-sequence 200,202 true "$sequence_payload" || true
  if [[ "$LAST_CODE" == 200 || "$LAST_CODE" == 202 ]] && jq -e '(.status == "queued" and (.validation_id|type) == "string" and (.events|length) == 3)' "$LAST_BODY" >/dev/null 2>&1; then
    record_check pass "validation chain sequence contract" "$LAST_CODE" "$LAST_DURATION" "queued 3-event sequence" '{}' false
  else
    record_check fail "validation chain sequence contract" "$LAST_CODE" "$LAST_DURATION" "expected queued 3-event sequence; body saved: $LAST_BODY" '{}' true
  fi

  sleep 2
  critical_endpoints "after validation"
  request "after validation GET /api/cge/runtime-status" GET /api/cge/runtime-status 200 true || true
  request "after validation GET /api/events/chains" GET /api/events/chains 200 true || true

  unsupported_payload='{"learn":false,"events":[{"event_type":"totally.unsupported","device_id":"cam_03","node_id":"zoneA.L0.entree"}]}'
  request "POST unsupported validation event" POST /api/cge/validation/chain-sequence 400 true "$unsupported_payload" || true
  if [[ "$LAST_CODE" == 400 ]] && jq -e '(.error == "validation_failed")' "$LAST_BODY" >/dev/null 2>&1; then
    record_check pass "unsupported validation event returns 400" 400 "$LAST_DURATION" "validation_failed" '{}' false
  else
    record_check fail "unsupported validation event returns 400" "$LAST_CODE" "$LAST_DURATION" "expected HTTP 400 validation_failed; body saved: $LAST_BODY" '{}' true
  fi
}

run_stress_lite() {
  local stress_dir="$TMP_DIR/stress"
  local iteration path job_count=0 failure_count=0 result code duration
  mkdir -p "$stress_dir"
  for iteration in 1 2 3 4 5; do
    for path in /api/state /api/devices /api/residents /api/automations /api/topology /api/cge/runtime-status; do
      (
        result="$(curl -sS --connect-timeout 3 --max-time 3 -o /dev/null -w '%{http_code}\t%{time_total}' -H "Authorization: Bearer $TOKEN" "$BASE_URL$path" 2>/dev/null || true)"
        printf '%s\t%s\n' "$path" "$result" > "$stress_dir/${job_count}.result"
      ) &
      job_count=$((job_count + 1))
    done
  done
  wait
  for result in "$stress_dir"/*.result; do
    IFS=$'\t' read -r path code duration < "$result"
    if [[ "$code" != 200 ]] || ! duration_under_limit "$duration"; then
      failure_count=$((failure_count + 1))
    fi
  done
  if ((failure_count > 0)); then
    record_check fail "stress-lite critical endpoints" '' '' "$failure_count/$job_count requests failed or exceeded 3 seconds" "{\"requests\":$job_count,\"failures\":$failure_count}" true
  else
    record_check pass "stress-lite critical endpoints" '' '' "$job_count parallel requests completed" "{\"requests\":$job_count,\"failures\":0}" false
  fi
}

collect_logs() {
  local log_file="$TMP_DIR/recent.log"
  local service pattern findings
  if ! command -v journalctl >/dev/null 2>&1; then
    record_check warn "logs journalctl" '' '' "journalctl unavailable; log inspection skipped" '{}' false
    return
  fi
  : > "$log_file"
  for service in synora-core synora-api synora-bus synora-actions; do
    journalctl -u "$service.service" --since "$STARTED_AT" --no-pager 2>/dev/null >> "$log_file" || true
  done
  pattern='incoming channel full|panic|fatal|deadlock|concurrent map|runtime error|nil pointer|data race|internal server error|5\.00[0-9]*s'
  findings="$(grep -Ein "$pattern" "$log_file" 2>/dev/null || true)"
  if [[ -n "$findings" ]]; then
    printf '%s\n' "$findings" > "$LOG_FINDINGS_FILE"
    mkdir -p "$FAILURE_DIR/logs" 2>/dev/null || true
    cp "$log_file" "$FAILURE_DIR/logs/recent.log" 2>/dev/null || true
    record_check fail "logs critical patterns" '' '' "blocking log patterns found; artifacts saved: $FAILURE_DIR/logs" "{\"log_file\":\"$FAILURE_DIR/logs/recent.log\"}" true
    echo "--- relevant log lines ---"
    printf '%s\n' "$findings" | tail -n 80
  else
    record_check pass "logs critical patterns" '' '' "no saturation, panic, deadlock, or timeout patterns found" '{}' false
  fi
}

run_webapp_checks() {
  request_html "GET /" /
  local index_body="$LAST_BODY"
  request_html "GET /cge" /cge
  request_html "GET /automations" /automations
  request_html "GET /dashboard" /dashboard
  local asset
  asset="$(grep -Eo 'src="/assets/[^" ]+\.js' "$index_body" 2>/dev/null | head -n 1 | sed -E 's/^src="//; s/"$//' || true)"
  if [[ -z "$asset" ]]; then
    record_check warn "webapp Vite asset" '' '' "no /assets/*.js reference found in index" '{}' false
  else
    request "GET $asset" GET "$asset" 200 false || true
    if [[ ! -s "$LAST_BODY" ]]; then
      record_check fail "webapp Vite asset body" "$LAST_CODE" "$LAST_DURATION" "asset body is empty" '{}' true
    fi
  fi
}

cleanup_mutations() {
  ((FULL_MUTATION_STARTED)) || return 0
  local code
  code="$(curl -sS --connect-timeout 3 --max-time 3 -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -X POST --data '{}' "$BASE_URL/api/cge/manual-risk/clear" 2>/dev/null || true)"
  if [[ "$code" != 200 ]]; then
    CLEANUP_FAILED=1
    record_check fail "cleanup manual risk clear" "$code" '' "cleanup failed" '{}' true
  fi
  code="$(curl -sS --connect-timeout 3 --max-time 3 -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -X POST --data '{}' "$BASE_URL/api/security/disarm" 2>/dev/null || true)"
  if [[ "$code" != 200 ]]; then
    CLEANUP_FAILED=1
    record_check fail "cleanup security disarm" "$code" '' "cleanup failed" '{}' true
  fi
}

write_report() {
  local finished_at report_file checks_json findings_json blocking_json
  finished_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  report_file="$REPORT_DIR/synora-system-test-$RUN_ID.json"
  checks_json="$(jq -s '.' "$CHECKS_FILE" 2>/dev/null || echo '[]')"
  findings_json="$(jq -R -s 'split("\n") | map(select(length > 0))' "$LOG_FINDINGS_FILE" 2>/dev/null || echo '[]')"
  blocking_json="$(jq -c '[.[] | select(.status == "fail" and .blocking == true) | {name,message,details}]' <<< "$checks_json")"
  jq -n \
    --arg started_at "$STARTED_AT" \
    --arg finished_at "$finished_at" \
    --arg target "$REPORT_TARGET" \
    --arg target_label "$TARGET_LABEL" \
    --arg host "$REPORT_HOST" \
    --arg base_url "$BASE_URL" \
    --arg mode "$MODE" \
    --argjson checks "$checks_json" \
    --argjson findings "$findings_json" \
    --argjson blocking "$blocking_json" \
    --argjson pass "$PASS_COUNT" \
    --argjson warn "$WARN_COUNT" \
    --argjson fail "$FAIL_COUNT" \
    '{started_at:$started_at,finished_at:$finished_at,target:$target,target_label:$target_label,host:(if $host == "" then null else $host end),base_url:$base_url,mode:$mode,summary:{pass:$pass,warn:$warn,fail:$fail},checks:$checks,blocking_failures:$blocking,log_findings:$findings}' > "$report_file"
  echo ""
  echo "Summary: PASS $PASS_COUNT  WARN $WARN_COUNT  FAIL $FAIL_COUNT"
  echo "Report: $report_file"
}

finish() {
  local original_status="${1:-0}"
  ((FINALIZED)) && return
  FINALIZED=1
  cleanup_mutations
  write_report
  cleanup_tmp
  if ((PRECHECK_FAILED)); then
    exit 2
  fi
  if ((FAIL_COUNT > 0 || CLEANUP_FAILED || original_status != 0)); then
    exit 1
  fi
  exit 0
}
trap 'finish "$?"' EXIT

echo "Synora System Test"
echo "Target: $TARGET_LABEL"
[[ -n "$REPORT_HOST" ]] && echo "Host: $REPORT_HOST"
echo "Base URL: $BASE_URL"
echo "Mode: $MODE"
echo "Started: $STARTED_AT"

check_command curl
check_command jq
if command -v systemctl >/dev/null 2>&1; then
  record_check pass "preflight systemctl" '' '' "available" '{}' false
else
  record_check warn "preflight systemctl" '' '' "unavailable; service checks will be skipped" '{}' false
fi
if command -v journalctl >/dev/null 2>&1; then
  record_check pass "preflight journalctl" '' '' "available" '{}' false
else
  record_check warn "preflight journalctl" '' '' "unavailable; log checks will be skipped" '{}' false
fi
if ! command -v curl >/dev/null 2>&1 || ! command -v jq >/dev/null 2>&1; then
  PRECHECK_FAILED=1
  exit 2
fi
if [[ -z "${TOKEN//[[:space:]]/}" ]]; then
  record_check fail "preflight API token" '' '' "token missing; use --token or SYNORA_API_TOKEN" '{}' true
  PRECHECK_FAILED=1
  exit 2
else
  record_check pass "preflight API token" '' '' "token supplied without printing it" '{}' false
fi

check_services
critical_endpoints "smoke"
check_synoranet "SynoraNet"
request "smoke GET /api/cge/runtime-status format source" GET /api/cge/runtime-status 200 true || true
runtime_body="$LAST_BODY"
request "smoke GET /api/events/chains format source" GET /api/events/chains 200 true || true
chains_body="$LAST_BODY"
check_runtime_and_chains "$runtime_body" "$chains_body"
request "GET /api/cge/validation/history" GET /api/cge/validation/history 200 true '' true || true
request "GET /api/automations/catalog" GET /api/automations/catalog 200 true '' true || true
if [[ "$LAST_CODE" == 200 ]] && jq -e . "$LAST_BODY" >/dev/null 2>&1; then
  check_catalog "$LAST_BODY"
fi
run_webapp_checks

case "$MODE" in
  full)
    FULL_MUTATION_STARTED=1
    run_security_mode_tests || true
    run_manual_risk_tests || true
    run_validation_tests
    request "GET /api/automations/catalog after validation" GET /api/automations/catalog 200 true '' true || true
    if [[ "$LAST_CODE" == 200 ]] && jq -e . "$LAST_BODY" >/dev/null 2>&1; then
      check_catalog "$LAST_BODY"
    fi
    ;;
  stress-lite)
    run_stress_lite
    collect_logs
    ;;
  smoke|readonly)
    ;;
esac

collect_logs
exit 0
