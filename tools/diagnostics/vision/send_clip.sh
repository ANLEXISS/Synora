#!/bin/bash

DEVICE="${SYNORA_DEVICE_ID:-cam_01}"
SECRET="${SYNORA_DEVICE_SECRET:-}"
if [[ -z "$SECRET" ]]; then
  echo "SYNORA_DEVICE_SECRET is required; no secret is embedded in this diagnostic." >&2
  exit 2
fi
SECRET_HASH=$(printf "%s" "$SECRET" | sha256sum | awk '{print $1}')

FILE="$1"

TS=$(date +%s)

HASH=$(sha256sum "$FILE" | awk '{print $1}')

PAYLOAD="${DEVICE}${TS}${HASH}"

SIG=$(printf "%s" "$PAYLOAD" | \
openssl dgst -sha256 -hmac "$SECRET_HASH" | \
awk '{print $2}')

curl -k https://127.0.0.1:7070/vision \
  -H "X-Synora-Device: $DEVICE" \
  -H "X-Synora-Timestamp: $TS" \
  -H "X-Synora-Signature: $SIG" \
  -F "clip=@${FILE}"
