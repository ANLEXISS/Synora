# Synora API endpoints

## Authentification et RBAC

Les routes API acceptent soit le Bearer admin/service existant, soit une
session web `synora_session` HttpOnly issuee par `/api/auth/login`.

| Méthode | Route | Auth | Permissions |
|---|---|---|---|
| POST | `/api/auth/login` | public | login/password depuis `auth.yaml`, ou token bootstrap |
| GET | `/api/auth/me` | session | utilisateur et permissions courantes |
| POST | `/api/auth/logout` | session ou Bearer | — |
| POST | `/api/auth/refresh` | session | rotation de session |

Sans authentification, le backend retourne `401 {"error":"unauthorized"}`.
Avec une session valide mais sans permission, il retourne
`403 {"error":"forbidden"}`. Le Bearer admin est toujours traité comme le
rôle `admin` complet.

## Endpoints backend réellement disponibles

| Méthode | Route | Handler/fichier | Auth requise | Statut | Utilisé par la webapp | Notes |
|---|---|---|---|---|---|---|
| GET | `/health` | `cmd/synora-api/main.go` | non | legacy | non | Pointe santé process locale. |
| GET | `/api/system/health` | `cmd/synora-api/main.go` | oui, sauf si `PublicSystemHealth` est activé | stable | futur | Santé système publique. |
| GET | `/api/runtime/diagnostics` | `cmd/synora-api/runtime_diagnostics.go` | CGE read | stable | futur | Diagnostic runtime borné. |
| GET | `/api/cge/runtime-status` | `cmd/synora-api/runtime_diagnostics.go` | CGE read | alias | oui | Alias du diagnostic runtime, danger/manual-risk et contexte sécurité. |
| POST | `/api/intrusion/reset` | `cmd/synora-api/runtime_controls.go` | admin | stable | futur | Réinitialise l'état sans supprimer l'historique. |
| POST | `/api/system/state/reset` | `cmd/synora-api/runtime_controls.go` | admin | stable | futur | Réinitialise vers `idle`, avec audit. |
| POST | `/api/cge/manual-risk` | `cmd/synora-api/runtime_controls.go` | admin | stable | oui | Risque manuel ; `test:true` est simulé/dry-run. |
| POST | `/api/cge/manual-risk/clear` | `cmd/synora-api/runtime_controls.go` | admin | stable | oui | Annule le risque manuel actif sans supprimer l’historique. |
| GET | `/api/security/mode` | `cmd/synora-api/security_mode.go` | state read | stable | oui | Mode durable courant ; lecture seule pour resident/guest. |
| POST | `/api/security/mode` | `cmd/synora-api/security_mode.go` | admin | stable | oui | Définit `home`, `night`, `away` ou `high_security`. |
| PATCH | `/api/security/mode` | `cmd/synora-api/security_mode.go` | admin | stable | futur | Mise à jour partielle du mode. |
| POST | `/api/security/arm` | `cmd/synora-api/security_mode.go` | admin | stable | oui | Arme `night`, `away` ou `high_security`, avec durée optionnelle. |
| POST | `/api/security/disarm` | `cmd/synora-api/security_mode.go` | admin | stable | oui | Revient à `home`, désarmé. |
| GET | `/api/state` | `cmd/synora-api/main.go` | oui | stable | oui | Source principale de `useSynoraSnapshot()`. |
| GET | `/api/snapshot` | `cmd/synora-api/main.go` | oui | stable | futur | Snapshot public compact. |
| GET | `/api/devices` | `cmd/synora-api/config_handlers.go` | oui | stable | oui | Liste des devices. |
| GET | `/api/devices/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| POST | `/api/devices` | `cmd/synora-api/config_handlers.go` | oui | stable | partiel | Client disponible, formulaire web à compléter. |
| PATCH | `/api/devices/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | partiel | Client disponible, édition web à compléter. |
| DELETE | `/api/devices/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | oui | Suppression Devices branchée après succès API. |
| GET | `/api/devices/pairing/capabilities` | `cmd/synora-api/synora_camera_pairing.go` | admin | stable | oui | Capacités Synora Camera Pairing. |
| POST | `/api/devices/pairing/synora-camera/start` | `cmd/synora-api/synora_camera_pairing.go` | admin | stable | oui | Valide un QR/JSON et ouvre une session TTL de 10 minutes. |
| POST | `/api/devices/pairing/synora-camera/confirm` | `cmd/synora-api/synora_camera_pairing.go` | admin | stable | oui | Crée atomiquement la caméra dans `devices.yaml`. |
| POST | `/api/devices/pairing/synora-camera/claim` | `cmd/synora-api/synora_camera_pairing.go` | admin | préparatoire | non | Vérifie le token hashé et marque `device_seen`; auth caméra future à définir. |
| GET | `/api/residents` | `cmd/synora-api/config_handlers.go` | oui | stable | oui | Configuration statique + métadonnées face pour admin. `/api/state.residents` reste la présence runtime. |
| GET | `/api/residents/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | oui | Configuration statique redacted selon le rôle. |
| POST | `/api/residents` | `cmd/synora-api/config_handlers.go` | admin | stable | oui | Création atomique, id slug immutable. |
| PATCH | `/api/residents/:id` | `cmd/synora-api/config_handlers.go` | admin | stable | oui | Modification statique, id non modifiable. |
| DELETE | `/api/residents/:id` | `cmd/synora-api/config_handlers.go` | admin | stable | oui | Soft delete ; les photos sont conservées. |
| GET | `/api/residents/:id/face` | `cmd/synora-api/face_handlers.go` | admin | stable | oui | Métadonnées uniquement. |
| POST | `/api/residents/:id/face/base` | `cmd/synora-api/face_handlers.go` | admin | stable | oui | Upload image jpeg/png/webp, maximum 4 photos. |
| DELETE | `/api/residents/:id/face/base/:photo_id` | `cmd/synora-api/face_handlers.go` | admin | stable | oui | Archive explicite de la photo. |
| POST | `/api/residents/:id/face/base/:photo_id/replace` | `cmd/synora-api/face_handlers.go` | admin | stable | oui | Remplacement et statut `needs_rebuild`. |
| GET | `/api/residents/:id/face/base/:photo_id/image` | `cmd/synora-api/face_handlers.go` | admin | stable | oui | Fichier local protégé, `private, no-store`. |
| POST | `/api/residents/:id/face/rebuild` | `cmd/synora-api/face_handlers.go` | admin | placeholder | oui | Marque `ready` si une photo existe ; vrai pipeline à brancher. |
| GET | `/api/residents/:id/face/review` | `cmd/synora-api/face_handlers.go` | admin | stable | oui | Liste les crops du dossier `review/`. |
| POST | `/api/residents/:id/face/review/:crop_id/accept` | `cmd/synora-api/face_handlers.go` | admin | stable | oui | Déplace un crop accepté vers `auto/`. |
| DELETE | `/api/residents/:id/face/review/:crop_id` | `cmd/synora-api/face_handlers.go` | admin | stable | oui | Supprime un crop de review. |
| GET | `/api/residents/:id/face/review/:crop_id/image` | `cmd/synora-api/face_handlers.go` | admin | stable | oui | Image review protégée, sans cache. |
| GET/POST/DELETE | `/api/residents/:id/face/pending*` | `cmd/synora-api/face_handlers.go` | admin | compatibilité | non | Alias historique de `review`; les crops sont maintenant implémentés. |
| GET | `/api/automations` | `cmd/synora-api/config_handlers.go` | oui | stable | oui | Liste des automatisations. |
| GET | `/api/automations/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | futur |  |
| POST | `/api/automations` | `cmd/synora-api/config_handlers.go` | oui | stable | oui | Création builder après succès API. |
| PATCH | `/api/automations/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | oui | Mise à jour builder après succès API. |
| DELETE | `/api/automations/:id` | `cmd/synora-api/config_handlers.go` | oui | stable | oui | Suppression après succès API. |
| GET | `/api/automations/catalog` | `cmd/synora-api/automation_catalog.go` | oui | stable | oui | Catalogue contrôlé pour le builder. |
| GET | `/api/topology` | `cmd/synora-api/config_handlers.go` | oui | stable | oui | Format plat `nodes` + `links`, normalisé par `src/lib/topology.ts`. |
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
| GET | `/api/cge/security-profile` | `cmd/synora-api/cge_profile.go` | oui | stable | oui | Profil de sécurité CGE, lecture résidents/guests. |
| PATCH | `/api/cge/security-profile` | `cmd/synora-api/cge_profile.go` | oui | stable | oui | Admin uniquement, validation stricte et écriture atomique. |
| GET | `/api/cge/feedback` | `cmd/synora-api/cge_profile.go` | oui | stable | oui | Corrections versionnées, filtre `chain_id`. |
| POST | `/api/cge/feedback/evaluation` | `cmd/synora-api/cge_profile.go` | oui | stable | oui | Admin uniquement, feedback d’intention (`correction_type`, `scope`, `preferred_actions`, `admin_note`), événement brut immuable. |
| POST | `/api/cge/feedback/chain` | `cmd/synora-api/cge_profile.go` | oui | stable | oui | Admin uniquement, feedback d’intention et influence optionnelle de la mémoire critique. |
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

## Contrat topology

`GET /api/topology` retourne la forme plate suivante :

```json
{
  "locked": true,
  "version": 1,
  "nodes": [
    {"id":"zoneA","name":"zoneA","type":"zone"},
    {"id":"zoneA.L0","name":"L0","parent":"zoneA","type":"floor"},
    {"id":"zoneA.L0.entree","name":"entree","parent":"zoneA.L0","type":"room","neighbors":[]}
  ],
  "links": [{"from":"zoneA.L0.entree","to":"zoneA.L0.salon"}]
}
```

La webapp reconstruit `zone -> floor -> room`, convertit `neighbors` et `links` en `connect`, et accepte également le tree de `/api/state.nodes` ou un wrapper `{ "topology": ... }`.

## Contrat webapp actuel

La webapp Vite est dans `synora-web/`. Le frontend appelle `GET /api/state`, `GET /api/ws`, `/api/topology`, `/api/devices`, `/api/residents` et `/api/automations` via `synora-web/src/lib/synora-api.ts`. Les écritures sont considérées persistées uniquement après un retour API réussi.

Les chaînes d’événements sont accessibles via `GET /api/events/chains` et `GET /api/events/chains/{id}`. La liste accepte `status`, `limit`, `since`, `severity` et `simulated`; `/api/state` n’expose qu’un résumé `event_chains`. La mémoire des chaînes critiques est disponible via `GET /api/cge/critical-chains` et `GET /api/cge/critical-chains/{id}`. Voir [event-chains.md](event-chains.md) pour la classification et les règles de fermeture.

### Contrat par page

| Page | Endpoints appelés aujourd’hui | Endpoints à brancher | Notes |
|---|---|---|---|
| Dashboard | `/api/state`, `/api/ws` | `/api/devices`, `/api/cge/summary` | La page est encore principalement alimentée par `synora-web/src/data/demo.ts`. |
| CGE | `/api/events/chains`, `/api/events/chains/:id`, `/api/cge/critical-chains`, `/api/cge/security-profile`, `/api/cge/feedback`, `/api/ws` | — | Onglets Live, chaînes connues, réglages sécurité et corrections. |
| Maison / Topologie | `/api/topology`, `/api/devices`, `/api/state`, `/api/ws` | — | Adaptateur plat/tree, résolution des devices et fallback démo diagnostiqué. |
| Périphériques | `/api/devices`, `/api/state`, `/api/ws` | formulaire create/edit | Delete branché ; create/edit affichent leur disponibilité réelle. |
| Résidents | `/api/residents`, `/api/state`, `/api/topology`, `/api/residents/:id/face/*` | — | CRUD, pièce de référence et photos admin branchés ; présence issue de `/api/state.residents`. |
| Automatisations | `/api/automations`, `/api/automations/catalog`, `/api/state`, `/api/ws` | — | Create/update/delete builder branchés après succès API. |
| CGE Inspector | `/api/state`, `/api/ws` | `/api/cge/summary`, `/api/cge/sequences`, `/api/cge/transitions`, `/api/cge/learned-behaviors`, `/api/cge/critical-seeds`, `/api/cge/danger-assessments`, `/api/validations` | La page est encore un placeholder. |
| Synora Lab | `/api/state`, `/api/ws` | `/api/simulation/scenarios`, `/api/simulation/run`, `/api/simulation/runs/:id` | La simulation n’est pas encore reliée à l’UI. |
| Settings | `/api/state`, `/api/ws` | `/api/system/health`, `/api/validations` | À brancher quand les réglages réels seront exposés. |

### Endpoints présents mais pas encore utilisés par l’UI

- `/api/devices` POST/PATCH (client disponible, formulaires create/edit à compléter)
- `/api/residents/:id/face/review*` (validation admin des crops ; l’alias `pending*` est conservé)
- `/api/automations/catalog` (catalogue backend disponible ; le builder conserve
  ses options statiques de compatibilité et expose aussi les champs sécurité)
- `/api/cge/*`
- `/api/simulation/*`
- `/api/system/health`

### Recommandations de branchement

- Dashboard: compléter les vues réelles dérivées de `/api/state` et `/api/ws`.
- Live Events: brancher `/api/ws` d’abord, puis ajouter `/api/events` si un flux historisé devient nécessaire.
- Maison / Topologie: normaliser `/api/topology` puis utiliser `/api/devices` pour l’occupation.
- Périphériques: compléter create/edit ; delete est déjà confirmé par succès API.
- Résidents: ajouter les formulaires CRUD ; les boutons actuels signalent leur indisponibilité.
- Automatisations: remplacer progressivement le catalogue local par `/api/automations/catalog`.
- CGE Inspector: remplacer les placeholders par les routes `/api/cge/*` et `/api/validations`.
- Synora Lab: connecter les scénarios de simulation au backend avant d’ajouter des écrans de contrôle avancé.
- Settings: exposer la santé système et les validations utilisateur avant d’ouvrir des réglages plus profonds.

## Remarque d’implémentation

`cmd/synora-api/main.go` est le routeur réel. Il branche les routes API, `/api/ws`, l’alias `/ws`, puis le fallback statique via `internal/api/web.go` quand `SYNORA_WEB_ENABLED` est actif. `SYNORA_WEB_ROOT` vaut `/var/lib/synora/web` par défaut.

## Permissions par rôle

- `admin` : lecture/écriture devices, residents, topology et automations ;
  CGE, simulation, settings, vidéo et sécurité.
- `resident` : `state:read`, `devices:read`, `residents:read`,
  `topology:read`, `automations:read`, `video:read`.
- `guest` : `state:read` et `topology:read` pour cette passe. Le snapshot
  invité reste le snapshot public courant ; une redaction dédiée est un TODO.

Les mutations `/api/devices*`, `/api/residents*`, `/api/automations*` et
`/api/topology*` sont admin-only. `/api/simulation/*` et `/api/cge/*` sont
également admin-only.

## Résidents : configuration et runtime

`/api/residents` lit la configuration statique : identité, rôle, flags,
`reference_node_id`, `account_id` et métadonnées `face_profile`. Les fichiers
face ne sont jamais renvoyés directement par cette route.

`/api/state.residents` (dans le snapshot `/api/state`) est la source runtime :
`state`, `node_id`, `confidence` et `last_seen`. La configuration ne doit pas
écraser ces champs lors de la fusion frontend ou backend.

Les photos sont stockées sous la racine configurable
`face_data_root/<resident_id>/` : `base/` contient au maximum quatre vues
validées (`face`, `up`, `left`, `right`), `auto/` contient les crops ajoutés
automatiquement et `review/` les crops en attente de validation. Les routes
face sont admin-only. La valeur de développement est
`services/vision-worker/data/face`; `SYNORA_FACE_DATA_ROOT` peut la remplacer.
En runtime systemd, la racine persistante est `/var/lib/synora/vision/face` et
elle n’est pas incluse dans les copies `rsync` de l’installation.
