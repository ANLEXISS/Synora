# Versioning, replay et migration

## Versions

Chaque contrat durable porte un ID de forme `...vN`. Les stores portent une
version de schéma indépendante, par exemple `synora.cge.workflow-record.v1`,
`synora.cge.workflow-checkpoint.v1` ou `calibration-ledger-record-v1`.
Les fingerprints, checksums et hash chains ne remplacent pas un numéro de
version : ils vérifient l’intégrité d’une version donnée.

Une modification compatible peut ajouter un champ optionnel, conserver les
invariants de protection, ne pas changer la sémantique d’un timestamp et
préserver le replay. Une modification breaking change supprime/renomme un
champ, change son type, son domaine d’identifiant, son unité temporelle, son
ordre ou son autorité. Elle exige un nouvel ID `vN+1`, une stratégie de
migration et des tests de replay.

## Replay

Les anciens records valides doivent rester acceptés par les replayers actuels.
Les nouveaux records sont écrits selon la version courante. Les journaux
existants ne sont pas réécrits automatiquement ; un ancien fichier peut donc
rester sensible. Le recovery valide d’abord checkpoint, checksum, fingerprint,
séquence et policy, puis rejoue le WAL/journal selon la politique du store.

Le replay ne republie pas d’événement historique dans Core, ne produit pas
d’action et ne déclenche pas d’append ledger opportuniste. Une migration future
devra prouver la stabilité des séquences, révisions, digests, pseudonymes et
marqueurs d’autorité.

## Stabilité

`stable` signifie que le contrat est suffisamment défini pour les consommateurs
documentés. Les modèles cognitifs encore expérimentaux restent marqués
`experimental`; les frontières et objets internes peuvent être `internal`.
Une dépréciation doit préciser une fenêtre, les consommateurs restants et le
plan de migration dans une mise à jour du catalogue.
