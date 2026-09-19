# API Discovery V1 — audit et migration

## Périmètre audité

Audit effectué sur `cmd/synora-api`, `internal/discovery`, `cmd/synora-core`, le bus Unix, les unités systemd et les cibles Makefile. Le serveur historique enregistrait deux listeners (`HTTP` et, selon configuration, `HTTPS`), une pile CORS/logging/metrics/rate-limit, les routes `/api/*`, les aliases `/api/v1/*`, `/api/ws`, `/ws`, les routes d’authentification et un serveur de fichiers web.

Les routes historiques couvraient : état/snapshot, événements et chaînes, incidents, clips, simulation, CGE et validation, appareils et appairage, flux, résidents/photos/visages, automatisations, topologie, sécurité, politique d’actions, diagnostics, connectivité, version et reset. Les handlers de modification appelaient `internal/coreclient` ; les handlers photos et appairage écrivaient aussi dans des fichiers locaux. Les routes CGE et les routes de démonstration étaient exclusivement attachées à ce serveur.

## Décision V1

`synora-discovery` est désormais le seul propriétaire des listeners HTTP(S) externes. La surface contractuelle V1 est :

| Route | Flux | Autorité | Données admises |
|---|---|---|---|
| `GET /api/v1/state` | Discovery → cache de snapshots Core | Store/Core | snapshot public canonique |
| `GET /api/v1/history` | Discovery → historique borné de snapshots | Store/Core | commits résumés |
| `GET /api/v1/health` | Discovery | Discovery | statut de service |
| `GET /api/v1/subscribe` | flux SSE Store/Core | Store/Core | événements de snapshot |
| `POST /api/v1/commands` | Discovery → événement → Core | Core | commande normalisée |
| `POST /api/v1/messages` | Discovery → événement → Core | Core | événement versionné validé |

Les requêtes mutantes ne touchent ni le Store ni un device : elles deviennent des événements `discovery.web.command` publiés sur le bus à destination du Core. Le Core publie `core.snapshot` après commit ; Discovery alimente un cache de lecture borné et le flux SSE. Le cache est un read model, pas un second Store.

L’authentification par Bearer token et l’interdiction de données brutes sont appliquées devant cette surface. Les réponses API sont `no-store` et la taille d’entrée reste limitée par `MaxBoundaryPayload`.

## Chemins retirés

Le binaire, l’unité systemd et l’inscription Makefile de `synora-api` sont supprimés. Les routes CGE, simulation, upload média, photos/biométrie, caméra réelle et anciens endpoints de démo ne sont pas réexposés dans V1 : ils dépendaient du serveur supprimé, de `coreclient` ou de données interdites. Leur conservation serait une seconde autorité et contredirait les frontières V1.

Les références restantes dans les outils historiques, tests de catalogue et notes de migration sont non exécutables par le runtime V1. Elles sont conservées uniquement comme historique de migration tant que leurs paquets de qualification ne sont pas retirés dans une opération dédiée.

## Preuves de non-écriture

- `internal/discovery.Boundary.Store` expose seulement `SnapshotJSON`.
- `SnapshotCache` ne possède aucune méthode de commit et limite l’historique à 256 entrées.
- `busEventPublisher` envoie les écritures comme événements `discovery → core`.
- `core.snapshot` est émis après le commit du Store.
- `Boundary.ExecuteAction` reste `dry_run` et produit `physical_action_executed: false`.

## Rollback

Le rollback fonctionnel est un revert du commit de migration API, suivi de la reconstruction du binaire Discovery. Il ne restaure pas `synora-api` dans le démarrage V1 : la restauration de l’ancienne surface nécessiterait une décision explicite et un commit séparé, car elle réintroduirait les chemins CGE et les écritures hors Core.
