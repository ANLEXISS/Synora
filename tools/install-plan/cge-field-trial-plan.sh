#!/bin/sh
set -eu

if [ "${1:-}" != "--print" ]; then
  echo "This is an inert installation plan. Re-run with --print to display proposed manual steps."
  exit 0
fi
cat <<'EOF'
Manual review only; no command below is executed by this script.

1. Create the field-trial root with the chosen owner/group and mode 0750.
2. Install the adapted environment and topology files with mode 0640.
3. Generate a separate pseudonymization key with synora-cge-trial key generate.
4. Validate with preflight and prepare a DeploymentManifest.
5. If approved by the operator, place the optional systemd drop-in manually.
6. Validate, then perform any daemon-reload/restart manually under the site's change control.
EOF
