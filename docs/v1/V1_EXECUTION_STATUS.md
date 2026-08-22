# Synora V1 — état d’exécution

## Jalon courant

- Jalon : 20 — OTA caméra et récupération
- Groupe : 16–20
- État : validé sur branche dédiée ; intégration au checkpoint 16–20 en cours
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

## Jalon 08

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j08-discovery-uploads`
- Branche dédiée : `codex/v1-j08-discovery-uploads`
- Commit : `75ba18b6a8e654ef8f521b08002343ae1465476a`
- Validations ciblées : `go test ./internal/discovery/... ./internal/clipstore
  -count=1`, vet ciblé et race ciblé — PASS
- Garanties démontrées : rejet des payloads vides et interrompus, nettoyage
  des temporaires, limites de taille, vérification finale taille/checksum,
  synchronisation du répertoire après rename et suppression des nouveaux
  fichiers si la publication Core échoue ; idempotence et collisions
  existantes conservées
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution` puis `integration/synora-v1`
- Limites : qualification matérielle non exécutée sur cette machine

## Jalon 09

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j09-vision-resilience`
- Branche dédiée : `codex/v1-j09-vision-resilience`
- Commit : `6ef33054c933fbf04a79bbd123338414af4fad37`
- Validations ciblées : `go test ./internal/discovery/... -count=1`, vet ciblé
  et race ciblé — PASS
- Garanties démontrées : arrêt du worker borné même après échec de `Kill`,
  état non-running après timeout, reprise avec backoff expiré nettoyé, pool
  normalisé à au moins un worker, validation des jobs, fermeture idempotente,
  rejet des jobs après fermeture et attente des goroutines
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution` puis `integration/synora-v1`
- Limites : qualification matérielle non exécutée sur cette machine

## Jalon 10

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j10-failure-matrix`
- Branche dédiée : `codex/v1-j10-failure-matrix`
- Commit : `3daa5e6f0f2d08b559cda92b51d9ea8f72636275`
- Validations ciblées : `go test ./internal/qualification/failurematrix
  -count=1`, vet ciblé et race ciblé — PASS
- Harness : cinq composants Core/bus/Discovery/Vision/StateStore, quatre
  points de coupure déterministes, charge 128, replay à identité stable,
  pertes maximales explicites et mesure du temps de reprise local
- Campagne : 5 scénarios × 100 itérations, soit 500 exécutions — PASS
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution`; intégration finale dans
  `integration/synora-v1` après ce compte rendu
- Limites : le harness est logiciel et déterministe ; aucune coupure
  électrique, qualification matérielle Rock 5/Zero 3W, panne radio ou test
  WireGuard réel n’est démontré ici

## Jalon 11

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j11-threat-model`
- Branche dédiée : `codex/v1-j11-threat-model`
- Commit : `28670be80aeb99c0ce670ec41f1fc96986fba7c0`
- Validations ciblées : `go test ./internal/security -count=1`, `go vet
  ./internal/security` et `go test -race ./internal/security -count=1` — PASS
- Garanties démontrées : registre public durable d’identités centrale/caméra,
  clés privées absentes du registre, permissions `0600`, refus des symlinks et
  formats corrompus, générations explicites, rotation, révocation,
  remplacement et vérification Ed25519 limitée à l’identité active
- Documentation : `docs/v1/V1_THREAT_MODEL.md` inventorie actifs, frontières,
  attaquants, bootstrap, contrôles et résidus de risque
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution` puis `integration/synora-v1`
- Limites : l’action physique de bootstrap et le pairing caméra seront
  implémentés et qualifiés au J12 ; aucune attestation matérielle

## Jalon 12

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j12-camera-pairing`
- Branche dédiée : `codex/v1-j12-camera-pairing`
- Commit : `8eeb8a2d8ff60187799a4707fc2d7d06b9c4f878`
- Validations ciblées : `go test ./internal/security
  ./internal/discovery/network ./cmd/synora-api -count=1`, vet ciblé et race
  ciblé — PASS
- Garanties démontrées : fenêtre de pairing active et bornée, clé publique
  caméra obligatoire en production, preuve Ed25519 horodatée, MAC observée,
  claim à usage unique, rejet replay/ancienne preuve, enregistrement de
  l’identité au moment de la confirmation et redaction des secrets conservée
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution` puis `integration/synora-v1`
- Limites : la chaîne de confiance matérielle du bouton physique et la
  qualification radio restent à démontrer au niveau hardware

## Jalon 13

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j13-api-security`
- Branche dédiée : `codex/v1-j13-api-security`
- Commits : `f04ace4` (origines, CSRF, headers, redaction) et `c2068ba`
  (rate limiting)
- Validations ciblées après intégration : `go test ./cmd/synora-api
  ./internal/api -count=1`, `go vet ./cmd/synora-api ./internal/api` et
  `go test -race ./cmd/synora-api ./internal/api -count=1` — PASS
- Garanties démontrées : sessions locales expirables et permissions par
  endpoint conservées, wildcard CORS sans credentials, prévols interdits
  refusés, mutations par cookie protégées contre les origines externes,
  en-têtes anti-clickjacking/anti-sniffing/anti-referrer, CSP, chemins
  filesystem absents des réponses web, limites JSON/upload conservées et
  rate limiting API borné par client
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution`; intégration finale dans
  `integration/synora-v1` après ce compte rendu
- Limites : le rate limiter est local au processus et ne constitue pas une
  quota distribué ; la qualification navigateur réelle reste à exécuter

## Jalon 14

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j14-connectivity`
- Branche dédiée : `codex/v1-j14-connectivity`
- Commits : `febd9c9` (registre d’accès signé) et `b7aaa03` (architecture)
- Validations ciblées après intégration : `go test ./internal/connectivity
  ./internal/security ./internal/discovery/network ./cmd/synora-api
  -count=1`, vet ciblé et race ciblé — PASS
- Garanties démontrées : frontière WireGuard directe/relais sans broker de
  données privées, rendez-vous limité aux métadonnées publiques et expirées,
  registre atomique `0600`, autorisations signées par génération, rotation,
  révocation immédiate, transfert de propriété et factory reset d’accès
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution`; intégration finale dans
  `integration/synora-v1` après ce compte rendu
- Limites : l’adaptateur WireGuard/netlink réel, le débit, la radio et la
  qualification Rock 5/Zero 3W restent hors validation locale

## Jalon 15

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j15-final-audit`
- Branche dédiée : `codex/v1-j15-final-audit`
- Commit : `5b674f2`
- Garanties démontrées : fuzz targets sur les parseurs connectivité/réseau,
  registre d’identités et preuves de pairing, redaction des tokens/cookies,
  clés, biométrie et chemins pour les extraits de support, permissions des
  artefacts sensibles vérifiées par les suites existantes
- Fuzzing ciblé : quatre campagnes de 3 secondes — PASS sans crash ni corpus
  défaillant
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution`; gate global groupe 11–15 exécuté avant
  le commit terminal de ce statut
- Limites : aucun service externe d’audit dépendances n’est configuré dans
  l’environnement ; l’audit est donc limité au graphe Go et aux outils locaux

## Jalon 16

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j16-retention`
- Branche dédiée : `codex/v1-j16-retention`
- Commits : `ef68ef3` (politique/store/réserve) et `7b0d823`
  (expiration outbox), `65923d9` (documentation)
- Validations ciblées après intégration : `go test ./internal/retention
  ./internal/state ./internal/event ./internal/outbox ./internal/dispatcher
  ./internal/discovery/ingress ./cmd/synora-core -count=1`, vet ciblé et race
  ciblé — PASS
- Garanties démontrées : inventaire et limites clips/incidents/événements/logs/
  outbox/temporaires, sélection stable UTC, réserve disque minimale de 512 MiB,
  protection des incidents actifs et événements référencés, détachement
  référentiel clip/incident, TTL des événements et purge terminale outbox sans
  suppression des livraisons pending/retry/in-flight
- Documentation : `docs/v1/V1_RETENTION_POLICY.md`
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution`; intégration finale dans
  `integration/synora-v1` après ce compte rendu
- Limites : les logs externes restent sous la responsabilité du journal système
  de l’image déployée ; aucun fichier de log global n’est possédé par Synora

## Jalon 17

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j17-sensitive-rights`
- Branche dédiée : `codex/v1-j17-sensitive-rights`
- Commit : `1a0b3e2`
- Validations ciblées : `go test` ciblé, `go vet` ciblé et `go test -race` ciblé
  — PASS
- Garanties démontrées : suppression physique sûre des sources et photos legacy
  d’un résident, refus des symlinks, export administrateur limité aux métadonnées,
  durcissement des répertoires biométriques, purge immédiate des anciennes
  versions de dataset après retrait, et nettoyage des artefacts biométriques/
  secrets/chemins dans les bundles de support
- Documentation : `docs/v1/V1_PRIVACY_LOCAL_FIRST.md`
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution`; intégration finale dans
  `integration/synora-v1` après ce compte rendu
- Limites : la propriété effective des fichiers reste celle de l’installateur
  du service ; la qualification d’un export matériel n’est pas exécutée ici

## Jalon 18

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j18-backup-restore`
- Branche dédiée : `codex/v1-j18-backup-restore`
- Commit : `174e5ca`
- Validations ciblées : `go test ./internal/backup ./internal/state
  ./cmd/synora-backup -count=1` et vet ciblé — PASS
- Garanties démontrées : snapshot cohérent par staging et rename, manifeste
  checksumé, restauration incidents/clips/présence/métadonnées, rejet d’un
  snapshot altéré, faible espace disque, interruption avant commit et
  expiration reprenable via répertoire `.delete`
- CLI : `cmd/synora-backup` pour `create`, `restore` et `expire`
- Documentation : `docs/v1/V1_BACKUP_POLICY.md`
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution`; intégration finale dans
  `integration/synora-v1` après ce compte rendu
- Limites : aucun coffre Synora+ ni service cloud n’est requis ou appelé ; la
  sauvegarde couvre l’état persistant et les fichiers explicitement listés

## Jalon 19

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j19-central-ota`
- Branche dédiée : `codex/v1-j19-central-ota`
- Commit : `bff4890`
- Validations ciblées : `go test ./internal/ota ./cmd/synora-ota
  ./internal/migrations -count=1` et vet ciblé — PASS
- Garanties démontrées : manifeste Ed25519, checksum streaming, compatibilité
  matériel/Core, journal de transaction atomique, bascule RAUC, healthcheck
  readonly avant `mark-good`, `mark-bad` automatique et récupération après
  interruption de processus
- Migration : les plans versionnés existants sont inclus dans la procédure et
  restent séparés du chemin décisionnel de Core
- Documentation : `docs/v1/V1_OTA_CENTRAL_POLICY.md`
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution`; intégration finale dans
  `integration/synora-v1` après ce compte rendu
- Limites : la signature matérielle RAUC et le redémarrage réel ne sont pas
  exécutés sur cette machine ; ils restent sous l’autorité de la plateforme

## Jalon 20

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j20-camera-recovery`
- Branche dédiée : `codex/v1-j20-camera-recovery`
- Commit : `49b5f8e`
- Validations ciblées : `go test ./internal/cameraota
  ./cmd/synora-camera-ota -count=1`, vet ciblé et race ciblé — PASS
- Garanties démontrées : manifeste signé/checksumé Zero 3W, compatibilité
  bootloader, mise en attente offline, journal de phases install/reboot/
  validation, rollback automatique, reprise après interruption et image de
  récupération écrite atomiquement
- Outils opérateur : `synora-camera-ota doctor|version|explain`
- Documentation : `docs/v1/V1_CAMERA_OTA_POLICY.md`
- État : branche dédiée poussée, fast-forward dans
  `integration/synora-v1-execution`; gate groupe 16–20 à exécuter avant
  intégration finale dans `integration/synora-v1`
- Limites : le transport radio et le redémarrage physique Zero 3W restent
  simulés localement

## Gate groupe 11–15

- `git diff --check` — PASS
- `go list ./...` — échec reproductible du VCS Go de l’environnement
  (répertoire parent `.git` vide/non-repository) ; `GOFLAGS=-buildvcs=false
  go list ./...` — PASS, 119 packages
- `go test ./... -count=1` — PASS
- `go test ./... -shuffle=on -count=3` — PASS
- `go vet ./...` — PASS
- `timeout 300s go test -race ./... -count=1` — PASS, exit 0
- `python3 -B -m unittest discover -s services/vision-worker/tests -v` —
  PASS (21 tests)
- Worktree d’exécution — propre avant ce commit de statut

## Gate groupe 06–10

- `git diff --check` — PASS
- `go list ./...` — bloqué par le défaut VCS Go de l’environnement (répertoire
  parent `.git` vide/non-repository) ; `GOFLAGS=-buildvcs=false go list ./...`
  — PASS, 119 packages
- `go test ./... -count=1` — PASS
- `go test ./... -shuffle=on -count=3` — PASS
- `go vet ./...` — PASS
- `timeout 300s go test -race ./... -count=1` — première passe avec un échec
  fonctionnel isolé sous contention dans
  `internal/cge/shadowworkflow/TestCognitiveSituationRebuiltAfterRecovery`,
  sans data race ; le test a passé trois fois isolément sous race, puis la
  commande globale exacte a terminé `race_exit=0`
- `python3 -B -m unittest discover -s services/vision-worker/tests -v` —
  PASS (21 tests)
- Worktree d’exécution — propre avant mise à jour de ce statut
- Limites : aucune qualification matérielle ou réglementaire exécutée sur
  cette machine

## Checkpoint groupe 06–10

- Tag annoté : `v1-checkpoint-10`
- Le tag sera poussé avec le commit terminal de ce statut, puis fast-forwardé
  dans `integration/synora-v1`.

## Checkpoint groupe 11–15

- Tag annoté : `v1-checkpoint-15`
- Le tag sera créé et poussé avec le commit terminal de ce statut, puis
  fast-forwardé dans `integration/synora-v1`.

## Prochain jalon

Jalon 21 — prochain groupe V1 à définir après validation du checkpoint 20.

## Historique

Ce fichier est mis à jour après chaque jalon et après chaque gate de groupe.
