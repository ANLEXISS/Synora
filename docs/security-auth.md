# Authentification et comptes Synora

`residents.yaml` décrit les personnes physiques et leur présence. `auth.yaml`
décrit les comptes de connexion et leurs rôles.

```yaml
users:
  - id: user_alexis
    login: alexis
    resident_id: alexis
    role: admin
    enabled: true
    password_hash: "$2a$10$REPLACE_WITH_BCRYPT_HASH"
  - id: user_carole
    login: carole
    resident_id: carole
    role: resident
    enabled: true
    password_hash: "$2a$10$REPLACE_WITH_BCRYPT_HASH"
```

Les mots de passe sont vérifiés avec bcrypt. Générer un hash de développement :

```bash
make hash-password PASSWORD='mot-de-passe-local'
```

La valeur en clair ne doit jamais être écrite dans Git, `auth.yaml`, les logs
ou le build statique.

## Session web

Le login crée une session serveur dans `/var/lib/synora/auth/sessions.json`.
Le navigateur ne reçoit qu'un cookie `synora_session` HttpOnly, SameSite Strict.
Le disque ne conserve que le hash de l'identifiant de session.

`GET /api/auth/me` renvoie `id`, `login`, `role`, `resident_id` et les
permissions, jamais `password_hash`. Les claims sont revalidés depuis
`auth.yaml` à chaque requête : une désactivation ou un changement de rôle
prend effet sans rebuild frontend.

## RBAC

Le backend applique les permissions avant les handlers. Le frontend masque les
actions interdites uniquement pour l'ergonomie.

- `admin` : accès complet, simulation, CGE, settings et sécurité.
- `resident` : lecture state, devices, residents sans champs sensibles,
  topology, automations et vidéo autorisée.
- `guest` : lecture minimale state/topology pour cette passe.

Un appel non authentifié reçoit `401`; un appel authentifié mais interdit reçoit
`403`. Tailscale ou WireGuard sécurisent le transport réseau mais ne remplacent
pas les comptes applicatifs et le RBAC. HTTPS reste recommandé hors réseau
local.

## Profils résidents et photos de reconnaissance

`residents.yaml` contient les métadonnées de la personne et peut référencer un
`account_id`. Les mots de passe et autres secrets restent exclusivement dans
`auth.yaml`.

Exemple de profil résident :

```yaml
- id: alexis
  first_name: Alexis
  last_name: Martin
  display_name: Alexis
  role: resident
  admin: false
  trusted: true
  reference_node_id: zoneA.L1.chambre_enfant
  account_id: user_alexis
  face_profile:
    status: needs_rebuild
    base_photos:
      - id: face-123
        filename: face-123.jpg
        path: services/vision-worker/data/face/alexis/base/face-123.jpg
        view: face
        source: manual_upload
        created_at: "2026-07-11T17:03:56Z"
        updated_at: "2026-07-11T17:03:56Z"
    auto_count: 0
    review_count: 0
```

Le bloc `face_profile` ne contient que des métadonnées et chemins générés par le
backend. La racine est `vision.face_data_root` (ou la variable
`SYNORA_FACE_DATA_ROOT`) et les images sont stockées localement sous :

```text
services/vision-worker/data/face/<resident_id>/base/
services/vision-worker/data/face/<resident_id>/auto/
services/vision-worker/data/face/<resident_id>/review/
```

En production, le chemin peut par exemple être
`/var/lib/synora/vision/face`; aucun chemin absolu n’est
codé dans les handlers.

Les photos de base sont limitées à quatre par résident. Les uploads,
remplacements, suppressions, images et recalculs sont admin-only. Les chemins
fournis par un client ne sont jamais utilisés pour construire un chemin disque;
le backend génère les noms et bloque les traversées de répertoire.

La suppression d’une photo est explicite et déplace le fichier dans un dossier
`archive` du résident. Le recalcul actuel ne fait que mettre le statut à
`ready` lorsqu’une photo existe ; le pipeline réel d’embeddings/ArcFace-RKNN
reste un TODO. Un crop `review` accepté est déplacé vers `auto`; la promotion
manuelle vers `base` sera ajoutée avec un choix de slot explicite.
