# Passe 49 — Instrumentation de qualification physique du Shadow Workflow

Cette passe prépare une campagne locale sur une Rock 5 ITX. Elle n’exécute pas
de campagne physique et ne change aucun comportement cognitif.

## Gate logiciel et qualification physique

`ShadowWorkflowReadiness.ReadyForPhysicalShadowQualification` reste le gate
logiciel de la passe 48. La nouvelle instrumentation expose
`QualificationInstrumentationReadiness().ReadyToStartPhysicalQualification`.
Les deux gates ne déclarent ni déploiement ni stabilité multi-jour :

```text
software qualification
        ↓
local bounded recorder + offline report
        ↓
[future] manual Rock 5 ITX qualification
```

`PhysicalDeploymentPerformed`, `SmokeProfileExecutedOnHub`,
`DurabilityProfileExecutedOnHub`, `EnduranceProfileExecutedOnHub` et
`MultiDayStabilityValidated` restent faux.

## Périmètre et coût désactivé

Le recorder est local, expurgé, borné et désactivé par défaut. Quand la
qualification est désactivée, aucun répertoire, fichier, ticker, goroutine ou
accès `/proc` n’est créé. Le chemin `TrySubmit` ne prend pas de mesure et ne
possède pas de dépendance vers le recorder.

## Configuration et profils

`QualificationConfig` valide un `RunID` borné, un chemin absolu explicite, les
intervalles et les limites. Les profils sont `smoke`, `durability`, `endurance`,
`stress`, `full_pipeline_synthetic` et `custom`. Ils décrivent des seuils et
des recommandations ; ils ne changent aucune policy cognitive.

Variables reconnues :

```text
SYNORA_CGE_SHADOW_QUALIFICATION_ENABLED
SYNORA_CGE_SHADOW_QUALIFICATION_RUN_ID
SYNORA_CGE_SHADOW_QUALIFICATION_PROFILE
SYNORA_CGE_SHADOW_QUALIFICATION_OUTPUT_DIR
SYNORA_CGE_SHADOW_QUALIFICATION_SAMPLE_INTERVAL
SYNORA_CGE_SHADOW_QUALIFICATION_MAX_OUTPUT_BYTES
```

La valeur par défaut est désactivée. Un répertoire est obligatoire lorsque
l’instrumentation est activée ; aucun chemin `/var/lib/synora` n’est implicite.
La configuration par défaut échantillonne toutes les 5 secondes, flush toutes
les 30 secondes, conserve au plus 100 000 samples et 512 MiB.

## Samples et redaction

Les fichiers sont :

```text
qualification.samples.ndjson
qualification.summary.json
qualification.manifest.json
```

Un sample contient uniquement des compteurs, des durées, des agrégats de
processus, de stockage, de queue, de stages et d’isolation historique. Il ne
contient aucun EventID, EpisodeID, FactID, HypothesisID, RequestID,
CapabilityInstanceID, grant, scope métier, payload, média, secret ou chemin
privé. Chaque sample et chaque document possède un SHA-256 déterministe.

Les samples sont append-only et bornés par `MaxSamples` et `MaxOutputBytes`.
À la limite, la collecte s’arrête et le workflow reste actif ; aucune rotation
infinie, suppression silencieuse ou bascule volatile n’est effectuée.

Le répertoire est `0700`, les fichiers `0600`. Les écritures de manifest et de
summary passent par fichier temporaire, `fsync`, rename atomique et `fsync` du
répertoire.

## Métriques

Les métriques processus utilisent `runtime.ReadMemStats`,
`runtime.NumGoroutine` et, sous Linux, une lecture bornée de `/proc/self` pour
RSS, CPU user/system et threads. Le fallback portable signale simplement les
valeurs indisponibles. Une erreur `/proc` ne dégrade pas le workflow.

Les stages mesurés sont `episode`, `situation_facts`,
`situation_hypotheses`, `evidence_discrimination`, `advisory_requests`,
`capability_mapping`, `authorization_boundary`, `transaction_planning`,
`durable_commit`, `checkpoint`, `recovery` et `full_cycle`. Chaque stage
conserve des compteurs, min/max/total et un histogramme récent borné à 256
durées, avec p50/p95/p99.

Les samples comprennent aussi :

* profondeur courante et high-water mark de la queue ;
* ratio de rejet queue pleine ;
* compteurs de cycles, commits, recovery et checkpoints ;
* octets moyens/maximaux des transactions et croissance du WAL ;
* taille du checkpoint et durées de checkpoint/recovery ;
* comparaisons historiques et mismatches, sans recopier les décisions.

## Rapport et gates

`QualificationReport` calcule les débits, distributions, tendances RSS/heap/
goroutines/GC, CPU moyen/p95, coût CPU par cycle, projection WAL et analyse des
checkpoints. La tendance mémoire utilise les samples après le warm-up configuré
(15 minutes par défaut), lorsque suffisamment de points sont disponibles.

Les gates critiques sont : mismatch historique, mismatch de fingerprint,
échec TrySubmit affectant une décision, filiation invalide, durabilité et
recovery. Les seuils de drop, timeout, p99 TrySubmit, p99 cycle, RSS et
goroutines sont provisoires et configurables. Un dépassement de performance
peut être un warning ; une rupture d’isolation ou de durabilité est un échec.

`PhysicalDeploymentPerformed` et `MultiDayStabilityValidated` restent faux dans
tous les rapports produits par cette passe.

## Reporter hors ligne

Le binaire `cmd/synora-cge-shadow-report` ne contacte aucun service :

```bash
GOCACHE=/tmp/synora-gocache go run ./cmd/synora-cge-shadow-report \
  --input /run-local/qualification.samples.ndjson \
  --manifest /run-local/qualification.manifest.json \
  --output /run-local/qualification.summary.json
```

Codes : `0` pass, `1` warning, `2` fail, `3` incomplete ou entrée invalide.
Les lignes tronquées, fingerprints invalides et samples invalides sont
signalés ; aucun contenu sensible n’est imprimé.

## Runbook smoke — 30 minutes

Créer un répertoire local `0700`, puis lancer manuellement le binaire déjà
présent dans le dépôt avec un environnement temporaire :

```bash
run_dir="$(mktemp -d)"
chmod 700 "$run_dir"
SYNORA_CGE_SHADOW_WORKFLOW_ENABLED=true \
SYNORA_CGE_SHADOW_QUALIFICATION_ENABLED=true \
SYNORA_CGE_SHADOW_QUALIFICATION_RUN_ID=smoke-local \
SYNORA_CGE_SHADOW_QUALIFICATION_PROFILE=smoke \
SYNORA_CGE_SHADOW_QUALIFICATION_OUTPUT_DIR="$run_dir" \
GOCACHE=/tmp/synora-gocache go run ./cmd/synora-core
```

Cette commande est une méthode de lancement manuel ; elle suppose que les
services et fichiers de configuration habituels du dépôt sont disponibles.
La campagne physique doit être arrêtée manuellement selon les procédures de
la centrale, puis analysée avec le reporter.

## Runbook durabilité — 2 heures

Utiliser un répertoire dédié explicitement fourni à la configuration durable,
`StoreMode=file` et le profil `durability`. Faire un démarrage, une activité
nominale, un arrêt propre, un redémarrage et comparer revision, sequence et
digest. Aucune corruption volontaire ne doit être faite sur la machine
principale.

## Runbook endurance — au moins 72 heures

Utiliser `StoreMode=file`, `advisory_requests`, le profil `endurance`, un
volume nominal et un répertoire local protégé. Conserver les samples, le
manifest et le summary pour l’analyse CPU, mémoire, WAL, checkpoints, queue,
timeouts et recovery. Un smoke test court ne valide pas cette étape.

## Runbook stress et pipeline synthétique

Le stress est limité à 10–30 minutes et aux débits contrôlés de 1, 2 puis 5
événements/seconde. Les outils de démonstration existants peuvent être
inspectés avec :

```bash
GOCACHE=/tmp/synora-gocache go run ./tools/dev/synora-cge-demo --help
GOCACHE=/tmp/synora-gocache go run ./cmd/synora-cge-shadow-report --help
```

Le démonstrateur est un outil local distinct et ne constitue pas un injecteur
d’événements Shadow. Aucun outil inexistant n’est supposé ici. Le profil
`full_pipeline_synthetic` est réservé aux providers synthétiques et ne qualifie
aucun dispositif réel.

## Limites et sécurité opérationnelle

Il n’y a ni endpoint, ni télémétrie distante, ni upload, ni collector réseau.
La file et les samples ne sont pas une nouvelle durabilité : seuls les commits
du workflow sont durables. Une campagne ne fournit pas exactly-once depuis le
bus source. Aucune compaction WAL n’est ajoutée.

`Software-qualified does not mean hardware-qualified.`
`Instrumentation does not grant production authority.`
`A short smoke test does not validate multi-day stability.`
`Synthetic providers do not qualify concrete devices.`
`No qualification output may contain cognitive or personal data.`

Aucun dispositif concret, token, invocation, action, automation, observation
active ou décision de sécurité n’est ajouté ou exécuté par cette passe.
