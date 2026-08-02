# CGE Shadow — préparation du smoke physique

Ce document décrit la préparation de la passe 62. Il ne constitue pas une
procédure exécutée sur la Rock : aucune installation, activation, modification
de `/etc`, `/opt`, `/var/lib` ou `/run` n'est effectuée par cette passe.

La classification reste **C0 — Shadow end-to-end prouvé hermétiquement avec
contexte Core read-only**. Le smoke décrit ici est la prochaine étape
opérateur, et sa réussite physique n'est pas encore démontrée.

## Artefacts et préflight

Le profil contrôlé est
`deployments/env/synora-core-cge-shadow.env.example`. Il est volontairement
un fichier d'exemple sans secret. Lors d'une installation future, sa copie
sera `/etc/synora/synora-core.env`; l'unité Core charge ce fichier avec
`EnvironmentFile=-/etc/synora/synora-core.env`. Le préfixe `-` rend le fichier
facultatif : sans ce fichier, les valeurs de code et le comportement par
défaut restent inchangés.

Le préflight read-only est `tools/dev/synora-cge-preflight`. Il réutilise le
parseur de production `cge.LoadShadowConfig`, refuse les variables inconnues,
les doublons, les entrées vides et les valeurs ressemblant à des secrets. Il
ne crée aucun répertoire, ne répare aucun fichier et ne communique pas avec
systemd.

Modes disponibles :

```text
go run ./tools/dev/synora-cge-preflight --mode config \
  --env deployments/env/synora-core-cge-shadow.env.example

go run ./tools/dev/synora-cge-preflight --mode systemd \
  --unit deployments/systemd/synora-core.service

go run ./tools/dev/synora-cge-preflight --mode filesystem --root <root-simulee>

go run ./tools/dev/synora-cge-preflight --mode build \
  --binary-dir <build-dir> --version <build-dir>/version.json \
  --expected-commit <commit-court>

go run ./tools/dev/synora-cge-preflight --mode all \
  --env deployments/env/synora-core-cge-shadow.env.example \
  --root <root-simulee> --binary-dir <build-dir> \
  --version <build-dir>/version.json --expected-commit <commit-court>
```

`--root` représente une racine de test et permet de vérifier les chemins
attendus sans toucher aux chemins système. Le mode `build` exige trois ELF
ARM64 exécutables : `synora-core`, `synora-bus` et `synora-actions`, ainsi
qu'un `version.json` valide lorsqu'il est fourni.

## Profil de smoke

Le profil active explicitement : Shadow, workflow asynchrone, provider de
contexte Core, Calibration Ledger et analytics. Il conserve désactivés les
chemins d'autorité cognitive, auto-evidence, apprentissage de routines,
deviation, qualification et field trial. L'allowlist reste celle du code,
exactement `vision.identity`, `vision.unknown` et `vision.uncertain`.

Valeurs importantes du profil :

| Variable | Valeur smoke | Effet |
| --- | --- | --- |
| `SYNORA_CGE_SHADOW_ENABLED` | `true` | active le Shadow |
| `SYNORA_CGE_SHADOW_WORKFLOW_ENABLED` | `true` | active le workflow asynchrone |
| `SYNORA_CGE_SHADOW_WORKFLOW_STORE_MODE` | `file` | active le FileStore durable du workflow |
| `SYNORA_CGE_SHADOW_WORKFLOW_STORE_DIRECTORY` | `/var/lib/synora/cge/workflow` | répertoire du WAL et du checkpoint |
| `SYNORA_CGE_SHADOW_CONTEXT_ENABLED` | `true` | demande le contexte Core read-only |
| `SYNORA_CGE_DATA_DIR` | `/var/lib/synora/cge` | racine durable CGE |
| `SYNORA_CGE_JOURNAL_PATH` | `/var/lib/synora/cge/journal.ndjson` | journal Shadow |
| `SYNORA_CGE_CALIBRATION_LEDGER_ENABLED` | `true` | active le ledger |
| `SYNORA_CGE_CALIBRATION_LEDGER_FSYNC` | `true` | durabilité renforcée |
| `SYNORA_CGE_CALIBRATION_LEDGER_MAX_BYTES` | `16777216` | plafond de 16 MiB |
| `SYNORA_CGE_CALIBRATION_LEDGER_MAX_RECORDS` | `1000` | plafond de 1000 records |
| `SYNORA_CGE_CALIBRATION_ANALYTICS_ENABLED` | `true` | active les analytics bornées |
| `SYNORA_CGE_CALIBRATION_ANALYTICS_MAX_WINDOWS` | `10` | limite de fenêtres |
| `SYNORA_CGE_SHADOW_QUALIFICATION_ENABLED` | `false` | opt-in séparé |
| `SYNORA_CGE_FIELD_TRIAL_ENABLED` | `false` | aucun field trial |

Le profil active explicitement `StoreMode=file` avec
`StoreDirectory=/var/lib/synora/cge/workflow`. Le défaut de production reste
`StoreMemory` lorsque ces variables sont absentes. Le runtime crée lui-même ce
répertoire avec le mode `0700`, puis `workflow.wal` et
`workflow.checkpoint.json` avec le mode `0600`; le profil exige
`SyncOnCommit=true`. Le checkpoint est créé après le premier commit selon la
politique du runtime et le recovery relit le même store après une fermeture et
une réouverture propres.

Les variables reconnues sont celles des parseurs `internal/cge`,
`internal/cge/shadowworkflow` et `internal/cge/fieldtrial`. Elles couvrent les
blocs Shadow, contexte, routines, deviation, workflow, ledger, analytics,
qualification et field trial. Le préflight vérifie l'alignement du profil avec
ces parseurs et n'ajoute aucune variable.

## Empreinte de build ARM64

La procédure future de build est réalisée dans un répertoire temporaire :

```bash
BUILD_DIR="$(mktemp -d)"
GOCACHE=/tmp/synora-gocache go build -o "$BUILD_DIR/synora-core" ./cmd/synora-core
GOCACHE=/tmp/synora-gocache go build -o "$BUILD_DIR/synora-bus" ./cmd/synora-bus
GOCACHE=/tmp/synora-gocache go build -o "$BUILD_DIR/synora-actions" ./cmd/synora-actions

SYNORA_GIT_COMMIT="$(git rev-parse --short HEAD)" \
SYNORA_BUILD_TIME="<RFC3339-fixe-du-build>" \
GOCACHE=/tmp/synora-gocache go run ./cmd/synora-version \
  -output "$BUILD_DIR/version.json"

file "$BUILD_DIR"/*
sha256sum "$BUILD_DIR"/*
ls -lh "$BUILD_DIR"/*
```

`version.json` identifie le commit court, la carte cible, le schéma de
configuration et le bundle. Le champ build time doit être injecté par le
runbook et n'est pas utilisé comme fingerprint fonctionnel. Une lacune
demeure : les binaires Go ne contiennent pas aujourd'hui une version linkée
directement ; la corrélation fiable est donc le triplet artefact, checksum et
`version.json` généré au même build.

## Empreinte système attendue

L'unité de référence est `deployments/systemd/synora-core.service` :

| Élément | Valeur auditée |
| --- | --- |
| utilisateur/groupe | `synora:synora` |
| binaire | `/opt/synora/bin/synora-core` |
| bus | `After` et `Requires` sur `synora-bus.service` |
| EnvironmentFile | facultatif : `/etc/synora/synora-core.env` |
| journal Core | `/var/lib/synora/logs/events.log` |
| redémarrage | `Restart=always`, `RestartSec=2` |
| arrêt | valeurs systemd par défaut ; `TimeoutStopSec` et `KillSignal` non spécifiés |
| ressources | `LimitNOFILE=65536` |
| hardening présent | `NoNewPrivileges=true` |
| hardening absent | pas de `StateDirectory`, `ReadWritePaths`, `ReadOnlyPaths`, `ProtectSystem`, `ProtectHome` ou `PrivateTmp` sur cette unité |

Le hardening n'est pas élargi automatiquement : le Core doit écrire son état
historique, le journal Shadow, le ledger et les logs. Le bus crée `/run/synora`
avec `RuntimeDirectory=synora` et le Core n'en est pas propriétaire.

### Chemins et permissions futures

| Chemin | Propriétaire / mode attendu | Création | Contenu |
| --- | --- | --- | --- |
| `/opt/synora/bin` | `root:root`, `0755` | installateur | binaires |
| `/opt/synora/version.json` | `root:root`, `0644` | installateur | manifest non secret |
| `/var/lib/synora` | `synora:synora`, `0755` | install-dirs | état durable |
| `/var/lib/synora/cge` | `synora:synora`, `0750` recommandé | pré-création contrôlée | journal, ledger, rapports |
| `/var/lib/synora/cge/workflow` | `synora:synora`, `0700` | runtime FileStore | `workflow.wal`, `workflow.checkpoint.json` |
| `/etc/synora` | `root:synora`, `0755` | installateur | profil non secret |
| `/etc/synora/synora-core.env` | `root:synora`, `0640` recommandé | opérateur | variables de profil |
| `/run/synora` | `synora:synora`, `0770` | systemd/tmpfiles du bus | socket bus, runtime éphémère |

Le fichier ledger est lisible et inscriptible par `synora` uniquement. Les
permissions exactes de la future création de `cge` doivent être appliquées
par la procédure d'installation contrôlée ; le préflight ne les applique pas.

## Dimensionnement indicatif

Hypothèses conservatrices : un record de calibration peut atteindre la limite
de 64 KiB du ledger, un événement accepté par minute, et aucune compression.
Le ledger du profil est plafonné à 16 MiB ou 1000 records. Ces nombres sont
des bornes de calcul, pas une mesure du prototype.

| Durée | Événements à 1/min | borne records bruts | interprétation |
| --- | ---: | ---: | --- |
| 15 min | 15 | 0,94 MiB | smoke court |
| 1 h | 60 | 3,75 MiB | sous le plafond ledger |
| 24 h | 1440 | 90 MiB théorique, 16 MiB effectifs ledger | plafond atteint avant 24 h |
| 7 jours | 10080 | 630 MiB théorique, 16 MiB effectifs ledger | records supplémentaires refusés/à observer |

Les limites à retenir sont des plafonds configurés ou de code, pas des
consommations attendues :

- Calibration Ledger : 16 MiB ou 1000 records dans ce profil ;
- Workflow WAL : plafond de code de 256 MiB ;
- Workflow checkpoint : plafond de code de 256 MiB ;
- qualification : limites de sortie et de WAL séparées lorsqu'elle est
  explicitement activée ;
- logs Core et journal : croissance séparée, soumise à la rotation opérateur.

Les logs et le journal doivent être surveillés et soumis à une rotation
opérateur avant toute campagne 24 h ou 7 jours.

## Préparation future

### Avant installation

1. vérifier la branche, le commit attendu et un worktree propre ;
2. exécuter les tests et le build ARM64 ;
3. conserver `file`, checksums et `version.json` ;
4. exécuter le préflight `config`, `systemd`, `filesystem` sur une racine
   simulée, puis `all` ;
5. vérifier l'espace libre et sauvegarder les données persistantes selon la
   politique existante ;
6. confirmer que le profil de smoke contient bien `Field Trial=false` et ne
   contient aucun secret.

### Installation, non exécutée dans la passe 62

La procédure future, après autorisation explicite, sera :

```text
copier les trois binaires et version.json vers /opt/synora
créer /var/lib/synora/cge avec synora:synora et le mode contrôlé
copier le profil vers /etc/synora/synora-core.env
appliquer les permissions documentées
auditer l'unité dans /etc/systemd/system
systemctl daemon-reload
redémarrer uniquement selon la fenêtre opérateur approuvée
```

Ces actions sont listées pour le futur runbook mais n'ont pas été lancées.
La passe 62 ne modifie aucun service.

### Vérifications du smoke

Les critères de succès sont fermés : service Core actif, commit et manifest
attendus, bus connecté, décision historique fonctionnelle, Shadow activé,
`vision.identity` admis, `vision.weapon` ignoré par policy, snapshot Core
réussi, commit durable, situation, recommandation, comparaison, record ledger,
analytics disponibles ou explicitement insuffisantes, absence de sensible,
absence d'action CGE, puis recovery avec séquence et chaîne de hash intactes.

Après chaque événement accepté, le smoke doit parcourir récursivement tous les
fichiers réguliers produits sous `SYNORA_CGE_DATA_DIR`, le répertoire configuré
par `SYNORA_CGE_SHADOW_WORKFLOW_STORE_DIRECTORY` et le chemin du Calibration
Ledger. Le scan inclut sans exception `journal.ndjson`, les manifests et
snapshots de générations, `workflow.wal`, `workflow.checkpoint.json` et
`calibration-ledger.ndjson`. Il recherche les sentinelles dans le contenu brut
et dans les champs JSON décodés, en identifiant le type de record et le champ
concerné ; `journal.ndjson` ne doit pas être exclu.

Les anciens fichiers peuvent contenir des identifiants bruts : ils sont
signalés comme potentiellement sensibles et ne sont ni réécrits ni supprimés
par le runtime. Les nouveaux records doivent utiliser le format
`cgeid-v1:<domain>:<sha256-hex>` documenté dans
`internal/cge/README_PASS64_1_DURABLE_IDENTIFIER_REDACTION.md`.

Le smoke devra utiliser d'abord un événement synthétique autorisé et un
événement synthétique non autorisé. Un événement caméra réel est une étape
ultérieure et n'est pas simulé ici.

Arrêt immédiat si une décision historique est affectée, si une action CGE est
produite, en cas de panic, corruption ou échec fsync du ledger, croissance
disque anormale, boucle de restart, deadlock, queue_full inattendu ou fuite de
donnée sensible.

### Recovery et rollback

Après un smoke, l'opérateur arrête proprement le Core, conserve le journal,
ledger, workflow WAL, checkpoint, rapport et checksums, puis relance avec le
même `DataDir` et le même `StoreDirectory`. Il vérifie la dernière séquence
du workflow, la révision et le digest, la reconstruction des projections,
le fingerprint du ledger, l'intégrité de la hash chain et le newline final.
Le runtime ne republie aucune observation pendant ce recovery et aucune
nouvelle entrée ledger ne doit être produite. Aucune réparation automatique
du WAL n'est permise ; une troncature finale reste gouvernée par la policy.

Un rollback ne supprime pas les preuves :

1. désactiver Shadow par configuration lors d'une prochaine fenêtre ;
2. conserver `/var/lib/synora/cge/workflow`, `/var/lib/synora/cge/calibration-ledger.ndjson` et `/var/lib/synora/cge/journal.ndjson` pour analyse ;
3. restaurer le binaire précédent et son `version.json` ;
4. conserver les données historiques et le ledger ;
5. effectuer ensuite un redémarrage contrôlé, séparément approuvé.

Le rollback ne réécrit ni le StateStore, ni le ledger, ni les secrets. La
compatibilité de lecture entre versions doit être vérifiée avant toute
campagne ; si elle n'est pas garantie, la campagne est bloquée.

## Observabilité et qualification courte

Commandes read-only futures : `systemctl status synora-core.service`,
`journalctl -u synora-core.service`, lecture de `/opt/synora/version.json`,
lecture des status internes déjà exposés par les tests/rapports, et mesure de
la taille et des checksums sous `/var/lib/synora/cge`. Aucun endpoint CGE n'est
supposé ici. Les codes d'admission, status context provider et état ledger
restent internes si aucun log ou CLI existant ne les publie ; cette absence
doit être notée dans le rapport de smoke.

La qualification courte est préparée mais désactivée dans le profil. Pour une
future fenêtre de 10 à 15 minutes, l'opérateur activera explicitement :

```text
SYNORA_CGE_SHADOW_QUALIFICATION_ENABLED=true
SYNORA_CGE_SHADOW_QUALIFICATION_RUN_ID=<run-id-sans-secret>
SYNORA_CGE_SHADOW_QUALIFICATION_PROFILE=smoke
SYNORA_CGE_SHADOW_QUALIFICATION_OUTPUT_DIR=/var/lib/synora/cge/qualification/<run-id>
SYNORA_CGE_SHADOW_QUALIFICATION_SAMPLE_INTERVAL=5s
SYNORA_CGE_SHADOW_QUALIFICATION_MAX_OUTPUT_BYTES=67108864
```

Le rapport sera lu sous `DataDir`; le code conserve sa durée de warmup par
défaut de 15 minutes. Une qualification logicielle ou un rapport `pass` ne
signifie pas que le déploiement physique est qualifié.

## État de préparation

`ReadyForControlledPhysicalSmoke = true` après les validations de la passe 62 :
profil vérifié, build ARM64 vérifié, préflight simulé et complet réussi,
permissions et rollback documentés, tests globaux verts. Cela autorise une
revue opérateur ; cela ne lance ni installation, ni service, ni événement
réel. La classification demeure C0.
