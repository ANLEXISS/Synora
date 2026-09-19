# Incident V2 — amélioration isolée

Le corpus indépendant a d’abord été audité par classe, topologie, danger, phase, gaps, disponibilité Vision et résultat d’action. Les erreurs dominantes de l’ancien incident concernaient les attentes `threshold_presence` (454 erreurs, principalement prédites `interior_intrusion` ou `anomaly`) et `perimeter_presence` (378 erreurs, prédites `routine_presence`).

La cause mesurable était une sérialisation Python des catégories par slice de liste : les one-hot étaient absents du corpus alors que le State encoder Go les produit. Incident-v2 reconstruit les mêmes 86 features dans le même ordre avec la révision `categorical-one-hot-v2`, en réutilisant les splits disjoints et les scénarios déclaratifs. Aucun label n’est dérivé d’une sortie de modèle ; les labels et l’action attendue restent ceux des scénarios.

Résultat de la comparaison sur le même split indépendant corrigé : ancien incident `83,36 %`, incident-v2 `100,00 %`. Les heads `danger`, `task` et `action` sont byte-identiques ; la dimension reste 86, le manifest reste V1 et l’ordre de features ne change pas. Le rapport détaillé et la matrice de confusion sont générés dans `build/cognitive-mlp-v1-incident-v2/incident-v2-report.json`.

La promotion n’a lieu que si l’accuracy dépasse 92 %, progresse, les trois autres artefacts restent identiques et les données interdites sont absentes. Le runtime reste CPU, `active_dry_run`, Safety Gate et sans action physique.

Rollback : restaurer `build/cognitive-mlp-v1/incident.cpu.json` depuis le bundle V1 précédent puis régénérer le manifest avec `tools/cognitive_v1_pipeline.py package-model`. Les poids des autres heads ne sont jamais réécrits.
