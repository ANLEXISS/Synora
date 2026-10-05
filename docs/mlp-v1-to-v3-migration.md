# Architecture cognitive V1 → V3

Audit du 5 octobre 2026 sur `/home/rock/Synora-v1`, branche `master`. Les
modifications locales préexistantes ont été conservées. Aucun fichier de
`/opt/synora/models` ou `/var/lib/synora` n’a été modifié.

## Statut et ordre d’exécution

V1 reste le bundle nominal et le seul bundle promu. Son manifest et ses
artefacts ont été vérifiés avant/après l’audit :

```text
build/cognitive-mlp-v1/MANIFEST.v1.json  4351645334a3cb290c806d2cdcd6d1613b97f1749a65c92052ea0568e1f97a59
danger.cpu.json                          09e954f8483cbdbba54703d6477d3ac8e08d0ef881e7921aeebf822af117b2e0
incident.cpu.json                        d2fe24985283913f95e9f3d0c05bf0a05b16e362ff38a9d4fe0dfb183550b9a0
task.cpu.json                            0fb637b981eed695df8b67ada5f3a8e140720835d8820321794337019b2f595c
action.cpu.json                          ec1a1a3405e39a17e65fc0bdc8eb69f6b743213d1f50efc7b29665567f4e2dd2
```

Le chemin candidat unique est :

```text
Discovery → enrichissement Vision Edge/central → CognitiveSnapshot V3
          → MLP CPU V3 (danger, incident, task, action, communication_intent)
          → Safety Gate → Universal Store → Discovery
```

V3 est `active_dry_run`, `promoted=false`, `physical_action_executed=false` et
`audio_rendered=false`. V2 n’a plus de sélecteur, commande, événement,
injecteur ou Core runtime permanent. Ses corpus et rapports restent uniquement
comme provenance d’apprentissage et comparaison historique.

## Contrat V3

`CognitiveSnapshotV3` est `cognitive-snapshot/v3`, encodé par
`cognitive-encoder/v3`, dimension fixe `[1,86]`, version `3.0.0`. Les valeurs
0–63 sont le préfixe V2 immuable ; les positions 64–85 sont, dans l’ordre :

```text
64–67 pose unavailable/not_requested/low_quality/available
68–71 posture unknown/upright/seated/ground
72–75 fall none/candidate/confirmed/unknown
76–80 risk persistence none/isolated/repeated/persistent/confirmed
81 risk persistence seconds, 82 risk observation count, 83 risk quality sufficient
84 pose observation count, 85 pose sampled
```

La définition unique de `immobility_seconds` est la durée normalisée
d’immobilité d’un humain confirmé, quelle que soit sa posture. `posture=ground`
porte l’état au sol ; il n’existe pas de feature `ground_duration` et aucune
conversion ambiguë n’est effectuée. Une transition upright/seated → ground,
une qualité suffisante et l’absence de récupération permettent au plus
`fall_state=candidate`. `confirmed` reste non qualifié.

Les agrégats V3 complémentaires restent hors de la dimension encodée fixe
86D, car aucun offset V3 réservé ne leur est attribué : `ground_duration`,
les tiers de mouvement, `physical_interaction_candidate`, les agrégats face
(`face_status`, consensus, qualité, confiance, expiration et provenance) et
la santé/intégrité caméra. Leur présence dans `CognitiveSnapshotV3` est
contractuelle et agrégée, mais elle ne change ni l’ordre ni la dimension du
vecteur ; une extension d’offset exigerait une nouvelle version d’encodeur.
Le champ historique `fall.confirmed` reste dans l’ordre immuable pour la
parité de contrat, mais toute entrée V3 qui le produit est rejetée.

Le contrat et les fixtures sont agrégés uniquement : aucun média, frame, bbox,
crop, embedding, identité, identifiant local ou identifiant matériel ne passe
dans le bus ou le Store.

## Modèles et limites

Sans backend qualifié, pose et risque restent explicitement `unavailable` ou
`not_available`. RTMPose-s conserve son manifest, son backend RKNN, ses scripts
et ses tests ; il n’y a aucun fallback YOLO-pose. Le backend face est local à
Vision et ne transmet qu’un agrégat ; aucune identité, embedding ou
`local_track_id` ne franchit la frontière Core. Dans l’environnement audité,
aucun artefact RTMPose RKNN utilisable ni backend face qualifié n’est
disponible. V3 ne peut donc pas être présenté comme un modèle réel de chute,
de visage ou d’arme, et aucune métrique Vision réelle indépendante n’est
revendiquée.

## Communication et sécurité

Les cas à risque incertain, suspecté, confirmé, critique ou de qualité
insuffisante apprennent `communication_intent=none` et `action != announce`.
La red team V3 causale comporte 256 exemples immuables : zéro violation, zéro
`announce` interdit et taux de faux `announce` nul. Le Safety Gate est
indépendant de la sortie MLP : il bloque une proposition `announce` interdite,
sans la réécrire silencieusement, et conserve `requested_action` pour l’audit.
Tout rendu audio et toute action physique restent désactivés.

## Qualification et promotion

La seule procédure de qualification est `make qualify-cognitive-v3`. Elle
génère les splits, entraîne V3, exporte les cinq heads, vérifie les hashes,
évalue validation/test indépendant, red team, parité d’export, absence de
données brutes et les garde-fous dry-run. Le rapport canonique est
`/home/rock/Synora-learning/build/cognitive-v3-qualification.json`.

La qualification actuelle reste bloquée parce que les backends pose/risque et
les métriques Vision réelles sont absents, malgré les cinq heads exportées et
la red team à zéro violation. Une promotion future exigerait en plus des
artefacts reproductibles, une validation RKNN/CPU indépendante, des métriques
réelles annotées, une revue des limites et une décision explicite ; aucune
promotion automatique n’existe.

L’injection canonique est `make test-central-v1 BUNDLE=v3`. Elle utilise les fixtures
V3, le bundle V3 candidat, le mode `active_dry_run` et n’exécute ni physique ni
audio. Il n’existe pas de V4, de nouvelle famille runtime ou de couche
d’observation ajoutée.
