# Organisation du code Synora

## Frontières principales

### Runtime production

Le runtime installé et démarré par systemd se trouve dans `cmd/`, `internal/`
et `pkg/` : Bus, Core/CGE, `DangerRuntime`, état, Event Chains, Discovery,
Actions, API, authentification, réseau, pairing réel, santé et snapshot.
Le runtime ne dépend pas de `tests/` ni d’un outil sous `tools/`.

`internal/simulation` est le moteur de scénarios contrôlés partagé par les
interfaces compatibles ; son exposition API est désactivée par défaut par
`features.dev_simulation_enabled`. Il ne doit jamais être confondu avec les
validations produit de Synora Lab.

### Synora Lab — module produit/admin

La page `synora-web/src/pages/SynoraLab.tsx` et les routes de validation
contrôlée constituent Synora Lab. Elles servent au commissioning, à la
maintenance et à la validation d’une installation réelle. L’accès est
admin-only via `lab:use`, avec les routes historiques
`/api/cge/validation/*` conservées et les noms produit `/api/lab/*` ajoutés.
Voir [synora-lab.md](synora-lab.md).

### Tests automatisés

Les `*_test.go` restent à côté du package testé afin de conserver les règles
de compilation et les frontières Go. Les tests web restent dans
`synora-web/src`. Les tests Python du worker restent dans
`services/vision-worker/tests`. Les scénarios et fixtures partageables vont
dans `tests/fixtures/` et `tests/scenarios/`; aucun test existant n’est
déplacé sans preuve qu’il s’agit d’un artefact autonome.

### Diagnostics opérateur

Les diagnostics support et réseau sont rangés ou référencés dans
`tools/diagnostics/`. Les endpoints de diagnostic (`/api/runtime/diagnostics`
et `/api/cge/runtime-status`) sont admin-protected et contrôlés par
`features.diagnostics_enabled`. Les détails debug bruts restent désactivés par
défaut (`debug_endpoints_enabled: false`).

### Archives et prototypes

Les simulateurs CLI et scripts de déploiement historiques restent hors du
runtime, dans `tools/dev/` ou `tools/maintenance/`. Les éléments dont le
remplacement est confirmé peuvent être placés dans
`tests/archived/YYYYMMDD-cleanup/` avec un README d’origine, de remplacement
et de suppression possible. Aucun `*_test.go` ne doit être archivé pour gagner
de la place.

## Règles d’import

- `internal/core`, `internal/cge` et `internal/discovery` n’importent jamais
  `tests/` ou `tools/`.
- Synora Lab consomme des contrats et façades publiques internes ; il ne
  contourne pas le pipeline Core/CGE.
- Les simulateurs développeur ne sont pas installés par défaut.
- Les routes Lab et diagnostics sont séparées des routes de production dans
  la documentation, même lorsque le routeur HTTP partagé reste dans
  `cmd/synora-api` pour compatibilité.

## Feature flags

Dans `features` de `security.yaml` :

- `synora_lab_enabled: true` ;
- `diagnostics_enabled: true` ;
- `cge_validation_enabled: true` ;
- `debug_endpoints_enabled: false` ;
- `dev_simulation_enabled: false`.

Les valeurs omises prennent ces défauts. Une valeur explicitement `false` est
respectée.

## Secrets et logs

Les secrets, tokens, signatures et chemins sensibles ne doivent jamais être
écrits dans les logs, les payloads de diagnostic ou les exports Lab. Les
résultats Lab exportables doivent rester redacted et porter leurs marqueurs de
validation (`source_type`, `validation`, `generated_by`).
