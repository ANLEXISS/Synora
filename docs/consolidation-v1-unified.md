# Synora V1 unified consolidation

Base: `integration/synora-v1` at `af6d9b9`.
Destination: `integration/synora-v1-unified` in the dedicated worktree
`/home/rock/Synora-worktrees/synora-v1-unified`.

## Audit matrix

| Fonctionnalité | Source | Déjà dans la base | Décision | Vérification |
| --- | --- | --- | --- | --- |
| Runtime V1 M001–M046 | `integration/synora-v1`; M001–M046 are ancestors | Oui | Conserver | Tests V1 existants |
| Persistance / reprise | M008, M034, M042; ancestors | Oui | Conserver | State, actions, backup and restart tests |
| Lifecycle / backpressure / budgets | M010, M041; ancestors | Oui | Conserver et durcir l’ingress | Tests saturation et budgets |
| E2E hermétique | M044; ancestor | Oui | Étendre le harness permanent | `v1_hermetic_e2e_test.go` |
| Home World Model | modifications locales de la stable `301cca4` | Non | Intégrer comme couche descriptive additive | S01–S10, engine/state/snapshot tests |
| Vision Clip V1 | modifications locales de `feature/vision-clip-pipeline-v1` | Non | Intégrer opt-in `pipeline: clip-v1` | Go + unittest Python |
| Cognitive runtime foundation | modifications locales de `feature/cognitive-runtime-foundation` | Non | Intégrer advisory/shadow/dry-run | Cognitive/eval/core tests |
| M047 RKNN Rock 5T | branche non ancêtre | Non | Exclure | Qualification physique non démontrée |
| M048 camera pairing live | branche non ancêtre | Non | Exclure | Qualification matérielle hors périmètre |
| Audit stable / fichier d’audit | `/home/rock/Synora/SYNORA_ARCHITECTURE_AUDIT.md` | Non | Exclure | La stable reste inchangée |

## Authority and safety

Vision and the Home Model publish observations or descriptive projections.
The historical engine remains the teacher/fallback. The Cognitive foundation
only emits `CognitiveOutput{advisory_only:true}` and its protocol is isolated
from Core input events. No cognitive output contains an executable command.

Actions now expose one production gate: `disabled`, `dry_run` (default), or
`armed`. `armed` additionally requires the exact explicit confirmation
`SYNORA_ACTIONS_ARMED_CONFIRMATION=I_UNDERSTAND_PHYSICAL_ACTIONS`.
The unified E2E uses `dry_run` and asserts that no executor is called.

Core ingress classifies overload as `critical`, `important`, or `best_effort`.
Critical events apply backpressure until admitted, or are refused explicitly
when no critical queue exists. Lower classes are bounded and expose accepted
and rejected counters. No second durable delivery mechanism was introduced;
the existing outbox/delivery path remains the durable bus delivery mechanism.

The laboratory SFace RKNN file was not wired into the worker. The Vision V1
pipeline reports unavailable or inconclusive enrichment where a model is not
available, and raw media/embeddings remain local references.

The large generated parity fixture from the source worktree is intentionally
not copied into this consolidation; parity generation remains opt-in through
`make cognitive-parity` with the verified model bundle mounted.

## Permanent validation command

Run the single hermetic harness for every V1 increment:

```text
make e2e-v1
```

It uses only in-process fakes and temporary directories. The test traverses
Discovery ingress, the fixed `clip-v1` path, the Vision Worker boundary, the
Core bus and state, the Cognitive scheduler in advisory mode, and Actions in
`dry_run`. It writes and rereads a normalized `v1-trace.jsonl` artifact under
the test temporary directory, then exercises replay, retry, saturation and
restart. No system service, network device, MQTT broker, notification target
or physical adapter is started.

## Rollback

The product baseline remains untouched. To roll back the unified product
branch, stop using its dedicated worktree and return to the original candidate:

```text
cd /home/rock/Synora-worktrees/v1-integration
git switch integration/synora-v1
```

This does not alter `/home/rock/Synora`; its pre-existing dirty Home Model
changes remain preserved there.
