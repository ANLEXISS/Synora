# Synora V1 — état d’exécution

## MASTER_PLAN — M001

- Jalon : M001 — baseline compilable et inventaire V1
- Worktree dédié : `/home/rock/Synora-worktrees/v1-m001-baseline`
- Branche dédiée : `codex/v1-m001-baseline`
- Commits : `7c1f74e` (formatage Go), `2e5fb9a` (inventaire)
- Résultat : validé ; aucune modification fonctionnelle volontaire
- Preuves : `git diff --check`, `go test ./...`, `go test -race ./...`,
  `go vet ./...`, `go build ./cmd/...`, compile/tests Vision et build WebApp
- Réserves d’environnement : les commandes Go nécessitent
  `GOFLAGS=-buildvcs=false` à cause du `.git` parent non exploitable ; le
  répertoire `automation/codex-loop/tests` est absent et le package WebApp ne
  définit pas de script npm `test`
- Intégration : à effectuer après validation de ce rapport

## MASTER_PLAN — M002

- Jalon : M002 — harnais de tests hermétique
- Worktree dédié : `/home/rock/Synora-worktrees/v1-m002-test-harness`
- Branche dédiée : `codex/v1-m002-test-harness`
- Commits : `933315b` (helpers communs), `49976d8` (attente d’état terminal
  dans le test de simulation)
- Résultat : validé ; horloge, IDs, chemins temporaires, bus mémoire, stockage
  temporaire, serveur HTTP et faux Vision sont couverts par des oracles
  déterministes et sans dépendance système
- Preuves : `GOFLAGS=-buildvcs=false go test ./...`,
  `GOFLAGS=-buildvcs=false go test -race ./...`, vet/build Go, 22 tests Vision,
  compile Python, lint et build WebApp — PASS
- Régression corrigée : le test API attend désormais explicitement l’état
  terminal de la simulation après l’envoi asynchrone
- Réserves : lint WebApp conserve 7 avertissements React-hooks existants ; le
  package ne définit toujours pas de script npm `test`
- Intégration : à effectuer après validation de ce rapport

## MASTER_PLAN — M003

- Jalon : M003 — configuration et chemins injectables
- Worktree dédié : `/home/rock/Synora-worktrees/v1-m003-config`
- Branche dédiée : `codex/v1-m003-config`
- Commits : `1f24287` (contrat et câblage runtime), `8a2514e` (oracle E2E
  tolérant à l’instrumentation)
- Résultat : validé ; chemins, ports, endpoints MediaMTX et timeouts sont
  centralisés, surchargeables sans mutation de l’environnement de test, et
  validés avant démarrage
- Composants câblés : Core, API, Bus, Discovery/Vision, Actions, Backup, OTA,
  Camera OTA, Runtime Manager et Connect
- Preuves : `GOFLAGS=-buildvcs=false go test ./...`,
  `GOFLAGS=-buildvcs=false go test -race ./...`, vet/build Go, compile/tests
  Vision, lint/build WebApp et `git diff --check` — PASS
- Régressions de baseline durcies : simulation API et E2E Core/CGE attendent
  désormais leurs états terminaux avec des délais bornés adaptés à `-race`
- Réserves : `GOFLAGS=-buildvcs=false` reste requis par l’environnement de
  worktree ; lint WebApp conserve 7 avertissements React-hooks ; aucun script
  npm `test` ni répertoire `automation/codex-loop/tests` n’est présent
- Intégration : à effectuer après validation de ce rapport

## MASTER_PLAN — M004

- Jalon : M004 — contrats V1 canoniques et compatibles
- Worktree dédié : `/home/rock/Synora-worktrees/v1-m004-contracts`
- Branche dédiée : `codex/v1-m004-contracts`
- Décision humaine validée : le package canonique est `pkg/contract`, les
  timestamps restent RFC3339/RFC3339Nano et aucune migration cassante vers Unix
  ms n’est autorisée
- Modifications : validations V1 non destructives pour les contrats partagés,
  `FaceDatasetVersion` pour la frontière Discovery/Vision, adaptateur depuis le
  manifeste interne et fixtures JSON courantes/legacy
- Types internes conservés : `Track`, `Presence` et `ObservationRef` ne
  traversent pas une frontière interservice V1 et ne sont pas promus
- Preuves : `GOFLAGS=-buildvcs=false go test ./... -p 1 -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`,
  `GOFLAGS=-buildvcs=false go build ./cmd/...`,
  `GOFLAGS=-buildvcs=false go test -race ./... -count=1`,
  `GOFLAGS=-buildvcs=false go test -race ./pkg/contract
  ./internal/facedataset ./internal/discovery/vision -count=1`, et les 22
  tests Python Vision — PASS ; `git diff --check` — PASS
- Diagnostic de suite : un premier lancement normal parallèle a exposé une
  flakiness de `internal/delivery`, reproduite isolément 5/5 et non reproduite
  ensuite ; un lancement race séquentiel a dépassé 300 s dans les tests CGE
  lourds, puis la même suite race parallèle a terminé entièrement avec PASS.
- Validation complète et intégration : validé ; commit `e00a570` atteignable
  depuis `integration/synora-v1`

## MASTER_PLAN — M005

- Jalon : M005 — bus robuste sous concurrence
- Worktree dédié : `/home/rock/Synora-worktrees/v1-m005-bus`
- Branche dédiée : `codex/v1-m005-bus`
- Commits : `5b9e05d` (framing/concurrence/déconnexion), `017e3e2` (ACK de
  registration), `17c72cb` (oracle Vision déterministe)
- Modifications : frames JSON bornées à 1 MiB côté serveur et client,
  écriture complète sous writer sérialisé, fermeture propre des pending RPC
  avec erreur de déconnexion, registration acquittée et suppression de la
  course sur `lastSeen`
- Tests dédiés : fragmentation/concaténation, frame trop grande, 32 écritures
  concurrentes, RPC pending à la fermeture, reconnexion après restart et
  Delivery/Dispatcher/CoreClient répétés — PASS, y compris sous race
- Preuves : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go test -race ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`,
  `GOFLAGS=-buildvcs=false go build ./cmd/...`, 22 tests Python Vision et
  `git diff --check` — PASS
- Diagnostics réparés pendant la qualification : l’ACK de registration
  supprime une perte de première livraison sous concurrence ; l’oracle
  `worker.crashed` attend désormais sa publication après la transition
  `backoff`. Aucun test n’a été supprimé ni affaibli.
- Validation complète et intégration : validé ; commit `22a9132` atteignable
  depuis `integration/synora-v1`

## MASTER_PLAN — M006

- Jalon : M006 — identité, ACL et anti-rejeu du Bus
- Worktree dédié : `/home/rock/Synora-worktrees/v1-m006-bus-identity`
- Branche dédiée : `codex/v1-m006-bus-identity`
- Décisions conservées : l’identité d’un producteur ne vient jamais du champ
  `Source` seul ; l’horodatage reste RFC3339/RFC3339Nano ; la protection
  cryptographique est additive et ne change pas la sémantique des messages
  existants
- Modifications : contrôle `SO_PEERCRED` complété par l’exécutable attendu,
  ACL explicite producteur/type/cible, nonce HMAC avec identifiant de clé et
  fenêtre temporelle lorsqu’un secret est provisionné, anti-rejeu par
  identifiant et empreinte pour les messages privilégiés sans secret, et
  rejet fail-closed des clés tournées ou payloads modifiés
- Compatibilité : `NewServer`/`NewClient` conservent leur API ; la clé du Bus
  est optionnelle et provisionnée hors dépôt via `SYNORA_BUS_KEY_ID` et
  `SYNORA_BUS_SECRET_FILE` (ou le fallback local `SYNORA_BUS_SECRET`)
- Tests déterministes : spoof de source, mauvaise cible, type non autorisé,
  timestamp expiré, rejeu de nonce, collision d’identifiant avec payload
  modifié, rotation de clé et sérialisation JSON des métadonnées d’authentification.
- Preuves finales : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`, `GOFLAGS=-buildvcs=false go build
  ./cmd/...`, `GOFLAGS=-buildvcs=false go test -race ./... -count=1`, et les
  22 tests Python Vision — PASS. La WebApp n’a pas pu être relancée dans ce
  worktree : ses dépendances locales `oxlint`/`tsc` sont absentes ; la
  qualification lint/build WebApp de M005 reste PASS et intégrée.
- État : implémentation en cours de validation sur branche dédiée

## MASTER_PLAN — M007

- Jalon : M007 — StateStore unique et snapshots défensifs
- Worktree dédié : `/home/rock/Synora-worktrees/v1-m007-state-store`
- Branche dédiée : `codex/v1-m007-state-store`
- Modifications : les valeurs mutables de `SystemState` sont clonées à
  l’entrée et à la sortie, les maps/slices imbriquées des payloads sont copiées
  récursivement, et une restauration persistée ne conserve plus d’alias vers
  l’entrée appelante
- Tests dédiés : aliasing entrée/sortie de `SystemState`, payload événementiel
  imbriqué, ownership après restauration, écritures/lectures concurrentes,
  plus les tests existants de snapshots, révisions et invariants référentiels
- Preuves : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go test -race ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`,
  `GOFLAGS=-buildvcs=false go build ./cmd/...` et `git diff --check` — PASS.
  Les 22 tests Python Vision et la qualification WebApp précédemment validée
  restent inchangés par ce jalon.
- État : validé sur branche dédiée ; intégration à effectuer

## MASTER_PLAN — M008

- Jalon : M008 — persistance atomique et migrations
- Worktree dédié : `/home/rock/Synora-worktrees/v1-m008-persistence`
- Branche dédiée : `codex/v1-m008-persistence`
- Modifications : les écritures d’état passent par un fichier temporaire du
  même filesystem, écriture complète, fsync, rename puis fsync du répertoire.
  Une seule copie `.bak` conserve le dernier état valide avant remplacement ;
  une copie courante tronquée ou dont le checksum SHA-256 est faux est
  quarantainée puis restaurée depuis cette copie. Les fichiers sans checksum
  restent acceptés pour la migration rétrocompatible des versions précédentes.
- Migration : la version 1 reste migrable vers la version 2 ; une version
  future est refusée sans écrasement silencieux. Le checksum est additif et
  ne change aucune sémantique de contrat ou de timestamp existante.
- Tests dédiés : récupération sur fichier tronqué/checksum faux, copie de
  récupération bornée, absence et migration de fichier, refus de version
  future, et simulation déterministe d’une coupure à chaque étape de l’écriture
  atomique sans perte du dernier état valide.
- Preuves : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go test -race ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`, `GOFLAGS=-buildvcs=false go build
  ./cmd/...`, 22 tests Python Vision et `git diff --check` — PASS. La
  qualification WebApp précédemment validée reste inchangée, ses dépendances
  locales n’étant pas présentes dans ce worktree.
- Commits : `29fb88f` (implémentation et tests), commit de statut ci-dessous
- État : validé sur branche dédiée ; intégration à effectuer

## Jalon courant

- Historique conservé : jalons 01–25 de l’ancien plan V1 (non substitutif au
  `MASTER_PLAN.json` maître)
- Groupe : 21–25
- État : validé sur branche dédiée ; intégration dans le groupe 21–25 en cours
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

## Gate groupe 16–20

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
- Worktree d’exécution — propre avant le commit terminal du checkpoint
- Qualification fonctionnelle matérielle — non exécutée ici ; les scénarios
  backup, OTA centrale et OTA caméra simulés sont verts

## Jalon 21

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j21-hardware-harness`
- Branche dédiée : `codex/v1-j21-hardware-harness`
- Commits : `7c13313`, correctif `bf801d7`
- Validations ciblées : `python3 -B -m unittest
  tools.tests.test_v1_hardware_qualification -v`, `python3 -B
  tools/v1_hardware_qualification.py doctor` et `git diff --check` — PASS
- Livrables : référence BOM versionnée sans inférence, protocole reproductible,
  collecte thermique/charge/stockage/réseau/écritures SSD, soak déterministe,
  journal de test de coupure assistée et rapport borné
- Garantie : chaque observation est marquée `fixture` ou `host_observation`;
  aucune mesure locale ne devient une qualification physique
- Blocage externe explicite : unité cible, BOM complète et résultats de coupure
  réelle non disponibles; statut conservé à
  `blocked_no_target_confirmation`, sans seuil matériel inventé
- État : branche poussée, fast-forward dans `integration/synora-v1-execution`

## Jalon 22

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j22-camera-network`
- Branche dédiée : `codex/v1-j22-camera-network`
- Commit : `4d24232`
- Validations ciblées : `python3 -B -m unittest
  tools.tests.test_v1_camera_network_qualification -v`, génération et relecture
  JSON du rapport, et `git diff --check` — PASS
- Couverture : trois caméras (`cam_01`, `cam_02`, `cam_03`), jour/nuit, IR-cut,
  envoi/pertes/latences, reconnexion SynoraNet et écritures atomiques privées
- Décisions explicites : PIR et Doppler non attachés; microphone caméra
  désactivé pour V1; chute, arme et tamper désactivés en attente de
  qualification; aucun signal générique n’est promu en capacité produit
- Limites : le fixture est synthétique et le rapport reste
  `fixture_observed_physical_qualification_blocked`; aucune qualification
  réelle de caméra, radio, capteur ou audio n’est revendiquée
- État : branche poussée, intégrée dans `integration/synora-v1-execution`

## Jalon 23

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j23-user-flow`
- Branche dédiée : `codex/v1-j23-user-flow`
- Commits : `51099b8` (santé/version web) et `4fa6530` (qualification du
  parcours)
- Validations ciblées : tests du manifeste, `npm run build`, `npm run lint`,
  `npm audit --omit=dev --audit-level=high` et `git diff --check` — PASS
- Couverture : onboarding/pairing, résidents/photos, incidents/clips,
  acquittement/résolution, live à la demande, santé/stockage/version/erreurs,
  responsive et parcours local d’un nouvel utilisateur
- Correctif release : `GET /api/system/version` est maintenant consommé dans
  Settings; le lockfile web est passé à `react-router`/`react-router-dom`
  `7.18.2`; audit production : zéro vulnérabilité
- Limites explicites : le contrôle est statique et le test navigateur réel
  reste à exécuter; accès distant `blocked_external_adapter` tant que
  WireGuard/netlink et le client distant ne sont pas disponibles
- État : branche poussée, intégrée dans `integration/synora-v1-execution`

## Jalon 24

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j24-release-engineering`
- Branche dédiée : `codex/v1-j24-release-engineering`
- Commits : `ff32960` (outillage) et `65f0ea4` (dossier documentaire)
- Validations ciblées : tests Python release engineering, tests/vet/race Go sur
  `internal/security`, `internal/version` et `cmd/synora-boot-healthcheck`,
  génération/relecture JSON et `git diff --check` — PASS
- Livrables : manifeste source reproductible sans timestamp, inventaire SBOM
  et licences avec limites déclarées, provisioning read-only, burn-in,
  récupération, diagnostics support expurgés et matrice CE/RED/RoHS/WEEE
- Limites explicites : aucune image cible n’est produite ici, aucune clé privée
  de signature n’est présente, les licences Go restent à résoudre par la
  chaîne d’approvisionnement et les éléments réglementaires restent
  `external_evidence_required`
- État : branche poussée, intégrée dans `integration/synora-v1-execution`

## Jalon 25

- Worktree dédié : `/home/rock/Synora-worktrees/v1-j25-rc-audit`
- Branche dédiée : `codex/v1-j25-rc-audit`
- Commit : `fa2e052`
- Validations ciblées : audit RC Python et relecture JSON, plus les harnesses
  J21–J24 — PASS
- Livrables : changelog V1, audit final, inventaire P0/P1, décision de RC,
  rappel des limites Vision FP/FN, restauration/OTA/rollback et sécurité
- État local : `software_rc_audited_external_gates_open`; aucun P0 ouvert dans
  l’audit du dépôt
- Décision honnête : une candidate locale peut être taguée, mais `v1-rc1`
  reste bloqué jusqu’aux preuves externes obligatoires
- Gates externes ouvertes : unité/BOM et mesures physiques, WireGuard/netlink
  et client distant, calibration Vision réelle et preuves CE/RED/RoHS/WEEE
- État : branche poussée, intégrée dans `integration/synora-v1-execution`;
  gate global final restant à exécuter

## Gate final groupe 21–25

- `GOFLAGS=-buildvcs=false go list ./...` — PASS
- `GOFLAGS=-buildvcs=false go test ./... -count=1` — PASS
- `GOFLAGS=-buildvcs=false go test ./... -shuffle=on -count=3` — PASS
- `GOFLAGS=-buildvcs=false go vet ./...` — PASS
- `GOFLAGS=-buildvcs=false timeout 300s go test -race ./... -count=1` — PASS
- `python3 -B -m unittest discover -s services/vision-worker/tests -v` — PASS
  (21 tests)
- Harnesses J21–J25 — PASS (13 tests ciblés)
- `npm --prefix synora-web audit --omit=dev --audit-level=high` — PASS,
  zéro vulnérabilité
- `npm --prefix synora-web run build` — PASS
- `npm --prefix synora-web run lint` — PASS avec 7 avertissements React
  `exhaustive-deps` préexistants, sans erreur
- `git diff --check` — PASS; worktree d’exécution propre
- P0 ouvert — aucun dans l’audit local
- Limites bloquantes : aucune mesure hardware/radio réelle, aucun adaptateur
  distant de production, aucune calibration Vision FP/FN réelle et aucune
  preuve externe CE/RED/RoHS/WEEE
- Décision : candidate locale autorisée sous le statut
  `software_rc_audited_external_gates_open`; `v1-rc1` reste bloqué

## Checkpoint groupe 16–20

- Tag annoté à créer et pousser : `v1-checkpoint-20`
- Le tag pointera sur le commit terminal de ce statut, avant fast-forward dans
  `integration/synora-v1`

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

Gate final groupe 21–25 puis candidate locale — aucun jalon V1 supplémentaire.

## Historique

Ce fichier est mis à jour après chaque jalon et après chaque gate de groupe.
