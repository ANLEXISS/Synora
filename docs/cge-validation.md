# Validation CGE contrôlée

Les événements de validation sont des injections administrateur volontaires
dans le pipeline Core/CGE/automations. Ils ne passent pas par les scénarios de
simulation et ne créent pas de faux snapshots : chaque événement est marqué
`source_type=validation`, `metadata.validation=true` et
`metadata.test_mode=controlled_real_test`.

`learn` vaut `false` par défaut. Une validation reste visible et peut recevoir
un feedback administrateur, mais elle ne renforce pas la mémoire critique. Avec
`learn=true`, la chaîne peut alimenter la mémoire et les corrections « cas
similaires futurs » peuvent influencer des évaluations comparables.

La route `POST /api/cge/validation/chain-sequence` accepte les alias
`motion.detected`, `weapon.detected` et `fall.detected`, normalisés vers les
types CGE `vision.motion`, `vision.weapon` et `vision.fall`. Une séquence partage
un `validation_id`, un `activation_id` et une `sequence_key`, et conserve son
index dans `event_index`/`clip_index`.
