# `synora-boot-healthcheck`

L’outil est maintenant un binaire du dépôt, mais il n’est pas installé ni
exécuté dans cette passe. Il est livré dans le rootfs avec `synora-check` et
sera invoqué par le contrôleur de boot RAUC.

## Interface proposée

```text
synora-boot-healthcheck run --readonly \
  --base-url http://127.0.0.1:8080 \
  --report /var/lib/synora/logs/boot-healthcheck-latest.json \
  --timeout 60s

# Exception explicite pour un profil prototype/maintenance :
synora-boot-healthcheck run --readonly --allow-synoranet-degraded

synora-boot-healthcheck explain
```

- sortie `0` : toutes les conditions bloquantes sont satisfaites ;
  `mark-good` peut être appelé par l’orchestrateur ;
- sortie `1` : échec bloquant ou timeout ; le slot reste pending et le
  bootloader doit déclencher le rollback ;
- aucune sortie standard ne contient de secret.

Le rapport est écrit atomiquement au chemin demandé, avec mode `0640`. Il ne
contient pas les corps HTTP, les lignes journald, tokens, PSK ou autres
secrets. Sans `--report`, aucune écriture n’est faite.

## Séquence de checks

### 1. Identité de l’image

- vérifier que `GET /api/system/version` répond et que
  `/opt/synora/version.json` reste un artefact non secret ;
- comparer `bundle_id`, `image_version`, `synora_version`, `git_commit` et
  `target_board` au manifest attendu ;
- vérifier le kernel courant et le runtime RKNN contre les contraintes du
  manifest ;
- refuser un downgrade implicite non autorisé.

La version est exposée par `GET /api/system/version`. Le slot est
`unmanaged` tant que RAUC n’est pas installé.

### 2. Mounts et permissions

- `/etc/synora/security.yaml` et `network.yaml` lisibles ;
- secrets lisibles par les seuls services autorisés, sans les inclure dans le
  rapport ;
- `/var/lib/synora` lisible et writable par `synora` ;
- `/var/lib/synora/state`, `logs`, `auth`, `vision/face`, `cge` accessibles ;
- `/var/lib/synora/models` présent si la policy modèle l’exige ;
- espace libre suffisant pour les écritures atomiques et backups.

Le futur outil peut déléguer la validation de configuration à
`synora-bootstrap-config validate`, en capturant uniquement le statut et des
messages redacted.

### 3. Services et API

Vérifier que ces unités sont actives et stables pendant une fenêtre bornée :

- `synora-bus`
- `synora-core`
- `synora-api`
- `synora-discovery`
- `synora-actions`

MediaMTX et les composants optionnels suivent la policy du manifest. Ensuite :

- `GET /api/system/health` répond avec un JSON valide ;
- `GET /api/state` répond avec un JSON valide ;
- la version rapportée correspond au manifest ;
- aucune réponse ne contient token, PSK, mot de passe ou clé privée.

Le token API est lu depuis le secret local dans un descripteur de fichier ou
en mémoire, jamais passé dans une URL, jamais écrit dans le JSON et jamais
affiché par `journalctl` ou le shell.

### 4. Réseau, driver et vision

Si SynoraNet est enabled :

- vérifier l’interface et le driver attendu avec des commandes read-only ;
- vérifier le bridge, l’état AP, la policy WPA3/PMF et le statut Discovery ;
- distinguer `ok`, `degraded` autorisé et fatal selon le manifest.

Par défaut, un SynoraNet enabled en `degraded` est fatal pour Founders Edition.
`--allow-synoranet-degraded` est une exception explicite pour un profil
prototype ou maintenance.

Pour la vision :

- vérifier le worker et son socket runtime ;
- vérifier les modèles requis ;
- considérer le manifest modèles absent ou invalide comme fatal ;
- accepter `weapon.rknn` absent comme `degraded` ;
- considérer NPU/RKNN absent ou modèle primaire absent comme fatal en
  Founders, sauf policy de maintenance explicitement signée.

### 5. Journaux et stabilité

Inspecter seulement la fenêtre depuis le dernier boot et rechercher panic,
fatal, deadlock, data race, crash loop et erreur runtime critique. Le rapport
contient des compteurs et noms de services, pas les lignes brutes si elles
peuvent contenir des chemins sensibles ou des secrets.

## Réutilisation de `synora-system-test`

Le mode `--mode boot-readonly` est réservé à la décision OTA : il n’injecte
aucun événement, n’ouvre pas de pairing, ne change pas le security mode et ne
déclenche aucune action. Le mode `full` reste un test de laboratoire.

Il ne lance pas `synora-connect`, ne génère pas d’identité et ne tente aucune
connexion control plane ou tunnel distant.

Le script existant permet déjà de rendre Discovery, Actions et MediaMTX
bloquants avec `--strict-services`; le boot healthcheck doit utiliser une
policy explicite plutôt que cette valeur implicite.

## Rapport minimal

```json
{
  "boot_id": "opaque-local-id",
  "status": "ok|degraded|rollback_required",
  "checked_at": "RFC3339 timestamp",
  "duration_ms": 1234,
  "checks": [],
  "fatal_reasons": [],
  "degraded_reasons": []
}
```

Les valeurs d’exemple sont des marqueurs de format et ne doivent pas être
remplacées par des secrets.
