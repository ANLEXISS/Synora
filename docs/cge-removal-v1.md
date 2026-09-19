# Retrait définitif des anciens chemins cognitifs

Le Core V1 ne dépend d’aucun ancien package de décision, serveur séparé,
client interne ou RPC historique. La purge finale a supprimé les packages,
outils et fixtures qui les chargeaient. Cette note est conservée uniquement
comme trace de migration.

## Critère d’architecture

Le test d’architecture inspecte `cmd/synora-core` et
`internal/cognitivecore`. Il échoue si un ancien marqueur ou une seconde source
de danger/action est réintroduit dans le runtime actif.

## Désactivation sûre

Un modèle V1 absent ou incompatible ne déclenche aucune heuristique de secours.
Le Core démarre en `active_dry_run`, enregistre la décision indisponible et
conserve les événements pour capture future.

## Rollback

Les commits de la branche sont séparés par responsabilité. Le rollback se fait
par `git revert <commit>` dans la branche d’intégration ou par reconstruction
du worktree depuis `cfb3a1d`; les worktrees source et stable ne sont jamais
modifiés.
