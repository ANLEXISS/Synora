#!/usr/bin/env bash
set -euo pipefail

# Generate disposable test material only. Never point this script at a
# production directory or commit its output. Production keys remain offline.
out=${1:?usage: create_test_pki.sh OUTPUT_DIR}
mkdir -p "$out"
umask 077
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

openssl genrsa -out "$tmp/root.key" 4096 >/dev/null 2>&1
openssl req -x509 -new -sha256 -days 3650 -key "$tmp/root.key" \
  -subj '/CN=Synora Test Production Root' \
  -addext 'basicConstraints=critical,CA:TRUE,pathlen:1' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign' \
  -out "$out/root.pem" >/dev/null 2>&1

openssl genrsa -out "$tmp/intermediate.key" 3072 >/dev/null 2>&1
openssl req -new -sha256 -key "$tmp/intermediate.key" \
  -subj '/CN=Synora Test Central Release' -out "$tmp/intermediate.csr" >/dev/null 2>&1
openssl x509 -req -sha256 -days 1825 -in "$tmp/intermediate.csr" \
  -CA "$out/root.pem" -CAkey "$tmp/root.key" -CAcreateserial \
  -extfile <(printf '%s\n' 'basicConstraints=critical,CA:TRUE,pathlen:0' 'keyUsage=critical,keyCertSign,cRLSign') \
  -out "$out/intermediate.pem" >/dev/null 2>&1

openssl genrsa -out "$tmp/release.key" 3072 >/dev/null 2>&1
openssl req -new -sha256 -key "$tmp/release.key" \
  -subj '/CN=synora-test-release' -out "$tmp/release.csr" >/dev/null 2>&1
openssl x509 -req -sha256 -days 365 -in "$tmp/release.csr" \
  -CA "$out/intermediate.pem" -CAkey "$tmp/intermediate.key" -CAcreateserial \
  -extfile <(printf '%s\n' 'basicConstraints=critical,CA:FALSE' 'keyUsage=critical,digitalSignature' 'extendedKeyUsage=critical,codeSigning') \
  -out "$out/release.pem" >/dev/null 2>&1

cp "$out/root.pem" "$out/central-ca-bundle.pem"
cat "$out/intermediate.pem" >> "$out/central-ca-bundle.pem"
cp "$tmp"/*.key "$out/"
echo "disposable test PKI written to $out (private keys remain outside the repository)" >&2
