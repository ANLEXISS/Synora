#!/bin/bash

DEVICE="cam_01"
SECRET="a8c1f3b2e7d44921c83f6a4d0c9e2f1b"

FILE="$1"

TS=$(date +%s)

HASH=$(sha256sum "$FILE" | awk '{print $1}')

PAYLOAD="${DEVICE}${TS}${HASH}"

SIG=$(printf "%s" "$PAYLOAD" | \
openssl dgst -sha256 -hmac "$SECRET" | \
awk '{print $2}')

curl -k https://127.0.0.1:7070/vision \
  -H "X-Synora-Device: $DEVICE" \
  -H "X-Synora-Timestamp: $TS" \
  -H "X-Synora-Signature: $SIG" \
  -F "clip=@${FILE}"