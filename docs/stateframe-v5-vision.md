# StateFrame V5 — Vision evidence shadow

Statut : contrat préparatoire, shadow-only, non consommé par le MLP actuel.

Ce contrat est ajouté depuis `5acd08d`. `state-encoder/v4` reste le seul
encodeur autorisé à alimenter `BuildInput`, le scheduler et les quatre heads
MLP CPU. V5 sert uniquement aux traces E2E et à une capture JSONL destinée à
un futur travail de dataset/réentraînement.

## Objectif et frontières

V5 projette des faits observés et normalisés : sécurité, présence, topologie,
priorité observée, phase d'épisode, enrichissement sans identité, continuité,
qualité/source et co-évidence structurée. Il ne contient aucune décision
teacher, aucun niveau de danger attendu, aucun identifiant d'épisode, caméra,
device ou track, aucun média, bbox, crop, embedding, identité biométrique ou
référence de média.

P0 est une priorité Core. L'encodeur refuse toute frame portant
`P0_system_critical` avec `priority_origin=vision`. La Vision ne peut donc pas
créer P0. Les observations Vision V1 émises par le replay sont P1–P4.

La chaîne duale est :

```text
faits Core + observations Vision agrégées
        ├── StateFrame V4 → StateEncoder V4 (477) → MLP CPU actuel
        └── StateFrame V5 → StateEncoder V5 (44) → traces/capture uniquement
```

Les deux branches sont calculées sur le même scénario. V5 n'est jamais passé
à `BuildInput`, au scheduler, aux heads, au Safety Gate ou à un exécuteur.
`advisory_shadow`, `dry_run`, le Safety Gate et
`physical_action_executed=false` restent obligatoires.

## Contrat canonique

Le schéma est `state-encoder/v5`, implémenté par
`synora-state-encoder/5.0.0-shadow`. Le `StateFrameV5` canonique contient :

| Groupe | Champs canoniques | Valeurs / défaut si absent |
|---|---|---|
| Sécurité | `armed`, `degraded`, `known` | booléens ; `false`, `false`, `false` |
| Présence | `human_present`, `track_count`, `track_confirmed` | booléens et entier borné ; `false`, `0`, `false` |
| Topologie | `topology_class` | `public_outdoor`, `private_perimeter`, `restricted_threshold`, `protected_interior`, `unknown` ; défaut `unknown` |
| Priorité | `priority`, `priority_origin` | P0–P4 contractuels ; défaut P4 et origine `unknown` |
| Phase | `episode_phase` | `initial`, `candidate`, `confirmed`, `final` ; défaut `initial` |
| Enrichissement | `enrichment_status` | `unavailable`, `not_requested`, `uncertain`, `recognized`, `unknown` ; défaut `unavailable` |
| Continuité | secondes depuis première/dernière observation, `segment_count`, `gap_count`, `calm_seconds` | non-négatif ; défaut zéro |
| Qualité/source | `real_detection`, `replay_simulation`, `observation_count`, `aggregate_confidence` | booléens et valeurs nulles ; défaut `false`, `false`, `0`, `0` |
| Co-évidence | `access_state`, `movement`, `sensor_evidence`, `alarm_state` | accès `unknown/closed/open/forced`, alarme `unknown/armed/disarmed/triggered`; défaut `unknown` |

Les identifiants de track peuvent être utilisés transitoirement pour calculer
la continuité et la confirmation, mais ils ne sortent jamais du projecteur V5.
`track_count` est un compte agrégé ; `track_confirmed` signifie qu'une
présence humaine est revue sur plusieurs observations ou possède l'état
d'enrichissement stable, pas qu'une identité a été reconnue.

## Dimension et ordre figé

La dimension est exactement **44 float32**. Toutes les composantes sont dans
`[0,1]`. Les catégories sont encodées en one-hot, dans l'ordre ci-dessous ;
une modification d'ordre impose une nouvelle version de contrat.

| Offset | Groupe | Features dans l'ordre | Normalisation |
|---:|---|---|---|
| 0–5 | sécurité/présence | `security.armed`, `security.degraded`, `security.known`, `presence.human`, `presence.track_count`, `presence.track_confirmed` | booléens ; tracks `/16`, saturé |
| 6–10 | topologie | `topology.public_outdoor`, `topology.private_perimeter`, `topology.restricted_threshold`, `topology.protected_interior`, `topology.unknown` | one-hot |
| 11–15 | priorité | `priority.p0`, `priority.p1`, `priority.p2`, `priority.p3`, `priority.p4` | one-hot ; P0 Core-only |
| 16–19 | phase | `phase.initial`, `phase.candidate`, `phase.confirmed`, `phase.final` | one-hot |
| 20–24 | enrichissement | `enrichment.unavailable`, `enrichment.not_requested`, `enrichment.uncertain`, `enrichment.recognized`, `enrichment.unknown` | one-hot, sans payload biométrique |
| 25–29 | continuité | `continuity.seconds_since_first`, `continuity.seconds_since_last`, `continuity.segment_count`, `continuity.gap_count`, `continuity.calm_seconds` | secondes `/300`, segments `/32`, gaps `/16`, tous saturés |
| 30–33 | qualité/source | `quality.real_detection`, `quality.replay_simulation`, `quality.observation_count`, `quality.aggregate_confidence` | booléens ; observations `/32`, confiance clampée |
| 34–43 | co-évidence | accès `unknown`, `closed`, `open`, `forced`; `movement`; `sensor`; alarme `unknown`, `armed`, `disarmed`, `triggered` | one-hot et booléens |

Les constantes et l'ordre exécutable sont dans
`internal/cognitive/stateframe_v5.go`, et le golden vector est dans
`internal/cognitive/stateframe_v5_test.go`.

## Invariants

- `CapturedAt` est UTC ; un timestamp absent devient l'époque Unix UTC pour
  garder l'encodage déterministe.
- Une catégorie inconnue est ramenée à son compartiment sûr : topologie
  `unknown`, priorité P4, phase `initial`, enrichissement `unavailable`, accès
  et alarme `unknown`.
- Les temps, comptes et confiances négatifs, NaN ou infinis sont ramenés à
  zéro lors de la normalisation ; les bornes sont saturées dans le vecteur.
- `priority_origin=vision` est compatible avec P1–P4 uniquement.
- La dimension, le dtype `float32`, le schéma et les 44 noms de features sont
  validés avant toute capture.
- La commande E2E vérifie la stabilité de deux encodages successifs, la
  stabilité de l'empreinte canonique, l'absence de tokens interdits et
  `physical_action_executed=false`.

## Co-évidence et absence de fuite sémantique

`access_state` est dérivé de la topologie et du contexte d'accès observé ;
`movement` du trigger mouvement ; `sensor_evidence` de la présence d'un
trigger structuré ; `alarm_state` du fait Core armed/disarmed/triggered.
Ces champs ne consultent ni la décision teacher, ni les logits MLP, ni le
niveau de danger attendu. Les champs teacher sont voisins de V5 uniquement
dans la capture, comme cible déterministe future, jamais dans le vecteur.

## Capture JSONL

Le fichier `stateframe-v5-capture.jsonl` utilise le schéma
`synora.stateframe-v5-capture/v1`. Chaque ligne contient :

- `state_encoder_schema`, version, dimension et `feature_order` ;
- l'état canonique sans identifiants ni média ;
- le vecteur V5 de 44 valeurs et son empreinte SHA-256 ;
- l'empreinte du vecteur V4 correspondant et sa dimension 477 ;
- l'évidence Vision agrégée : présence, nombre de tracks, confirmation,
  observations, segments, gaps, détection réelle, replay et confiance ;
- la sortie teacher déterministe ;
- la provenance `segmented_vision_replay`, le mode `advisory_shadow` et la
  confirmation d'absence d'action physique.

La capture est écrite après le calcul MLP, sur une file/fichier E2E hors chemin
de décision. Le writer refuse les clés ou tokens correspondant à bbox, crop,
embedding, biométrie, identité, image brute, track/caméra/device/épisode ou
média vidéo/image.

## Compatibilité

`state-encoder/v4` reste inchangé : schéma `state-encoder/v4`, version 4.0.0,
477 features, même ordre, même manifest runtime et mêmes poids. Aucun modèle
`.pt`, ONNX, JSON CPU, manifest cognitif ou ordre V4 n'est modifié. Le runtime
MLP actuel continue d'exécuter V4 ; V5 ne peut pas être sélectionné par
configuration dans cette phase.

Une future activation de V5 nécessitera un nouveau manifest, des poids et une
validation de compatibilité séparés. Aucun fallback silencieux de V4 vers V5
n'est autorisé.

## Fixtures et commande

`testdata/stateframe-v5/` couvre : état normal sans Vision, humain en
`private_perimeter`, humain au `restricted_threshold`, humain en
`protected_interior`, candidat multi-segments, confirmé multi-segments, fin
d'épisode calme, gap de segments, détecteur indisponible, enrichissement
indisponible et P0 Core-only.

Les fixtures restent disponibles pour l’encodeur V5 et le bloc
`VisionEvidence[44]`. La commande Core V1 est :

```text
make e2e-cognitive-core-v1
```

Elle vérifie que V4 reste inchangé, que le bloc V5 ne transporte aucune donnée
interdite, que le snapshot complet est encodé par le Core et qu’aucune action
physique n’est exécutée.
