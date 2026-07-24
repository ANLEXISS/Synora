# Versioning, replay et migration

Le jeu complet gelé est `configs/cge/contracts/baselines/cge-contract-set-v1.json`.
Il est immuable dans Git et `freeze-baseline` refuse de le remplacer. Les
commandes `generate`, `check`, `check-compat` et `coverage` sont les gates de
génération et de revue ; `check-compat` ne modifie aucun fichier et classe les
différences en `compatible`, `migration_required` ou `breaking`.

`generate` produit uniquement le registre Go et l'inventaire de découverte.
L'inventaire n'est pas une baseline et ne vaut pas mapping approuvé.
La migration approuvée vers `cge-contract-set-v2.json` est décrite dans
`configs/cge/contracts/migrations/contract-set-v1-to-v2.yaml`. La v1 et la v2
sont immuables ; `freeze-baseline-v2` refuse tout écrasement.

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

`check-compat` retourne zéro pour `compatible` et pour une migration
`migration_required` approuvée, et retourne une erreur pour une migration
manquante ou une rupture. `check-compat --baseline v2` doit être
`classification=compatible` pour l'ensemble courant.

## Processus contractuel obligatoire

1. Ajouter l'ID versionné, son propriétaire, son autorité, sa sensibilité, ses
   stores et son implémentation dans le catalogue.
2. Ajouter le type à `go-surfaces.yaml`, les champs wire et les tests de
   frontière/store nécessaires.
3. Exécuter `go run ./cmd/cge-contractgen generate`, puis `check`.
4. Pour une modification compatible, conserver l'ID, n'ajouter que des
   champs optionnels et mettre à jour la compatibilité/replay.
5. Pour une modification breaking, créer un nouvel ID, une migration explicite
   et des fixtures legacy ; aucune réécriture automatique n'est permise.
