# Fondation contractuelle du Triple MLP cognitif V1

## Portée du jalon

Cette passe fige des contrats sémantiques redacted et des projections déterministes depuis le Store. Elle ne crée aucun poids, n’entraîne ni n’exécute les trois nouveaux MLP, et ne les raccorde pas à la décision active. Le runtime nominal V1 et le candidat V3 `active_dry_run` restent inchangés ; le Triple MLP est une fondation contractuelle, pas un chemin parallèle actif.

## Les trois rôles

- `EventCorrelationMLP` recevra un `EventFrame V1`, qui décrit les faits d’un même instant logique, leurs sources, leur qualité, la sûreté, la présence agrégée, la topologie et les co-évidences.
- `ContextCorrelationMLP` recevra un `ContextFrame V1`, construit à partir du ledger d’épisode et du Universal Store. La récurrence, la persistance, les gaps, la reprise, les transitions de zones et les résultats d’action sont explicites dans son entrée ; le modèle n’a pas de mémoire temporelle implicite.
- `DecisionArbiterMLP` recevra uniquement un `ArbitrationFrame V1` constitué des sorties sémantiques des deux premières branches et de contraintes Core redacted. Il ne reçoit pas les faits Event/Context bruts, ne peut pas contourner les masques de disponibilité et ne commande jamais directement un périphérique.

Les contrats sont `synora.cognitive.event-frame/v1`, `synora.cognitive.context-frame/v1`, `synora.cognitive.arbitration-frame/v1` et `synora.cognitive.triplet-bundle/v1`. L’ordre des champs est défini par les structs contractuels et les fixtures golden. Les vecteurs d’embedding sont bornés, mais leur dimension n’est volontairement pas figée.

## Sûreté et états dégradés

`unknown`, `unavailable`, `degraded` et `blocked` sont distincts. Une branche indisponible porte un masque explicite et aucun embedding : elle n’est jamais remplacée par un vecteur nul ambigu. Une indisponibilité Vision ne constitue pas une absence. Les données spatiales sont des relations de zones, pas des pixels, trajectoires ou coordonnées. `confirmed` relève du cycle de vie déterministe et ne peut venir d’une confiance MLP seule.

Le Safety Gate reste seul responsable des permissions, capacités, cooldowns et refus. Les encodeurs ne produisent que des entrées redacted. Les sorties arbiter futures devront être validées puis converties en intention abstraite avant Safety Gate. Les actions demeurent en dry-run dans les tests.

## Bundle, labels et compatibilité

Le bundle lie les hashes des trois manifests de modèle, des trois contrats de frames, de la taxonomie de labels et du manifest de calibration, avec une version de compatibilité commune. Tout hash manquant, contrat différent ou version mêlée doit le faire refuser. La taxonomie future comprend `danger`, `incident`, `task`, `action` et `communication_intent`; ses valeurs restent fermées et versionnées. Aucun bundle ni modèle n’est généré dans ce jalon.

## Harnais et migration

La famille `triplet_cognitive_contract_v1` construit et valide trois frames à partir d’un journey central réellement persisté, puis rejoue les constructeurs pour vérifier leur déterminisme. Ces frames restent des observations de contrat et ne sont jamais passées au runtime V3. Chaque cas continue d’exercer le chemin actif central existant ; sa dimension 86D et ses sorties sont vérifiées avant la projection Triple MLP. Les scénarios simulés/replay restent étiquetés et ne prétendent pas constituer un corpus réel.

La migration future se fera après constitution d’un corpus versionné, définition empirique des dimensions et évaluations indépendantes : d’abord entraîner/qualifier les branches Event et Context, puis l’Arbiter, enfin proposer un bundle tripartite candidat. Le déploiement ne pourra remplacer V3 qu’après validation, comparaison, approbation et maintien du Safety Gate comme autorité d’exécution.

## Frontière OEM/ODM

Les modèles Vision restent intégrés au cas par cas par un OEM, derrière les modules fonctionnels et adaptateurs existants. Le Core cognitif ne connaît ni matériel, modèle Vision, tenseur, chemin d’artefact ni driver ; cette passe n’ajoute aucun registre générique d’exécution de modèles.

## Figé et reporté

Figés : versions des quatre contrats, sémantique et ordre canonique des groupes, vocabulaires fermés, limites de charge, règles d’indisponibilité/redaction, chaînage des hashes et frontières entre les branches.

Reportés : dimensions exactes des embeddings, normalisation apprise, seuils, taxonomie détaillée d’incidents/tâches, corpus représentatif, poids, calibration, métriques de qualification et promotion. Prochain jalon unique : concevoir le corpus et déterminer les dimensions vectorielles des trois MLP à partir d’évaluations indépendantes.
