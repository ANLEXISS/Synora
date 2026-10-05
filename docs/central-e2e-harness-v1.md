# Central E2E harness V1

Le point d’entrée système officiel est `make test-central-v1`. Le harness est
hermétique, déterministe et s’exécute dans un processus de test avec un bus
Unix temporaire :

```text
camera-simulator
    -> message Edge versionné sur le vrai bus Unix
    -> Discovery réel, mode bus-only
    -> Core réel (V1 nominal ou V3 active_dry_run)
    -> encodeur + chargeur MLP CPU réel
    -> Safety Gate réel, dry-run
    -> Universal Store réel sur disque temporaire
    -> outbox/action result via Discovery
```

Aucune caméra, aucun clip, aucun NPU/RKNN, aucun socket réseau, aucun chemin
`/opt/synora` et aucun Store persistant ne sont ouverts par ce harness. Les
capacités d’action et de rendu sont des gardes négatives : toute exécution
physique, audio ou réseau rend le scénario invalide.

## Commandes

```bash
make test-central-v1
make test-central-v1 CASE=human-interior-armed
make test-central-v1 BUNDLE=v1
make test-central-v1 BUNDLE=v3
make test
```

Le rapport est écrit par défaut dans `/tmp/synora-central-e2e-v1.json` et ne
contient que les types de messages, les statuts, les révisions et les hash de
payload. Il ne recopie aucun payload Vision.

Le chargeur MLP réel exécute toutes les heads, mais les mesures de temps
internes au MLP sont normalisées à zéro dans ce test hermétique afin que les
payloads de décision et leurs hash soient identiques à chaque exécution. La
latence opérationnelle se mesure séparément sur le backend concerné.

La suite versionnée est dans `testdata/central-e2e-v1/`. Elle contient 99
scénarios déclaratifs, un seed et une date logique fixes, deux suites JSONL
immutables (`reference-suite.jsonl`, `redteam-suite.jsonl`) et un minimum
déclaré de 80 cas. Les scénarios couvrent l’ingress Edge, les séquences,
doublons et gaps, topologies, cas invalides/red-team, modèle indisponible,
matrice V3 pose/posture/chute candidate/récupération, communications et
multi-caméras.

Pour ajouter un cas, ajouter une règle dans
`tools/generate_central_fixtures.py`, régénérer avec ce script, puis vérifier
le diff des fixtures et le manifest. Les fixtures ne sont pas un corpus
d’apprentissage et ne contiennent pas de média brut, bbox, crop, keypoint,
embedding, identité ou `local_track_id` autorisé vers le bus ou le Store.

Les assertions centrales vérifient notamment :

- l’acceptation ou le rejet Discovery et l’absence de passage Core pour les
  ingress interdits ;
- la version et la dimension du snapshot, l’ordre des heads MLP et la
  persistance Universal Store ;
- l’absence de données Vision brutes dans les contrats et traces observés ;
- `physical_action_executed=false`, `audio_rendered=false` et l’absence de
  réseau ;
- les sorties V1/V3 et leur Safety Gate sans promotion de bundle.

Les scénarios V3 valident des signaux agrégés synthétiques. Un état
`fall_state=candidate` n’est pas une qualification de chute et aucun état
`confirmed` n’est produit par ce harness.

## Migration des anciens lanceurs

Le harness central remplace les parcours système concurrents qui combinaient
des tests directs Core, des replays Edge et des injecteurs V3. Les tests
unitaires et contractuels restent utiles et sont conservés dans leurs paquets;
ils ne constituent pas un second chemin système officiel. Les artefacts et
scripts RTMPose, les bundles V1/V3 et le corpus d’apprentissage restent hors
de cette migration.

Les anciens parcours qui utilisaient une vraie vidéo restent explicitement
hors du harness central : ils sont réservés au smoke test RTMPose ou aux
mesures de performance séparées. `/home/rock/test3.mp4` ne peut démontrer ni
détection ni qualification de chute.

La migration a supprimé les anciens points d’entrée et leurs références
sortantes :

| Supprimé | Remplacement | Impact vérifié |
| --- | --- | --- |
| `e2e-v1`, `e2e-vision-mlp-v1`, `e2e-cognitive-core-v1`, `e2e-store-compaction-v1` | `make test-central-v1` et tests de composants conservés | le bus réel, Discovery, Core, MLP, Safety Gate et Store sont exercés par les 99 cas |
| `replay-edge-vision-v1`, `replay-vision-core-v1`, `cmd/synora-v1-replay`, `replay_*_v1.py` | fixtures Edge sérialisées et `test-central-v1` | plus d’accès clip/caméra dans le runner système |
| `inject-edge-clip`, `inject-vision-v3`, `cmd/synora-inject-edge-clip`, injecteur HTTP API | caméra simulée sur bus Unix | plus de raccourci direct Core ni de route HTTP d’injection |
| fixtures `testdata/cognitive-v1/scenarios.jsonl` et scénarios injecteur V3 | `testdata/central-e2e-v1/{manifest.json,cases/,immutable/}` | attentes déclaratives centralisées, seed fixe et red-team versionnée |
| scripts de comparaison qui dépendaient des replays (`perf_baseline_v1.py`, `first_observation_latency_v1.py`) | `cognitive-runtime-benchmark-v1` pour le runtime seul | aucune suppression de bundle, de corpus d’apprentissage ou d’artefact RTMPose |

L’audit entrant a trouvé le qualifieur externe historique sous
`/home/rock/Synora-learning/tools/qualify_cognitive_v1.py`; il n’est plus
appelé par le dépôt. `make qualify-cognitive-v1` pointe désormais vers la
suite centrale V1. Les tests unitaires Go/Python de composants restent dans
leurs paquets et sont exécutés par `make test`.

## Limites et non-objectifs

- V1 reste le bundle nominal ; V3 reste `active_dry_run` candidat.
- Aucun bundle n’est promu et aucun binaire modèle n’est ajouté à Git.
- Le harness ne restaure ni CGE, ni V4, ni shadow runtime, ni API/webapp
  legacy.
- Aucun son, rendu TTS, envoi réseau ou action physique n’est effectué.
- La latence mesurée ici est celle du chemin hermétique CPU et ne remplace pas
  une mesure RTMPose RKNN sur RK3588.
