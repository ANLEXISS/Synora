#!/usr/bin/env bash
set -euo pipefail

REPO_DIR="${REPO_DIR:-$HOME/WorkSpace/Synora}"
PROTO_HOST="${PROTO_HOST:-rock@100.80.170.47}"
PROTO_DIR="${PROTO_DIR:-~/Synora}"

COMMIT_MSG="${1:-}"

if [[ -z "$COMMIT_MSG" ]]; then
  echo "Usage:"
  echo "  $0 \"message de commit\""
  echo
  echo "Exemple:"
  echo "  $0 \"Fix CGE validation sequence\""
  exit 2
fi

cd "$REPO_DIR"

echo "=== Synora dev commit + push + rsync ==="
echo "Repo local : $REPO_DIR"
echo "Proto      : $PROTO_HOST:$PROTO_DIR"
echo

echo "=== Vérification fichiers dangereux ==="
BAD_FILES="$(git status --porcelain | grep -E 'node_modules|(^|/)dist/|\.vite|__pycache__|\.pyc|sessions\.json|state\.json|/var/lib|/etc/synora' || true)"

if [[ -n "$BAD_FILES" ]]; then
  echo "ATTENTION : fichiers potentiellement dangereux détectés :"
  echo "$BAD_FILES"
  echo
  read -r -p "Continuer quand même ? [y/N] " ANSWER
  if [[ "$ANSWER" != "y" && "$ANSWER" != "Y" ]]; then
    echo "Abandon."
    exit 1
  fi
fi

echo
echo "=== git diff --check ==="
git diff --check

echo
echo "=== git status ==="
git status --short

echo
echo "=== git add ==="
git add cmd internal pkg docs synora-web configs deployments Makefile tools services 2>/dev/null || true

echo
echo "=== git status après add ==="
git status --short

if git diff --cached --quiet; then
  echo
  echo "Aucun changement staged. Pas de commit."
else
  echo
  echo "=== git commit ==="
  git commit -m "$COMMIT_MSG"

  echo
  echo "=== git push ==="
  git push
fi

echo
echo "=== rsync vers prototype ==="
rsync -avz --delete --info=progress2 \
  --exclude='.git/' \
  --exclude='**/node_modules/' \
  --exclude='**/dist/' \
  --exclude='**/.vite/' \
  --exclude='**/.cache/' \
  --exclude='**/.venv/' \
  --exclude='**/venv/' \
  --exclude='**/__pycache__/' \
  --exclude='**/*.pyc' \
  --exclude='artifacts/system-test/' \
  ./ \
  "$PROTO_HOST:$PROTO_DIR/"

echo
echo "=== Terminé ==="
echo "Code synchronisé sur $PROTO_HOST:$PROTO_DIR"
