# Synora V1 — état d’exécution

## MASTER_PLAN — M030

- Jalon : M030 — Frontière HTTPS et WebSocket
- Worktree dédié : `/home/rock/Synora-worktrees/v1-m030-https-ws`
- Branche dédiée : `codex/v1-m030-https-ws`
- HTTPS : lorsque TLS est activé et valide, HTTP devient un listener de
  redirection et HTTPS sert le handler applicatif ; les certificats/clé
  absents ou non réguliers refusent le démarrage.
- Proxy : aucun `X-Forwarded-*` n’est accepté sans frontière de confiance
  explicitement configurée ; le transport HTTPS est déterminé par TLS natif.
- WebSocket : authentification avant upgrade, Origin contrôlée, limite de
  message 1 MiB, file d’envoi bornée, délais, ping/pong, fermeture explicite
  des clients lents et reprise/resynchronisation par epoch/révision.
  Les sessions cookie sont revalidées périodiquement et ferment la connexion
  après expiration ou révocation.
- Couverture : séparation HTTP/HTTPS, transport forwarded non fiable, Origin,
  authentification, limite et backpressure WebSocket, ping/pong, reprise,
  session révoquée et absence de snapshot avant authentification.
- Validation : à compléter avec Go complet, vet, build, race, Python,
  qualification fonctionnelle, `git diff --check` et vérification des deux
  worktrees avant intégration.

## MASTER_PLAN — M029

- Jalon : M029 — Administrateurs, sessions et rôles
- Worktree dédié : `/home/rock/Synora-worktrees/v1-m029-auth`
- Branche dédiée : `codex/v1-m029-auth`
- Périmètre : bootstrap applicatif à usage unique, authentification locale,
  comptes persistés dans `auth.yaml`, changement de mot de passe, RBAC et
  gestion administrateur.
- Sécurité : identifiants de session aléatoires et hashés au repos, rotation,
  expiration/révocation, cookie `HttpOnly`/`SameSite=Strict`/`Secure` sous
  HTTPS, contrôle d’origine pour les mutations cookie, réponses sans hash ni
  détail sensible.
- Invariants : `residents.yaml` ne contient aucun secret d’authentification ;
  le dernier administrateur activé ne peut pas être supprimé, désactivé ou
  rétrogradé ; les modifications de comptes sont atomiques et le stockage est
  protégé (`0640`, répertoire `0700`).
- Couverture : bootstrap/rejeu, mauvais mot de passe et non-énumération,
  fixation/rotation, expiration, révocation, CSRF par origine, élévation de
  rôle, changement de mot de passe, dernier administrateur, permissions,
  rechargement après redémarrage et JSON inconnu.
- Validation : à compléter avec Go complet, vet, build, race, Python,
  qualification fonctionnelle, `git diff --check` et vérification des deux
  worktrees avant intégration.

## MASTER_PLAN — M028

- Jalon : M028 — API REST v1 cohérente
- Worktree dédié : `/home/rock/Synora-worktrees/v1-m028-api`
- Branche dédiée : `codex/v1-m028-api`
- Périmètre : ajout non cassant de `/api/v1` par adaptation des handlers
  existants ; les chemins `/api` gardent leurs contrats historiques.
- Contrat : enveloppe JSON `data/error/meta`, ETag déterministe, `304` via
  `If-None-Match`, précondition `If-Match` pour les mutations de ressources,
  pagination stable par curseur bornée de 1 à 100 et document
  `GET /api/v1/openapi.json`.
- Sécurité : permissions et exception de claim sont évaluées sur le chemin
  canonique ; les erreurs internes et corps backend non JSON ne divulguent pas
  leur contenu.
- Couverture : méthodes et statuts, Content-Type, limite de body et JSON
  stricts des handlers propriétaires, identifiants invalides, pagination,
  filtres conservés, ETag/conditionnel, concurrence optimiste, document
  OpenAPI et absence de détail sensible.
- Validation : à compléter avec la suite Go complète, vet, build, race,
  Python, qualification fonctionnelle et vérification des deux worktrees
  avant intégration.

## MASTER_PLAN — M027

- Jalon : M027 — Pairing par clé imprimée
- Worktree dédié : `/home/rock/Synora-worktrees/v1-m027-pairing`
- Branche dédiée : `codex/v1-m027-pairing`
- Commit fonctionnel : `feae3d7` (pairing sécurisé et autorisation caméra)
- Résultat : le pairing Synora Camera exige une preuve Ed25519 liée à la clé
  imprimée, l’identité publique, le MAC et un timestamp borné. Les sessions
  sont persistées sans secret en clair, les claims sont à usage unique et
  limités après cinq échecs par caméra/origine.
- Autorisation : Discovery exige une caméra active, trusted, `network_trust:
  paired`, un secret de transport dérivé et une identité persistante active.
  Les flux non autorisés ne renvoient aucune URL live ; la révocation ferme
  immédiatement les clips et le live. Le reset explicite révoque d’abord puis
  réassocie une nouvelle clé avec une génération augmentée.
- Compatibilité : les tests et interfaces de pairing existants sont conservés
  lorsqu’ils restent dans le périmètre ; le claim préalable à la confirmation
  est désormais obligatoire pour une caméra autorisée.
- Couverture : première association, clé/preuve fausse, rejeu, reprise après
  redémarrage, limitation bornée, concurrence de claim, révocation, reset,
  rotation de génération et masquage des URL non autorisées.
- Preuves : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`, `GOFLAGS=-buildvcs=false go build
  ./cmd/...`, tests ciblés de sécurité/pairing et `git diff --check` — PASS.
- Réserve d’environnement : les dépendances Web (`synora-web/node_modules`)
  restent absentes et hors périmètre ; lint/build Web non exécutés.
- Intégration : à effectuer après race, Python et qualification fonctionnelle.

## MASTER_PLAN — M026

- Jalon : M026 — Supervision des flux MediaMTX
- Worktree dédié : `/home/rock/Synora-worktrees/v1-m026-mediamtx`
- Branche dédiée : `codex/v1-m026-mediamtx`
- Commits fonctionnels : `26f8740` (client, allowlist et réconciliation),
  `4ccd49d` (configuration runtime et sonde HTTP), `e291eb3` (statut API),
  `b131793` (supervision périodique Discovery)
- Modifications : MediaMTX démarre avec un ensemble de chemins explicite,
  Discovery réconcilie les caméras activées par créations/suppressions
  idempotentes, et les changements de caméra sont reflétés sans wildcard.
  L’API `/api/streams` expose `ready`, `degraded` ou `unknown` sans publier de
  credentials ; les URLs de supervision et les erreurs n’exposent pas de secret.
- Invariants : MediaMTX est injectable dans les tests, son indisponibilité ne
  bloque pas l’ingestion ou l’analyse des clips déjà reçus, et les décisions de
  sécurité ne dépendent pas du live.
- Couverture : client factice, timeout, configuration partielle, doublons,
  caméra renommée, reprise après redémarrage et continuité d’ingestion clips.
- Preuves : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go test -race ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`,
  `GOFLAGS=-buildvcs=false go build ./cmd/...`, 35 tests Python,
  qualification fonctionnelle locale (8 tests), et `git diff --check` — PASS.
  Un échec race intermittent de Dispatcher a été reproduit isolément 5/5 sans
  reproduction, puis la suite race globale a été relancée avec succès.
- Réserve d’environnement : le worktree ne contient pas les dépendances Web
  (`synora-web/node_modules` est explicitement hors périmètre), donc lint/build
  Web non exécutés sur ce jalon.
- Intégration : à effectuer après validation de ce rapport

## MASTER_PLAN — M025

- Jalon : M025 — Discovery et registre caméras
- Worktree dédié : `/home/rock/Synora-worktrees/v1-m025-camera-registry`
- Branche dédiée : `codex/v1-m025-camera-registry`
- Commits fonctionnels : `f56f760` (contrat `CameraObservation`), `8ee76b6`
  (registre Discovery, projection Core et tests de compatibilité)
- Décisions : Discovery reste propriétaire de la découverte technique et de la
  santé locale ; Core applique une projection durable. Les observations sont
  idempotentes par identifiant stable, les capacités sont canonisées, les
  collisions d’identité matérielle sont refusées et les changements d’endpoint
  sont acceptés sans changer l’identité caméra. Les timestamps restent RFC3339.
- Compatibilité : les événements historiques `discovery.camera.online` et
  `discovery.camera.offline` sont conservés ; aucune migration de champ existant
  vers Unix ms n’est effectuée.
- Couverture : trois caméras, doublon d’observation, changement d’adresse,
  collision matérielle, flapping réseau, observation inconnue appliquée par Core,
  reprise de publication et sérialisation JSON déterministe.
- Preuves : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go test -race ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`,
  `GOFLAGS=-buildvcs=false go build ./cmd/...`, 35 tests Python,
  qualification fonctionnelle locale (8 tests), et `git diff --check` — PASS.
- Réserve d’environnement : le worktree ne contient pas les dépendances Web
  (`synora-web/node_modules` est explicitement hors périmètre), donc lint/build
  Web non exécutés sur ce jalon.
- Intégration : à effectuer après validation de ce rapport

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

## MASTER_PLAN — M009

- Jalon : M009 — ingress Core validé et idempotent
- Worktree dédié : `/home/rock/Synora-worktrees/v1-m009-ingress`
- Branche dédiée : `codex/v1-m009-ingress`
- Modifications : la frontière d’ingestion valide le contrat Event avant toute
  mutation, contrôle les payloads `action.result` et les transitions de clips,
  refuse les références clip absentes pour ces transitions, applique une
  fenêtre de timestamps configurable (futur toléré 5 minutes, passé par défaut
  24 heures), et garde les scénarios simulés explicitement historiques. Les
  IDs de transport sont laissés à la déduplication Core ; une collision d’ID
  avec un payload divergent est rejetée avant Engine, StateStore, incident ou
  publication. Les corrélations clip optionnelles des événements vision restent
  compatibles lorsqu’un clip n’est pas encore disponible.
- Compatibilité : les messages legacy sans ID continuent d’être régulés par le
  RateController ; les retries identifiés traversent la frontière pour que Core
  distingue retry et collision. Le journal récent restauré sert de barrière de
  rejeu après redémarrage.
- Tests dédiés : payload poison sans mutation, référence clip absente, timestamps
  futur/ancien avec horloge déterministe, simulation historique explicite,
  collision d’ID, retry après redémarrage et ordre de traitement existant.
- Preuves : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go test -race ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`, `GOFLAGS=-buildvcs=false go build
  ./cmd/...`, 22 tests Python Vision et `git diff --check` — PASS. La
  qualification WebApp précédemment validée reste inchangée, ses dépendances
  locales n’étant pas présentes dans ce worktree.
- Commits : `127498b` (implémentation et tests), commit de statut ci-dessous
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

## Jalon M024 — Corrélation Vision vers incident

- Worktree dédié : `/home/rock/Synora-worktrees/v1-m024-vision-incident`
- Branche dédiée : `codex/v1-m024-vision-incident`
- Commit fonctionnel : `05c9708`
- Livrables : le parcours `clip → vision → Bus → Core → StateStore → Engine`
  conserve les identifiants physiques du clip, de la caméra, du track, de
  l’activation et de la séquence. Chaque événement Vision possède un ID stable
  dérivé du clip et son média est attaché à l’incident correspondant.
- Fiabilité : les tentatives Vision retryables ne publient pas d’échec terminal
  intermédiaire ; `clip.failed` est réservé à l’épuisement des retries. Le
  lifecycle reprend ainsi de `processing` vers `processed` sans état empoisonné.
  `vision.end` est publié après `clip.processed` et Core ne purge les tracks
  qu’après la terminaison de tous les clips connus de l’activation, en
  conservant le comportement legacy des événements directs sans clip.
- Compatibilité : les replays et messages tardifs restent idempotents par ID
  stable ; les payloads Vision ne peuvent pas réécrire le clip ou la caméra
  acceptés par l’ingress. Une identité résident n’est acceptée qu’après
  validation du résident et des seuils de présence ; les événements unknown ou
  uncertain ne rebondissent pas sur un track déjà lié à un résident.
- Tests déterministes ajoutés : parcours multi-clips d’une activation,
  corrélation de deux médias au même incident, échec transitoire puis retry
  complet, publication d’un échec terminal après épuisement, et ordre
  `clip.processed`/`vision.end`.
- Validations : `GOFLAGS=-buildvcs=false go test ./...`,
  `GOFLAGS=-buildvcs=false go vet ./...`,
  `GOFLAGS=-buildvcs=false go build ./...`,
  `GOFLAGS=-buildvcs=false go test -race ./...`, tests Python Vision 35 et
  `git diff --check` — PASS. Les deux premières races globales ont exposé des
  échecs intermittents préexistants dans `shadowworkflow`; les tests ciblés
  M024 passent sous race et la troisième race globale complète est verte. La
  qualification Web n’a pas été relancée, ses dépendances locales étant
  absentes et leur installation hors périmètre autorisé.
- État : validation complète verte ; intégration dans `integration/synora-v1`
  autorisée.

## Jalon M023 — Reconnaissance faciale locale

- Worktree dédié : `/home/rock/Synora-worktrees/v1-m023-face-recognition`
- Branche dédiée : `codex/v1-m023-face-recognition`
- Commit fonctionnel : `7a3ce0b`
- Livrables : SCRFD et ArcFace restent reliés à la base immuable chargée par
  `FaceDatasetManager`, avec embeddings normalisés et dimension contractuelle
  512. Les sorties restent strictement `match` (résident), `unknown` ou
  `uncertain` et les événements ne divulguent ni embedding ni image.
- Invariants V1 : seuil de match `0.58`, seuil uncertain `0.45`, seuil minimal
  de visage `80 px` sur toutes les passes d’analyse, égalités de seuil
  inclusives, rejet des dimensions incorrectes, valeurs non finies et vecteurs
  nuls. Les cas zéro visage, plusieurs visages, visage trop petit et qualité
  inutilisable restent refusés au build dataset.
- Cache : la réutilisation track est bornée à 10 secondes avec horloge
  monotone ; une identité `uncertain` ne peut pas devenir `match` par cache.
  Le remplacement à chaud d’une version ou la suppression d’un résident
  invalide les mémoires et buffers d’identité avant toute nouvelle émission.
- Tests déterministes ajoutés : seuils et taille minimale, expiration du cache,
  non-promotion de `uncertain`, invalidation sur remplacement de dataset,
  normalisation/dimension des embeddings, égalités et non-divulgation des
  données biométriques.
- Validations : `GOFLAGS=-buildvcs=false go test ./...`,
  `GOFLAGS=-buildvcs=false go vet ./...`,
  `GOFLAGS=-buildvcs=false go build ./...`,
  `GOFLAGS=-buildvcs=false go test -race ./...`, compilation Python et 35
  tests Python Vision — PASS. Deux premières races globales ont exposé des
  échecs intermittents préexistants dans `dispatcher` puis `shadowworkflow` ;
  les tests concernés passent isolément et la troisième race globale complète
  est verte. La qualification Web n’a pas été relancée, ses dépendances
  locales étant absentes et leur installation hors périmètre autorisé.
- État : validation complète verte ; intégration dans `integration/synora-v1`
  autorisée.

## Jalon M022 — Dataset facial versionné

- Worktree dédié : `/home/rock/Synora-worktrees/v1-m022-face-dataset`
- Branche dédiée : `codex/v1-m022-face-dataset`
- Commit fonctionnel : `a40943f`
- Livrables : uploads bornés et validés sous `uploads`, sources canoniques
  sous `sources/<resident_id>`, staging isolé, versions immuables sous
  `datasets/versions` et pointeur `datasets/current` publié atomiquement
  après rechargement Vision réussi. Les sources sont contrôlées par taille,
  checksum, type MIME réel, dimensions, orientation, unicité et état du
  résident ; les cas sans visage ou multi-visages sont refusés au build par
  la frontière Vision.
- Fiabilité : un build interrompu ou un reload refusé conserve la dernière
  version courante ; une reprise réutilise la version immuable déjà publiée
  pour la même `desired_revision`, y compris après `BuiltAt` différent. Une
  collision de révision différente reste refusée. Les activations concurrentes
  de la même version convergent vers un seul dataset immuable ; la rétention
  et la purge ne suppriment jamais `current`.
- Compatibilité : `FaceDatasetVersion` reste limité aux métadonnées traversant
  la frontière Discovery/Vision ; embeddings, chemins et stockage restent
  internes. Aucun champ RFC3339 existant n’est migré vers Unix ms et aucune
  définition de `Track` ou `Presence` n’est promue en contrat public.
- Tests déterministes ajoutés : reprise après publication interrompue,
  conservation du pointeur précédent après reload échoué, activation
  concurrente, validation de manifeste et exclusion des champs internes du
  contrat public.
- Validations : `GOFLAGS=-buildvcs=false go test ./...`,
  `GOFLAGS=-buildvcs=false go vet ./...`,
  `GOFLAGS=-buildvcs=false go build ./...`,
  `GOFLAGS=-buildvcs=false go test -race ./...`, 32 tests Python Vision et
  `git diff --check` — PASS. Une première race globale a exposé un échec
  intermittent de `internal/dispatcher`; le test passe 3/3 isolément et la
  seconde race globale complète est verte. La qualification Web n’a pas été
  relancée, ses dépendances locales étant absentes et leur installation hors
  périmètre autorisé.
- État : validation complète verte ; intégration dans `integration/synora-v1`
  autorisée.

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

## Jalon M021 — Détection personne et tracking réel

- Worktree dédié : `/home/rock/Synora-worktrees/v1-m021-person-tracking`
- Branche dédiée : `codex/v1-m021-person-tracking`
- Commit fonctionnel : `19d68f5`
- Livrables : normalisation des sorties YOLO RKNN (formats batch, transposé et
  mono-détection), filtrage fail-closed des NaN/boîtes invalides, conversion
  cohérente des coordonnées letterbox vers les résolutions source et NMS
  bornée pour les personnes.
- Tracking : les tracks gardent leur identifiant pendant une perte brève puis
  une récupération ; seuls les tracks observés dans la frame courante alimentent
  l’analyse visage, tandis que les tracks manqués restent disponibles pour la
  continuité de reconnaissance. Les frames grayscale, BGRA et flottantes sont
  normalisées avant traitement.
- Compatibilité : les métadonnées `activation_id`, `clip_id`, `clip_index`,
  `track_id` et `sequence_key` restent portées par le contexte d’événement ; les
  limites existantes de détections/tracks restent inchangées.
- Tests déterministes ajoutés : sorties synthétiques mono/transposées, NMS de
  personnes multiples, absence et sorties malformées, perte/récupération de
  track, boîtes invalides et normalisation de frames multi-résolutions.
- Validations : `GOFLAGS=-buildvcs=false go test ./...`, `GOFLAGS=-buildvcs=false
  go vet ./...`, `GOFLAGS=-buildvcs=false go build ./...`,
  `GOFLAGS=-buildvcs=false go test -race ./...` et tests Python Vision (32) —
  PASS. La qualification Web n’a pas été relancée, ses dépendances locales
  étant absentes et leur installation hors périmètre autorisé.
- État : validation complète verte ; intégration dans `integration/synora-v1`
  autorisée.

## Jalon M020 — Protocole du runtime Vision

- Worktree dédié : `/home/rock/Synora-worktrees/v1-m020-vision-protocol`
- Branche dédiée : `codex/v1-m020-vision-protocol`
- Commit fonctionnel : `52460f7`
- Livrables : handshake JSON-lines obligatoire `protocol.hello`, version de
  protocole `synora.vision.v1`, corrélation par `request_id`, annonce du
  backend, des modèles, de la dimension ArcFace et de l’état du dataset chargé.
  Les requêtes clip utilisent explicitement `clip.process`.
- Fiabilité : les réponses malformées ou désynchronisées sont rejetées et la
  connexion est fermée ; une capacité dégradée, un modèle requis absent, un
  backend RKNN indisponible ou une dimension autre que 512 ne produisent aucun
  résultat Vision. L’état `degraded` remonte dans le health de Discovery et les
  délais existants restent bornés.
- Compatibilité : le transport JSON-lines et les opérations dataset existantes
  sont conservés ; les réponses clip retournent désormais leur identifiant de
  requête pour valider la compatibilité Go/Python. Aucun contrat public V1 ni
  sémantique de timestamp n’a été modifié.
- Tests déterministes ajoutés : sérialisation hello, fragmentation des
  réponses, corrélation incorrecte, handshake obligatoire, capacités
  dégradées, refus sans faux résultat et compatibilité JSON côté Python.
- Validations : `GOFLAGS=-buildvcs=false go test ./...`, `GOFLAGS=-buildvcs=false
  go vet ./...`, `GOFLAGS=-buildvcs=false go build ./...`,
  `GOFLAGS=-buildvcs=false go test -race ./...` et tests Python Vision (26) —
  PASS. Une première course globale a exposé le flaky connu
  `internal/cge/shadowworkflow/TestQualificationCorruptMiddleWALFailsClosed` ;
  il passe 3/3 isolément et la seconde course globale complète est verte.
- Limite environnement : la qualification Web n’a pas été relancée, car les
  dépendances locales de `synora-web` sont absentes et leur installation est
  hors périmètre autorisé.
- État : validation complète verte ; intégration dans `integration/synora-v1`
  autorisée.

## Jalon M019 — Rétention, quota et accès média

- Worktree dédié : `/home/rock/Synora-worktrees/v1-m019-media-retention`
- Branche dédiée : `codex/v1-m019-media-retention`
- Commit fonctionnel : `c073d2b`
- Livrables : endpoint protégé `/api/clips/{id}/media`, dérivation du chemin
  canonique sans exposition de chemin interne, support GET/HEAD et Range via
  HTTP, avec accès limité aux clips `ready`/`processed`.
- Contrôles V1 : rejet des clips `missing`/`expired` ou non prêts, refus des
  symlinks et traversals, vérification de taille et checksum SHA-256 juste
  avant lecture, et conservation de la politique de purge/quota existante.
- Tests déterministes ajoutés : Range valide, checksum falsifié, états
  expired/missing, symlink, traversal et purge pendant une lecture déjà
  ouverte ; la rétention conserve les associations et les métadonnées expirées.
- Validations : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`, `GOFLAGS=-buildvcs=false go build
  ./...`, `timeout 360s env GOFLAGS=-buildvcs=false go test -race ./... -count=1`
  et tests Python Vision (22) — PASS.
- Limite environnement : la qualification Web n’a pas été relancée, car les
  dépendances locales de `synora-web` sont absentes et leur installation est
  hors périmètre autorisé.
- État : validation complète verte ; intégration dans `integration/synora-v1`
  autorisée.

## Jalon M018 — Queue clips, retry et reprise

- Worktree dédié : `/home/rock/Synora-worktrees/v1-m018-clip-queue`
- Branche dédiée : `codex/v1-m018-clip-queue`
- Commit fonctionnel : `af15405`
- Livrables : queue Vision bornée et dédupliquée par identifiant logique,
  journal JSON durable sous le spool clips, reprise des jobs non acquittés,
  timeout de traitement, retries plafonnés et backoff exponentiel borné.
- Fiabilité : le job reste dans le journal jusqu’au succès du callback ou à la
  notification d’échec terminal ; un timeout terminal publie `clip.failed` avec
  un `failure_code` exploitable, et les identifiants de job sont clonés à la
  frontière de queue.
- Tests déterministes ajoutés : restauration/ack du journal, borne de queue,
  déduplication des pending jobs, timeout après retries et échec permanent ;
  les tests existants de retry, fermeture et indisponibilité restent verts.
- Validations : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`, `GOFLAGS=-buildvcs=false go build
  ./...`, `timeout 360s env GOFLAGS=-buildvcs=false go test -race ./... -count=1`
  et tests Python Vision (22) — PASS après une première flakiness fonctionnelle
  isolée dans CGE, passée 3/3 isolément puis lors de la seconde passe globale.
- Limite environnement : la qualification Web n’a pas été relancée, car les
  dépendances locales de `synora-web` sont absentes et leur installation est
  hors périmètre autorisé.
- État : validation complète verte ; intégration dans `integration/synora-v1`
  autorisée.

## Jalon M017 — Ingress vidéo borné et sûr

- Worktree dédié : `/home/rock/Synora-worktrees/v1-m017-video-ingress`
- Branche dédiée : `codex/v1-m017-video-ingress`
- Commit fonctionnel : `3177ad7`
- Livrables : lecture multipart en streaming vers un temporaire borné, puis
  renommage atomique avant publication `clip.ready`; la taille et le transfert
  sans longueur déclarée restent strictement bornés.
- Contrôles V1 : durée maximale de 60 secondes, extension/conteneur MP4 et
  média supporté, checksum SHA-256 annoncé, identifiant sûr, authentification
  caméra, quotas disque et nettoyage systématique des temporaires.
- Résilience : retries identiques acceptés, doublons divergents refusés,
  upload interrompu nettoyé, erreurs de stockage exposées en `507`, et clip
  final validé préservé par la réconciliation.
- Tests déterministes ajoutés : transfert chunked, coupure de flux, taille,
  durée, média/conteneur, checksum, caméra non autorisée, quotas, symlinks,
  publication Core défaillante et nettoyage sans suppression du clip validé.
- Validations : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`, `GOFLAGS=-buildvcs=false go build
  ./...`, `timeout 360s env GOFLAGS=-buildvcs=false go test -race ./... -count=1`
  et tests Python Vision (22) — PASS.
- Limite environnement : la qualification Web n’a pas été relancée, car les
  dépendances locales de `synora-web` sont absentes et leur installation est
  hors périmètre autorisé.
- État : validation complète verte ; intégration dans `integration/synora-v1`
  autorisée.

## Jalon M016 — Contrat et store Clips V1

- Worktree dédié : `/home/rock/Synora-worktrees/v1-m016-clips`
- Branche dédiée : `codex/v1-m016-clips`
- Commit fonctionnel : `e06836f`
- Livrables : cycle de vie durable `receiving/ready/processing/processed`,
  ainsi que `failed/missing/expired`, transitions autorisées et idempotentes,
  conservation des métadonnées canoniques et compatibilité des enregistrements
  existants.
- Correctifs d’intégrité : collision d’identifiant avec métadonnées divergentes
  refusée, collision `(camera_id, activation_id, clip_index)` refusée, et
  rattachement incident↔clip complété lors d’un enregistrement tardif.
- Tests déterministes ajoutés : matrice complète des transitions, idempotence,
  collisions d’identité et d’index, et restauration des références
  incident/événement.
- Validations : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`, `GOFLAGS=-buildvcs=false go build
  ./...`, `timeout 360s env GOFLAGS=-buildvcs=false go test -race ./... -count=1`
  et tests Python Vision (22) — PASS.
- Limite environnement : la qualification Web n’a pas été relancée, car les
  dépendances locales de `synora-web` sont absentes et leur installation est
  hors périmètre autorisé.
- État : validation complète verte ; intégration dans `integration/synora-v1`
  autorisée.

## Jalon M015 — API et temps réel des incidents

- Worktree dédié : `/home/rock/Synora-worktrees/v1-m015-incident-api`
- Branche dédiée : `codex/v1-m015-incident-api`
- Commit fonctionnel : `4fb12a7`
- Livrables validés sur l’existant : liste/détail/statut des incidents par API
  protégée, contrats realtime versionnés `connection.ready`, `snapshot`,
  `security_state.changed`, `incident.created`, `incident.updated` et
  `resync_required`, avec curseur epoch/séquence/révision.
- Tests déterministes ajoutés : déduplication des messages Bus, détection des
  trous de séquence et changement d’epoch avec resynchronisation avant delta ;
  les tests existants couvrent ordre snapshot/delta, reconnexion, cursor trop
  ancien, client lent, clients multiples, Bus Unix et contrôle d’accès.
- Invariants conservés : l’abonnement `api` ne lit que le client Bus dédié,
  les messages Core sont filtrés par source, les publications sont sérialisées
  avec le snapshot initial et aucun delta n’est envoyé avant la resynchronisation
  requise.
- Validations : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`, `GOFLAGS=-buildvcs=false go build
  ./...`, `timeout 300s env GOFLAGS=-buildvcs=false go test -race ./... -count=1`
  et tests Python Vision (22) — PASS.
- Limite environnement : la qualification Web n’a pas été relancée, car les
  dépendances locales de `synora-web` sont absentes et leur installation est
  hors périmètre autorisé.
- État : validation complète verte ; intégration dans `integration/synora-v1`
  autorisée.

## Jalon M014 — Incidents V1 durables

- Worktree dédié : `/home/rock/Synora-worktrees/v1-m014-incidents`
- Branche dédiée : `codex/v1-m014-incidents`
- Commit fonctionnel : `1028f79`
- Livrables : regroupement borné à une fenêtre d’une minute et à la même
  caméra/au même lieu, déduplication d’événements rejoués, conservation des
  statuts `new/viewed/acknowledged/resolved`, références d’événements/clips
  bornées et enrichissement par clip arrivé tardivement.
- Correctifs d’intégrité : une intrusion indépendante sur une autre caméra ou
  un autre lieu ne fusionne plus via une `sequence_key`, une `activation_id`,
  un track ou un entity ID réutilisé ; un incident résolu ne peut pas être
  rouvert par une nouvelle observation ; l’acquittement reste durable.
- Tests déterministes ajoutés : frontières caméra/lieu, résolution sans
  réouverture, clip tardif et concurrence de doublons. Les tests existants
  couvrent idempotence, transitions de statut, références manquantes,
  persistance/reprise et rétention.
- Validations : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`, `GOFLAGS=-buildvcs=false go build
  ./...`, `timeout 300s env GOFLAGS=-buildvcs=false go test -race ./... -count=1`
  et tests Python Vision (22) — PASS.
- Note de qualification race : deux premières passes ont exposé
  successivement les flaky connus de `internal/cge/shadowworkflow` et
  `internal/dispatcher`; les relances ciblées et la troisième passe globale
  sont vertes, sans rapport de data race.
- Limite environnement : la qualification Web n’a pas été relancée, car les
  dépendances locales de `synora-web` sont absentes et leur installation est
  hors périmètre autorisé.
- État : validation complète verte ; intégration dans `integration/synora-v1`
  autorisée.

## Jalon M013 — Présence résidente cohérente

- Worktree dédié : `/home/rock/Synora-worktrees/v1-m013-presence`
- Branche dédiée : `codex/v1-m013-presence`
- Commit fonctionnel : `f49c145`
- Livrables : seuils d’hystérésis centralisés (`enter=0.60`, `exit=0.40`),
  présence par résident et emplacement, provenance additive de confiance dans
  StateStore (`confidence_source`), decay de 15 minutes à la frontière de
  contexte en lecture seule, et conservation de `last_seen` lors d’une caméra
  muette ou d’une expiration.
- Compatibilité : la vue legacy `residents` reste inchangée ; la provenance
  est exposée uniquement dans la collection publique runtime `presence` et
  persiste avec le StateStore. Une identité incertaine ne devient jamais une
  présence certaine, et les identités faibles restent classées uncertain dans
  les incidents.
- Tests déterministes ajoutés : entrée/sortie et oscillation aux seuils,
  refus d’une identité incertaine, decay sans mutation de la source, source de
  confiance dans snapshot read-only, caméra muette avec dernier signal et
  restauration de la provenance après redémarrage.
- Validations : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`, `GOFLAGS=-buildvcs=false go build
  ./...`, `timeout 300s env GOFLAGS=-buildvcs=false go test -race ./... -count=1`
  et tests Python Vision (22) — PASS.
- Note de qualification race : une première passe a reproduit l’échec
  intermittent connu de `internal/cge/shadowworkflow`; la relance ciblée et
  la seconde passe globale sont vertes, sans rapport de data race.
- Limite environnement : la qualification Web n’a pas été relancée, car les
  dépendances locales de `synora-web` sont absentes et leur installation est
  hors périmètre autorisé.
- État : validation complète verte ; intégration dans `integration/synora-v1`
  autorisée.

## Jalon M012 — Tracks, clusters et fenêtres temporelles

- Worktree dédié : `/home/rock/Synora-worktrees/v1-m012-tracks`
- Branche dédiée : `codex/v1-m012-tracks`
- Commit fonctionnel : `b15b1af`
- Livrables : identité de track explicitement bornée par caméra et activation,
  conservation de l’identité lors d’un déplacement de nœud, refus des
  réaffectations silencieuses entre résidents, expiration des tracks à 20 s et
  des clusters à 10 s, et borne de 100 identifiants par cluster fusionné.
- Tests déterministes ajoutés : séparation caméra/activation, mouvement de
  nœud, refus de rebinding résident, ordre d’expiration TTL et rafale de 2 000
  observations avec cluster borné.
- Validations : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`, `GOFLAGS=-buildvcs=false go build
  ./...`, `timeout 300s env GOFLAGS=-buildvcs=false go test -race ./... -count=1`
  et tests Python Vision (22) — PASS.
- Note de qualification race : une première passe a exposé un échec
  intermittent isolé dans `internal/cge/shadowworkflow`; la relance ciblée et
  la seconde passe globale sont vertes, sans rapport de data race.
- Invariants conservés : `sequence_key` reste une métadonnée de rejeu,
  RFC3339 reste la représentation temporelle canonique et aucun contrat public
  V1 n’a été modifié.
- État : validation complète verte ; intégration dans `integration/synora-v1`
  autorisée.

## Jalon M011 — Machine de sécurité déterministe

- Worktree dédié : `/home/rock/Synora-worktrees/v1-m011-security`
- Branche dédiée : `codex/v1-m011-security`
- Commit fonctionnel : `01fcf22`
- Livrables : sérialisation concurrente de l’état temporel de
  `DangerRuntime`, reset explicite de l’hystérésis lors d’un reset Core
  autorisé, et matrice table-driven des entrées de sécurité.
- Couverture déterministe : résident connu, inconnu avec intrusion immédiate,
  disparition caméra, répétition, reset, événement hors ordre ; les timelines
  existantes couvrent decay, verrou de 15 secondes et redémarrage pendant le
  verrou.
- Validations : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`, `GOFLAGS=-buildvcs=false go build
  ./...`, `timeout 300s env GOFLAGS=-buildvcs=false go test -race ./... -count=1`
  et tests Python Vision (22) — PASS.
- Invariant conservé : une disparition caméra dans une zone de contrôle reste
  un signal de sécurité `suspicious` (niveau 3), conformément au scoring V1
  existant ; aucune sémantique n’a été modifiée silencieusement.
- État : validation complète verte ; intégration fast-forward autorisée.

## Jalon M010 — Cycle de vie propre des services Go

- Worktree dédié : `/home/rock/Synora-worktrees/v1-m010-lifecycle`
- Branche dédiée : `codex/v1-m010-lifecycle`
- Commit fonctionnel : `64de094`
- Livrables : arrêt SIGTERM/context déterministe du Core, arrêt des boucles
  périodiques et attente des goroutines avant persistance finale, fermeture
  idempotente des ressources Discovery, supervision et serveurs HTTP
  annulables, retries de connexion Bus bornés et annulables pour Core, API,
  Actions, Discovery et Runtime Manager.
- Compatibilité : les wrappers sans contexte restent disponibles pour les
  appelants et tests existants ; aucun contrat V1 n’a été modifié.
- Tests ajoutés : annulation déterministe du raccordement Bus, arrêt du loop
  Core et arrêt du loop runtime Discovery.
- Validations : `GOFLAGS=-buildvcs=false go test ./... -count=1`,
  `GOFLAGS=-buildvcs=false go vet ./...`, `GOFLAGS=-buildvcs=false go build
  ./...`, tests Python Vision (22) — PASS.
- Race : `timeout 300s env GOFLAGS=-buildvcs=false go test -race ./... -count=1`
  — PASS, exit 0, après une première passe ayant exposé deux flaky connus
  dans `internal/cge/shadowworkflow` et `internal/delivery` ; aucun rapport
  de data race n’a été produit.
- Limite environnement : la qualification Web n’a pas été relancée, car les
  dépendances locales de `synora-web` sont absentes et leur installation est
  hors périmètre autorisé.
- État : validation complète verte ; intégration dans `integration/synora-v1`
  autorisée.
