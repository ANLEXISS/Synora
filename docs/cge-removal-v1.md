# Retrait de CGE du runtime V1

Le nouveau Core V1 ne dépend d’aucun package CGE, Engine décisionnel,
configuration teacher/shadow ou automation de danger. La suppression porte
d’abord sur le point d’entrée runtime et ses tests de décision ; les packages
CGE historiques non appelés par le runtime restent conservés dans cette étape
tant qu’une recherche de références ne prouve pas qu’ils peuvent être retirés
sans casser les outils de migration et les contrats historiques.

## Critère d’architecture

Le test d’architecture inspecte `cmd/synora-core` et
`internal/cognitivecore`. Il échoue si ces chemins importent ou chargent CGE,
`internal/engine`, teacher, shadow, `advisory_shadow`, ou une seconde source de
danger/action.

## Désactivation sûre

Un modèle V1 absent ou incompatible ne déclenche aucune heuristique de secours.
Le Core démarre en `active_dry_run`, enregistre la décision indisponible et
conserve les événements pour capture future.

## Rollback

Les commits de la branche sont séparés par responsabilité. Le rollback se fait
par `git revert <commit>` dans la branche d’intégration ou par reconstruction
du worktree depuis `cfb3a1d`; les worktrees source et stable ne sont jamais
modifiés.
