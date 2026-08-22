# Synora V1 — état d’exécution

## Jalon courant

- Jalon : 07 — StateStore et pannes de stockage
- Groupe : 06–10
- État : validé et intégré ; poursuite automatique vers J08
- Branche : `integration/synora-v1-execution`
- Worktree : `/home/rock/Synora-worktrees/v1-execution`
- Base consolidée : `integration/synora-v1`
- HEAD initial : `864a379801bc1537f39624f102b9f9a57c4509c0`

## Jalon 01

- Commit : `0146783f57ab8f26f3a7d99558efd3462ade0d57`
- Validations :
  - `go test ./... -count=1` — PASS
  - `go vet ./...` — PASS
  - `python3 -B -m unittest discover -s services/vision-worker/tests -v` — PASS (21 tests)
  - `go test -race ./... -count=1` — PASS après une première exécution ayant subi
    un timeout de contention dans `cmd/synora-core`; le test concerné passe aussi
    trois fois isolément sous race
  - `git diff --check` — PASS
- Limites : qualification matérielle non exécutée sur cette machine
- Blocages : aucun

La baseline consolidée est donc qualifiée verte pour le jalon 01. Aucun code
produit n’a été modifié pendant cette qualification. Le commit est poussé sur
`origin/integration/synora-v1-execution`.

## Jalon 02

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j02-delivery-contract`
- Branche dédiée : `codex/v1-j02-delivery-contract`
- Commit : `fbee49cdafc7382d2978d4554fc81016a9e4ca60`
- Validations : `go test ./pkg/contract -count=1`, `go vet ./pkg/contract`
  et `go test -race ./pkg/contract -count=1` — PASS
- Périmètre : contrat de livraison uniquement ; aucun contrat public existant
  supprimé ou rendu incompatible
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution`

## Jalon 03

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j03-persistent-outbox`
- Branche dédiée : `codex/v1-j03-persistent-outbox`
- Commit : `56ac7cb19d088431759b328fb01de501f08389c5`
- Validations : `go test ./internal/outbox -count=1`, `go vet
  ./internal/outbox` et `go test -race ./internal/outbox -count=1` — PASS
- Garanties démontrées : écriture temporaire synchronisée puis rename,
  restauration pending/retry, copies bornées, rollback mémoire sur échec
  d’écriture et refus fail-closed d’un fichier corrompu
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution`

## Jalon 04

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j04-dispatcher-ack`
- Branche dédiée : `codex/v1-j04-dispatcher-ack`
- Commit : `3c133347d7f2717a98865d3a5f3d33bfc6daa7af`
- Validations : `go test ./internal/outbox ./internal/dispatcher -count=1`,
  `go vet ./pkg/contract ./internal/outbox ./internal/dispatcher` et
  `go test -race ./internal/outbox ./internal/dispatcher -count=1` — PASS
- Garanties démontrées : ACK explicite, retry borné avec jitter injectable,
  reconnexion simulée, replay après ACK perdu avec identité inchangée,
  rejet des ACK invalides et arrêt d’un transport annulable
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution`

## Jalon 05

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j05-outbox-integration`
- Branche dédiée : `codex/v1-j05-outbox-integration`
- Commits : `69851aef9d6194b7ee9137484cbdfc1f4dc28816` et
  `ff2e61c21607dab0c751b4537670e0f8cd145f52`
- Validations ciblées : `go test ./internal/delivery ./internal/dispatcher
  ./internal/outbox -count=1`, vet ciblé et race ciblé — PASS
- Intégration réelle : bus Unix, ACK `delivery.ack` limité aux métadonnées,
  ordre `incident.created → clip.available`, replay après arrêt avant ACK,
  identité conservée, anciennes epochs et doublons rejetés/idempotents
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution`

## Gate groupe 01–05

- `go test ./... -count=1` — PASS
- `go test ./... -shuffle=on -count=3` — PASS
- `go vet ./...` — PASS
- `timeout 300s go test -race ./... -count=1` — PASS à la seconde exécution
  globale ; la première a subi un timeout de contention isolé, puis le test
  concerné a passé trois fois sous race
- Tests Python Vision — PASS (21 tests)
- `go list ./...` — bloqué par l’environnement Go : le worktree est détecté
  sous le répertoire parent `/home/rock`, qui contient un répertoire `.git`
  vide et non-repository ; la variante strictement équivalente
  `GOFLAGS=-buildvcs=false go list ./...` — PASS et énumère tous les packages.
  Aucun fichier ni configuration du dépôt n’a été modifié pour contourner ce
  problème.
- `git diff --check` et worktree — PASS/propre
- Limites : aucune qualification matérielle effectuée sur cette machine
- Checkpoint : tag annoté `v1-checkpoint-05` créé et poussé sur `origin`, pointant
  sur le commit terminal `f3db7b9bb149b208e168e77ba6057c61bc7e148c`

## Jalon 06

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j06-core-recovery`
- Branche dédiée : `codex/v1-j06-core-recovery`
- Commit : `47c1c9837d86daf8131a9ec601883aed98b59e92`
- Validations ciblées : `go test ./internal/recovery ./internal/state
  ./internal/rpc ./cmd/synora-core -count=1`, vet ciblé et race ciblé — PASS
- Garanties démontrées : états `starting/recovering/running/degraded/failed`,
  transition atomique, readiness refusée avant récupération complète,
  dépendances requises et optionnelles, révocation de readiness sur panne
  requise, persistance de l’état de récupération et exposition dans la santé
  RPC
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution` puis `integration/synora-v1`
- Limites : qualification matérielle non exécutée sur cette machine

## Jalon 07

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j07-state-failures`
- Branche dédiée : `codex/v1-j07-state-failures`
- Commit : `b40b0a169107cc186cd49274502cf5867a57ed23`
- Validations ciblées : `go test ./internal/state -count=1`, `go vet
  ./internal/state` et `go test -race ./internal/state -count=1` — PASS
- Validations complémentaires : `go test ./... -p 1 -count=1` — PASS ; la
  première passe parallèle `go test ./... -count=1` a été interrompue après
  plus de cinq minutes d’attente dans `synora-api`, puis le package isolé a
  passé en moins d’une seconde avec timeout borné
- Garanties démontrées : état corrompu ou version inconnue jamais appliqué au
  StateStore vivant, erreur de quarantine conservée, erreurs de write/rename/
  sync observables, nettoyage des temporaires atomiques et état de santé de
  persistance récupérable après succès
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution` puis `integration/synora-v1`
- Limites : qualification matérielle non exécutée sur cette machine ; la
  stabilité de la passe Go multi-packages parallèle reste à confirmer au gate
  du groupe 06–10

## Prochain jalon

Jalon 08 — Discovery et uploads résilients.

## Historique

Ce fichier est mis à jour après chaque jalon et après chaque gate de groupe.
