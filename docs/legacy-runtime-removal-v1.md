# Retrait des chemins runtime legacy V1

Le runtime actif est limité aux modes `disabled`, `active_dry_run` et `active_armed`. Le Core V1 utilise actuellement `active_dry_run`; la porte de sécurité reste déterministe et aucune exécution physique n’est disponible.

Supprimés de ce lot :

- `internal/coreclient` et `internal/rpc`, exclusivement utilisés par l’ancien serveur API pour ses RPC et ses mutations directes ;
- les références de service `synora-api` dans le manager, la santé de démarrage, les scripts d’installation et les scripts de contrôle ;
- les chemins de configuration runtime CGE et le feature flag de validation CGE ;
- les anciens fixtures de catalogue qui ciblaient `cmd/synora-api` et `internal/rpc`.

Conservés volontairement hors runtime : `internal/cge`, `internal/engine` et `internal/eval` sont encore compilés par des outils de catalogue, de qualification ou de migration. Aucun import actif du Core, de Discovery ou du replay V1 ne les charge ; le test d’architecture échoue si un import ou un marqueur de mode legacy réapparaît dans ces racines runtime.

V4 reste présent comme contrat historique byte-compatible et comme couverture de tests/parité. Il n’est pas le vecteur du MLP V1 actif, qui utilise le snapshot encoder V1 à 86 dimensions. Les poids `.pt`, les manifests et l’ordre de features ne sont pas modifiés.

Rollback : revert du commit `refactor: remove remaining legacy runtime paths`, puis reconstruction/test des outils historiques concernés. Aucun fichier stable hors ce worktree n’est requis.
