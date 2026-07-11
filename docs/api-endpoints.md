# Synora API endpoints

## Endpoints backend réellement disponibles

| Méthode | Route | Handler/fichier | Auth requise | Statut | Utilisé par la webapp | Notes |
|---|---|---|---|---|---|---|
| GET | `/health` | `cmd/synora-api/main.go` | non | legacy | non | Pointe santé process locale. |
| GET | `/api/system/health` | `cmd/synora-api/main.go` | oui, sauf si `PublicSystemHealth` est activé | stable | futur | Santé système publique. |
| GET | `/api/state` | `cmd/synora-api/main.go` | oui | stable | oui | Source principale de `useSynoraSnapshot()`. |
| GET | `/api/snapshot` | `cmd/synora-api/main.go` | oui | stable | futur | Snapshot public compact. |
| GET | `/api/devices` | `cmd/synora-api/config_handlers.go` | oui | stable | futur | CRUD public des devices. |
| GET | `/api/devices/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| POST | `/api/devices` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| PATCH | `/api/devices/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| DELETE | `/api/devices/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| GET | `/api/residents` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| GET | `/api/residents/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| POST | `/api/residents` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| PATCH | `/api/residents/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| DELETE | `/api/residents/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| GET | `/api/automations` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| GET | `/api/automations/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| POST | `/api/automations` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| PATCH | `/api/automations/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| DELETE | `/api/automations/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| GET | `/api/automations/catalog` | `cmd/synora-api/automation_catalog.go` | oui | stable | futur | Catalogue contrôlé pour le builder. |
| GET | `/api/topology` | `cmd/synora-api/config_handlers.go` | oui | stable | oui | Représentation courante de la topologie. |
| POST | `/api/topology` | `cmd/synora-api/config_handlers.go` | oui | stable | futur | Remplacement complet. |
| DELETE | `/api/topology` | `cmd/synora-api/config_handlers.go` | oui | stable | futur | Reset et déplacement des devices non localisés. |
| GET | `/api/validations` | `cmd/synora-api/main.go` | oui | stable | futur | Inspection des validations utilisateur. |
| GET | `/api/validations/:id` | `cmd/synora-api/main.go` | oui | stable | futur |  |
| POST | `/api/validations` | `cmd/synora-api/main.go` | oui | stable | futur |  |
| PATCH | `/api/validations/:id` | `cmd/synora-api/main.go` | oui | stable | futur |  |
| DELETE | `/api/validations/:id` | `cmd/synora-api/main.go` | oui | stable | futur |  |
| GET | `/api/cge/summary` | `cmd/synora-api/main.go` | oui | stable | futur | CGE inspection. |
| GET | `/api/cge/sequences` | `cmd/synora-api/main.go` | oui | stable | futur |  |
| GET | `/api/cge/sequences/:id` | `cmd/synora-api/cge.go` | oui | stable | futur |  |
| GET | `/api/cge/transitions` | `cmd/synora-api/main.go` | oui | stable | futur |  |
| GET | `/api/cge/learned-behaviors` | `cmd/synora-api/main.go` | oui | stable | futur |  |
| GET | `/api/cge/learned-behaviors/:id` | `cmd/synora-api/cge.go` | oui | stable | futur |  |
| PATCH | `/api/cge/learned-behaviors/:id` | `cmd/synora-api/cge.go` | oui | stable | futur |  |
| DELETE | `/api/cge/learned-behaviors/:id` | `cmd/synora-api/cge.go` | oui | stable | futur |  |
| POST | `/api/cge/learned-behaviors/:id/approve` | `cmd/synora-api/cge.go` | oui | stable | futur |  |
| POST | `/api/cge/learned-behaviors/:id/reject` | `cmd/synora-api/cge.go` | oui | stable | futur |  |
| POST | `/api/cge/learned-behaviors/:id/disable` | `cmd/synora-api/cge.go` | oui | stable | futur |  |
| POST | `/api/cge/learned-behaviors/:id/reset` | `cmd/synora-api/cge.go` | oui | stable | futur |  |
| GET | `/api/cge/critical-seeds` | `cmd/synora-api/cge_config.go` | oui | stable | futur |  |
| GET | `/api/cge/critical-seeds/:id` | `cmd/synora-api/cge_config.go` | oui | stable | futur |  |
| POST | `/api/cge/critical-seeds` | `cmd/synora-api/cge_config.go` | oui | stable | futur |  |
| PATCH | `/api/cge/critical-seeds/:id` | `cmd/synora-api/cge_config.go` | oui | stable | futur |  |
| DELETE | `/api/cge/critical-seeds/:id` | `cmd/synora-api/cge_config.go` | oui | stable | futur |  |
| GET | `/api/cge/danger-assessments` | `cmd/synora-api/cge_config.go` | oui | stable | futur |  |
| GET | `/api/cge/danger-assessments/:id` | `cmd/synora-api/cge_config.go` | oui | stable | futur |  |
| GET | `/api/simulation/scenarios` | `cmd/synora-api/main.go` | oui | simulation | futur |  |
| POST | `/api/simulation/run` | `cmd/synora-api/main.go` | oui | simulation | futur |  |
| GET | `/api/simulation/runs/:id` | `cmd/synora-api/main.go` | oui | simulation | futur |  |
| GET | `/api/ws` | `cmd/synora-api/main.go` + `cmd/synora-api/ws.go` | oui | stable | oui | Canal WebSocket principal. |
| GET | `/ws` | `cmd/synora-api/main.go` + `cmd/synora-api/ws.go` | oui | legacy | futur | Alias de compatibilité du WS. |
| GET | `/` | `internal/api/web.go` | non | stable | oui | Sert `index.html` si la web statique est activée. |
| GET | `/assets/*` | `internal/api/web.go` | non | stable | oui | Cache immutable pour les assets Vite. |
| GET | `/*` fallback SPA | `internal/api/web.go` | non | stable | oui | `/automations`, `/settings`, `/devices`, etc. |

## Endpoints attendus mais absents

| Méthode | Route | Notes |
|---|---|---|
| GET | `/api/events` | Pas d’endpoint dédié exposé pour l’instant. La webapp Live Events doit encore s’appuyer sur `/api/state` et `/api/ws`. |

## Contrat webapp actuel

La webapp Vite est dans `synora-web/`. Le frontend appelle déjà `GET /api/state` et `GET /api/ws` via `synora-web/src/lib/synora-api.ts` et `synora-web/src/lib/useSynoraSnapshot.ts`. Les autres helpers (`/api/devices`, `/api/topology`, `/api/automations`) sont présents dans la couche de lib, mais les pages restent majoritairement branchées sur les données démo.

### Contrat par page

| Page | Endpoints appelés aujourd’hui | Endpoints à brancher | Notes |
|---|---|---|---|
| Dashboard | `/api/state`, `/api/ws` | `/api/devices`, `/api/cge/summary` | La page est encore principalement alimentée par `synora-web/src/data/demo.ts`. |
| Live Events | `/api/state`, `/api/ws` | `/api/events` (futur) | La page est encore un placeholder. |
| Maison / Topologie | `/api/state`, `/api/ws` | `/api/topology`, `/api/devices` | La topologie affichée vient encore des démos locales. |
| Périphériques | `/api/state`, `/api/ws` | `/api/devices` | Le CRUD visuel n’est pas encore branché. |
| Résidents | `/api/state`, `/api/ws` | `/api/residents` | La page est encore sur les jeux de données démo. |
| Automatisations | `/api/state`, `/api/ws` | `/api/automations`, `/api/automations/catalog`, `/api/devices`, `/api/topology` | Le builder côté UI utilise encore un catalogue local. |
| CGE Inspector | `/api/state`, `/api/ws` | `/api/cge/summary`, `/api/cge/sequences`, `/api/cge/transitions`, `/api/cge/learned-behaviors`, `/api/cge/critical-seeds`, `/api/cge/danger-assessments`, `/api/validations` | La page est encore un placeholder. |
| Synora Lab | `/api/state`, `/api/ws` | `/api/simulation/scenarios`, `/api/simulation/run`, `/api/simulation/runs/:id` | La simulation n’est pas encore reliée à l’UI. |
| Settings | `/api/state`, `/api/ws` | `/api/system/health`, `/api/validations` | À brancher quand les réglages réels seront exposés. |

### Endpoints présents mais pas encore utilisés par l’UI

- `/api/devices`
- `/api/residents`
- `/api/automations`
- `/api/automations/catalog`
- `/api/topology`
- `/api/cge/*`
- `/api/simulation/*`
- `/api/system/health`

### Recommandations de branchement

- Dashboard: remplacer les tuiles démo par `useSynoraSnapshot()` et des vues dérivées de `/api/state`.
- Live Events: brancher `/api/ws` d’abord, puis ajouter `/api/events` si un flux historisé devient nécessaire.
- Maison / Topologie: consommer `/api/topology` pour la structure et `/api/devices` pour l’occupation.
- Périphériques et Résidents: basculer vers les CRUD réels sans conserver la source démo.
- Automatisations: utiliser `/api/automations/catalog` pour le builder, puis `/api/automations` pour la persistance.
- CGE Inspector: remplacer les placeholders par les routes `/api/cge/*` et `/api/validations`.
- Synora Lab: connecter les scénarios de simulation au backend avant d’ajouter des écrans de contrôle avancé.
- Settings: exposer la santé système et les validations utilisateur avant d’ouvrir des réglages plus profonds.

## Remarque d’implémentation

`cmd/synora-api/main.go` est le routeur réel. Il branche les routes API, `/api/ws`, l’alias `/ws`, puis le fallback statique via `internal/api/web.go` quand `SYNORA_WEB_ENABLED` est actif. `SYNORA_WEB_ROOT` vaut `/var/lib/synora/web` par défaut.
