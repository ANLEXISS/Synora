# Audit et nettoyage V1

Date de l'audit : 2026-09-18. Périmètre exclusif : `feature/synora-architecture-freeze-v1`, depuis `5e83381`.

## Verdict

Le replay réel de `/home/rock/test3.mp4` conserve la même sortie fonctionnelle avant/après : mêmes cinq observations utiles, mêmes transitions `candidate → confirmed`, mêmes priorités, mêmes quatre tracks, mêmes quatre sorties teacher/MLP shadow et un seul résumé final. Le bus ne contient toujours aucune donnée Vision brute et aucune action physique n'est exécutée.

Le bénéfice mesuré provient principalement de la non-initialisation du chemin visage lorsqu'il n'est pas explicitement activé, ainsi que de la non-conservation des ROI en mémoire dans le pipeline sans enrichisseur visage configuré.

## Chemins actifs vérifiés

Le hot path est le suivant :

```text
clip / segment vidéo
  → replay_segments_v1.py
  → worker.py / clip.process
  → VisionClipPipelineV1.process_video
  → detector_backend.py / trois runners RKNN maximum
  → tracker et EpisodeVisionContext
  → observations versionnées + résumé final
  → bus Core / EventStore / Engine
  → synora-vision-mlp-e2e
  → StateFrame déterministe v4
  → quatre heads CPU : danger, incident, task, action
  → Safety Gate et traces advisory_shadow
```

Constats factuels :

- Le replay OpenCV lit 297 frames, en échantillonne 50 et en ignore 247. Les segments sont traités dans l'ordre.
- `ThreePinnedDetectorBackend` conserve au plus trois runners RKNN et `detect_many` accepte au plus trois frames à la fois. Le benchmark mesure trois frames maximum en vol.
- Le tracking et les transitions d'épisode sont produits par `VisionClipPipelineV1`; le runtime émet les observations puis un résumé final unique.
- Le worker ne sérialise vers le bus que les contrats d'observation et de résumé. Les contrôles d'absence de `bbox`, `crop`, `embedding`, `face_image` et `raw_frame` passent.
- Le runtime MLP reconstruit le StateFrame à partir du contrat, appelle les quatre heads CPU et envoie uniquement des propositions au Safety Gate. Le résumé confirme `cognitive_mode=advisory_shadow` et `physical_action_executed=false`.
- Le replay active explicitement `SYNORA_VISION_FACE_ENABLED=0`, `SYNORA_VISION_PLATE_ENABLED=0` et `SYNORA_VISION_SENSITIVE_OBJECTS_ENABLED=0`. Le mode debug HTTP est désactivé dans ce chemin.

## Preuves de code mort et doublons

Une recherche exhaustive des références du worktree ne trouvait aucune référence code, test, Makefile ou documentation active vers :

- `services/vision-worker/core/async_pipeline.py`, ancien sketch asynchrone générique, supprimé ;
- `services/vision-worker/core/pipe.py`, ancienne implémentation de pipeline sans référence, supprimée ;
- `tools/cognitive/rknn_report.py`, ancien rapport RKNN sans référence, supprimé.

Le document `services/vision-worker/docs/pipeline_debug.md` a été corrigé pour décrire le backend réellement utilisé. `services/vision-worker/tools/vision_inference_benchmark.py` a été conservé car il est référencé par le Makefile et par `test_inference_benchmark.py`. `core.pipeline`, `FaceRecognizer` et `face_dataset` ont été conservés car le chemin visage explicite et plusieurs tests les référencent.

## Nettoyage et optimisations appliqués

1. Les modèles et services visage ne sont plus importés ni initialisés par défaut. Le chemin ne s'active que si `SYNORA_VISION_FACE_ENABLED=1`. Les enrichisseurs plaque et objets sensibles restent désactivés et déclarés comme différés ; aucun backend, RAM ou NPU correspondant n'est initialisé.
2. Quand aucun enrichisseur visage configuré ne requiert de ROI, le pipeline ne conserve plus les vues de frames dans les tracks. Les références opaques de contrat restent inchangées.
3. L'enrichisseur objets sensibles indisponible ne fait plus d'appel de méthode inutile à chaque détection ; il produit directement le même statut `not_available`.
4. Les réponses IPC JSON utilisent une sérialisation compacte et refusent les valeurs non JSON. Cette réduction est hors calcul Vision et ne modifie pas les contrats décodés.
5. Les trois chemins morts prouvés ci-dessus ont été supprimés. Aucun contrat V1, modèle `.pt`, State Encoder v4, caméra réelle ou action physique n'a été modifié.

Les limites architecturales sont conservées : trois workers RKNN maximum, trois frames maximum en vol, ordre logique des segments, files bornées par les contrats/runtime, P0 Core, P1 Vision non décisionnel et absence de boucle MLP → StateFrame → MLP.

## Mesure reproductible

Le baseline initial a été conservé avant les changements dans `/tmp/synora-perf-v1-before-20260918-1`. Le comparatif final est `/tmp/synora-perf-v1-after-20260918-2/perf-baseline.json`, produit par `make perf-baseline-v1` avec `/home/rock/test3.mp4` et le runtime CPU MLP existant.

| Métrique | Avant | Après | Delta après - avant |
|---|---:|---:|---:|
| latence première observation | 414.856 ms | 410.196 ms | -4.660 ms |
| latence candidat | 401.204 ms | 401.204 ms | 0 ms |
| latence confirmé | 1003.010 ms | 1003.010 ms | 0 ms |
| temps mur Vision | 416.190 ms | 413.551 ms | -2.639 ms |
| coût cumulé détecteur | 6121.625 ms | 5935.848 ms | -185.777 ms |
| temps mur du processus | 60692.701 ms | 51507.649 ms | -9185.052 ms |
| RSS maximale enfants | 682.168 MB | 635.152 MB | -47.016 MB |
| frames lues / échantillonnées / ignorées | 297 / 50 / 247 | 297 / 50 / 247 | identique |
| maximum frames en vol | 3 | 3 | identique |
| observations / segments / tracks / résumés | 5 / 10 / 4 / 1 | 5 / 10 / 4 / 1 | identique |

Latences des heads MLP CPU :

| Head | Avant | Après |
|---|---:|---:|
| danger | 0.541 ms | 0.538 ms |
| incident | 0.769 ms | 0.782 ms |
| task | 0.876 ms | 0.874 ms |
| action | 1.004 ms | 0.983 ms |

Le rapport JSON vérifie aussi : sortie fonctionnelle identique, transitions identiques, teacher/MLP identiques, résumé final unique, bus sans données brutes, `advisory_shadow=true`, `physical_action_executed=false` et maximum de trois frames en vol.

## Validation et rollback

Les validations finales sont exécutées depuis ce worktree : `make e2e-v1`, replay segmenté réel de `/home/rock/test3.mp4`, `make e2e-vision-mlp-v1`, `make perf-baseline-v1`, `go test ./...`, `go vet ./...`, `go build -buildvcs=false ./...`, tests Python et `git diff --check`.

Le nettoyage est isolé dans le commit `refactor: remove dead paths and reduce V1 hot-path overhead`. Le rollback complet est donc `git revert <commit-de-nettoyage>` depuis ce worktree. Les suppressions peuvent aussi être restaurées individuellement par le même revert ; le commit de baseline MLP et le commit d'architecture figée restent séparés.

## Risques restants

- Le chemin visage explicite conserve des dépendances lourdes et doit rester opt-in ; il n'est pas couvert par le replay shadow standard.
- Plaque/OCR et objets sensibles restent indisponibles par conception V1.
- La RSS est la valeur maximale des processus enfants et le temps mur inclut l'export/compilation de l'E2E ; ces chiffres ne sont pas une mesure caméra continue.
- Le backend RKNN Vision reste dépendant de la disponibilité matérielle des trois cœurs ; le MLP demeure CPU-only et advisory.
