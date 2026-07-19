# Synora Lab

Synora Lab is the admin/operator validation companion for the Synora product.
The web module is the primary product surface; this CLI is kept for
commissioning and maintenance workflows outside the webapp. It is not a
systemd runtime service and is not installed by default.

It reads `PublicSnapshot` from the Synora API and injects test events into Core through the Unix Bus. It is not a runtime service and is not installed by systemd.

## Product Lab vs developer simulation

The controlled validation page in the webapp is Synora Lab proper. This CLI is
its operator companion. Developer-only simulators remain separate and are
disabled by default by `features.dev_simulation_enabled`.

The web Lab uses the controlled validation markers documented in
`docs/synora-lab.md`. The historical CLI companion additionally uses the
following simulation metadata for its dry-run/operator flows:

- `payload.metadata.simulated = true`
- `payload.metadata.test_run_id`
- `payload.metadata.scenario_id` when running a scenario
- `payload.metadata.scenario_step_id` when running a scenario step
- `payload.metadata.generated_by = "synora-lab"`

When `--dry-run-actions` is used, metadata also includes `dry_run = true`. Automations propagate that metadata to `ActionRequest.metadata`, and `synora-actions` returns a simulated result without calling the real action handler.

## Prerequisites

- Synora Bus, Core, and API are running.
- The simulated device exists in the Synora device registry, for example `cam_01`.
- If API auth is enabled, set `SYNORA_API_TOKEN` or pass `--token`.

Defaults:

- API: `http://127.0.0.1:8080/api/state`
- Bus: `/run/synora/bus.sock`
- Device/camera: `cam_01`
- Identity: `alexis`

## Examples

```bash
go run ./tools/dev/synora-lab --send vision.unknown --device cam_01 --node maison.rdc.entree
go run ./tools/dev/synora-lab --send vision.identity --identity alexis --device cam_01
go run ./tools/dev/synora-lab --identity alexis --device cam_01 --node maison.rdc.entree
go run ./tools/dev/synora-lab --list-scenarios
go run ./tools/dev/synora-lab --scenario unknown_at_entrance
go run ./tools/dev/synora-lab --scenario unknown_at_entrance --dry-run-actions
go run ./tools/dev/synora-lab --watch
go run ./tools/dev/synora-lab --no-tui
```

Use an API base URL or the full state URL:

```bash
go run ./tools/dev/synora-lab --api http://127.0.0.1:8080 --send vision.motion
```

## Interactive Commands

- `r`: refresh snapshot
- `1`: send `vision.identity`
- `2`: send `vision.unknown`
- `3`: send `vision.uncertain`
- `4`: send `vision.motion`
- `5`: send `vision.weapon`
- `6`: send `vision.fall`
- `7`: send `vision.tamper`
- `o`: send `discovery.camera.online`
- `x`: send `device.offline`
- `s`: run a predefined scenario
- `q`: quit

Commands are entered in the terminal prompt and confirmed with Enter.

## Scenarios

- `resident_enters_home`
- `unknown_at_entrance`
- `camera_offline`
- `fall_detected`
- `weapon_detected`
- `uncertain_identity`

Each scenario sends events to Core, refreshes the snapshot after each step, and prints a compact state summary.

## Dry-Run Actions

```bash
go run ./tools/dev/synora-lab --scenario unknown_at_entrance --dry-run-actions
```

In dry-run mode, matching automations still create `ActionRequest`, but `synora-actions` does not call the real executor. It publishes an `ActionResult` with `status = simulated_success` and `data.dry_run = true`.
