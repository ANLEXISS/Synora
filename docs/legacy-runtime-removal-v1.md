# Retrait des chemins runtime legacy V1

Le runtime actif est limité aux modes `disabled`, `active_dry_run` et `active_armed`. Le Core V1 utilise actuellement `active_dry_run`; la porte de sécurité reste déterministe et aucune exécution physique n’est disponible.

Supprimés de ce lot :

- `internal/coreclient` et `internal/rpc`, exclusivement utilisés par l’ancien serveur API pour ses RPC et ses mutations directes ;
- les références de service `synora-api` dans le manager, la santé de démarrage, les scripts d’installation et les scripts de contrôle ;
- les chemins de configuration runtime CGE et le feature flag de validation CGE ;
- les anciens fixtures de catalogue qui ciblaient `cmd/synora-api` et `internal/rpc`.

La purge finale a supprimé les anciens packages, outils de catalogue et
fixtures qui les chargeaient. Cette note est conservée comme historique de
migration ; aucun import actif du Core, de Discovery ou du replay V1 ne les
charge.

Les contrats V1 et les poids `.pt` restent inchangés par cette migration.

Rollback : revert du commit `refactor: remove remaining legacy runtime paths`, puis reconstruction/test des outils historiques concernés. Aucun fichier stable hors ce worktree n’est requis.
