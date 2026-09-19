# Edge / Vision compute split V1

## Statut

Ce document fige la séparation de calcul V1. Le replay de `/home/rock/test3.mp4`
émule l’Edge sur la centrale (`edge_emulated: true`) ; il ne constitue pas une
preuve de caméra matérielle réelle.

Le flux nominal est :

```text
Caméra / Edge → Discovery → média temporaire + manifeste Edge
             → enrichissement Vision central sélectionné
             → observations sémantiques → Core → StateFrame 86D → MLP
             → Safety Gate → Universal Store → Discovery
```

Le MLP, la Safety Gate et le Store conservent leurs contrats V1. Le mode reste
`active_dry_run`; aucune action physique n’est exécutée.

## Répartition de calcul

L’Edge est propriétaire des opérations qui exigent des pixels : décodage,
détection légère, classification de trigger, tracking local borné et sélection
d’évidence. Le tracker `services/vision-worker/edge/tracker.py` conserve au plus
16 pistes locales, avec une fenêtre temporelle bornée. Ses identifiants sont
process-local et ne sont jamais une identité, un identifiant caméra ou un
contrat de bus.

Discovery accepte le média dans un cache temporaire indépendant du Store. Le
cache utilise un jeton aléatoire opaque, une taille maximale, un nombre maximal
d’entrées et un TTL ; `Cleanup` supprime les fichiers expirés. Le chemin local,
le média brut et le jeton ne sont pas propagés dans les événements Core.

La Vision centrale ne reçoit que l’évidence sélectionnée et produit des faits
sémantiques : présence, classe de sujet disponible, confiance, phase,
topologie, priorité indicative et résumé. Les enrichisseurs visage, plaque et
objets sensibles restent `not_requested` par défaut dans ce chemin. Ils ne sont
pas initialisés par le replay Edge.

Le Core assemble les observations par épisode, calcule le StateFrame V1 86D,
alimente les quatre heads MLP CPU, applique la Safety Gate et écrit le Store.
Discovery reste l’unique frontière externe ; Core reste l’unique écrivain Store.

## Manifeste `synora.vision.edge-track-manifest/v1`

Le manifeste contient uniquement :

| Champ | Règle |
| --- | --- |
| `camera_id`, `episode_id` | identifiants contractuels déjà validés par Discovery |
| `topology_class` | `public_outdoor`, `private_perimeter`, `restricted_threshold`, `protected_interior`, `unknown` |
| `trigger_class`, `trigger_confidence` | classe normalisée et confiance `[0,1]` |
| `tracking_status` | `ok`, `unavailable`, `invalid`, `contradictory` |
| `started_at`, `ended_at` | temps UTC de l’évidence |
| `track_count`, `confirmed_track_count` | agrégats sans piste locale |
| `observation_count`, `segment_count`, `gap_count` | continuité agrégée |
| `evidence_refs` | références `evidence://` opaques et déterministes, sans chemin |
| `priority_reason` | raisons déclaratives, jamais une commande |
| `edge_emulated` | vrai uniquement pour le replay central |
| `metrics` | compteurs et latences sans pixels |

Le validateur rejette récursivement `bbox`, `crop`, `embedding`, `identity`,
`hardware_id`, `serial`, `mac`, `local_track_id`, `raw_media` et les médias ou
frames bruts. Les événements JSONL écrits pour Discovery suppriment aussi
`media`, `identity`, `plate` et `sensitive_objects`.

Les événements emploient `aggregate-track-N`. Ces valeurs sont des agrégats de
replay, pas les IDs internes du tracker et pas des identités persistantes.

## Routage et retracking exceptionnel

`SYNORA_EDGE_VISION=1` sélectionne `edge-v1` dans le bridge Discovery → worker.
Pour cette voie, le worker n’alloue pas `EpisodeVisionContext` central et ne
conserve pas de tracking central entre segments. La métrique obligatoire est
`central_visual_tracking_invocations: 0`.

Un retracking central est une voie de secours seulement pour les statuts Edge
`unavailable`, `invalid` ou `contradictory`. Il est désactivé par défaut ; la
fonction de politique exige une activation explicite et expose séparément
`central_retrack_fallback_enabled` et `central_retrack_fallback_used`. Toute
activation doit rester temporaire, visible dans les traces et couverte par un
test de contrat. Le statut `ok` ne peut jamais déclencher cette voie.

## Bornes et invariants

- trois workers RKNN maximum ;
- trois frames maximum en vol ;
- ordre temporel des segments conservé par Discovery et Core ;
- files, épisodes et cache média bornés ;
- aucune frame, bbox, crop, embedding biométrique, identité, ID matériel ou
  média brut sur le bus, dans Core ou dans Store ;
- aucune décision P0 créée par Vision ;
- aucune boucle MLP → StateFrame → MLP ;
- `physical_action_executed` reste `false`.

## Mesure reproductible

La commande permanente est :

```text
make replay-edge-vision-v1 CLIP=/home/rock/test3.mp4 OUT=/tmp/synora-edge-replay-v1
```

Elle écrit le manifeste Edge, le JSONL sémantique, le rapport Vision et le
rapport Core/Store. `make replay-vision-core-v1` passe désormais par cette même
voie, et `make e2e-vision-mlp-v1` la réutilise.

Sur le replay réel observé pendant cette phase : 31 détections RKNN, 45 frames
échantillonnées, 252 ignorées par la politique d’échantillonnage, 10 segments,
5 observations, 3 pistes agrégées, 1 résumé, 3,106 ms de tracking Edge cumulé,
4,385 ms de détection, 7,738 ms de mur Vision, et 0 appel de tracking central.
Le média n’est pas transféré vers Core (`media_transferred_bytes: 0`) et le
manifeste fait 1 911 octets dans ce replay.

## Retour arrière

Le retour logiciel consiste à désactiver `SYNORA_EDGE_VISION` et à conserver le
pipeline `clip-v1` existant. Les quatre commits sont indépendants et peuvent
être revertis dans l’ordre inverse. Le retour arrière ne modifie ni modèles,
ni poids, ni contrats Store, ni StateFrame 86D.
