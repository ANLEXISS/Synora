# Production config hardening

Synora sépare les templates versionnés, la configuration runtime et les
secrets locaux. Le dépôt ne doit jamais devenir la source d’une valeur
d’authentification ou d’un secret caméra.

## Ce qui peut rester dans Git

Les ports, chemins, noms de services, seuils CGE, options de sécurité et
politiques sûres par défaut peuvent rester dans `configs/`. Les valeurs
réseau documentées comme `SynoraNet`, `10.77.0.1`, RTSP `8554`, HLS `8888` et
WebRTC `8889` sont des conventions produit, pas des secrets.

Les templates portent des marqueurs explicites :

- `__GENERATED_AT_INSTALL__` pour les hashes et identifiants générés ;
- `__SET_DURING_FIRST_BOOT__` pour une valeur fournie au commissioning ;
- `__LOCAL_ONLY__` pour une valeur strictement locale.

`debug_endpoints_enabled` et `dev_simulation_enabled` restent désactivés.
Synora Lab reste activé mais protégé par authentification admin et `lab:use`.
SynoraNet reste WPA3, PMF required, réseau caché et fermé par défaut.

MediaMTX conserve pour l’instant un mode RTSP clair de compatibilité sur le
réseau privé SynoraNet. Son API est limitée à `127.0.0.1`, mais le RTSP/HLS/
WebRTC reste bindé pour les flux caméra ; cette configuration ne doit pas être
exposée au LAN général ou au WAN avant ajout d’authentification/transport
adapté.

Classification appliquée dans cette passe :

- modèle safe : ports, chemins, noms de services, seuils, policies et flags
  non sensibles (`configs/cge_profile.yaml`, `configs/action_policy.yaml` et
  la section `features` de `security.yaml`) ;
- prototype déplacé : anciens tokens, hashes/mots de passe, secrets device,
  PSK Wi-Fi, coordonnées de notification, URLs ou modes de debug de
  développement ;
- production générée : token API, secret de session, PSK SynoraNet, compte
  admin initial, secrets de pairing, certificats TLS et futures clés mTLS ;
- valeur documentée : SSID `SynoraNet`, `10.77.0.1`, ports API/RTSP/HLS/
  WebRTC et chemins `/var/lib/synora`.

`configs/synora.yaml` garde `127.0.0.1:1883` comme endpoint de broker local
non sensible, avec un identifiant `synora-core` et des logs `info`. Le suffixe
prototype `rock5-dev`, les identifiants dev et les niveaux `debug` ne font plus
partie du modèle standard.

## Ce qui est généré localement

`synora-bootstrap-config` prépare `/etc/synora` à partir des templates :

```text
synora-bootstrap-config plan --etc /etc/synora
synora-bootstrap-config validate --etc /etc/synora
synora-bootstrap-config validate --templates /opt/synora/config-templates --template
synora-bootstrap-config init --etc /etc/synora
synora-bootstrap-config rotate-api-token --etc /etc/synora
synora-bootstrap-config rotate-synoranet-psk --etc /etc/synora
```

`init` est dry-run par défaut. L’écriture exige `--apply`. Les valeurs ne sont
jamais imprimées ; elles sont placées dans :

- `/etc/synora/secrets/api_token` ;
- `/etc/synora/secrets/session_secret` ;
- `/etc/synora/secrets/synoranet_psk` ;
- `/etc/synora/secrets/admin_initial_password`.

Le hash API est stocké dans `security.yaml`, le hash bcrypt admin dans
`auth.yaml`. Les secrets existants ne sont pas remplacés par `init`.
Les rotations créent un backup adjacent avant remplacement.

## Runtime vs templates

`make install` installe les templates sûrs dans
`/opt/synora/config-templates/`, mais diffère les fichiers générés
`security.yaml`, `auth.yaml`, `network.yaml` et `devices.yaml` à
`synora-bootstrap-config`. Les fichiers non sensibles sont copiés vers
`/etc/synora` en conservant une configuration existante.

Une installation Founders Edition doit donc suivre :

1. installer le runtime ;
2. exécuter le plan bootstrap ;
3. exécuter `init --apply` pendant le first boot contrôlé ;
4. exécuter `validate` avant d’activer les services ;
5. provisionner les certificats TLS et secrets providers hors Git.

## Permissions

- `security.yaml`, `auth.yaml`, `network.yaml` et `devices.yaml` : `0640`,
  owner `root:synora` ;
- `/etc/synora/secrets/*` : `0600` par défaut, `session_secret` en `0640`
  pour `synora-api`, jamais world-readable ;
- fichiers temporaires hostapd : `0600` sous `/run/synora` ;
- `/var/lib/synora` : `synora:synora`, aucune donnée secrète exposée au monde ;
- certificats publics : `0644` ; clés TLS : `0640` ou `0600`.

`validate` refuse les placeholders, les secrets trop courts, les fichiers
world-readable et un compte admin absent, désactivé ou sans hash bcrypt.
`validate --template` vérifie au contraire qu’un template Git reste safe et
accepte ses placeholders explicites.

## OTA et rollback

Une OTA peut remplacer binaires, unités systemd, web statique, templates et
worker. Elle ne doit pas écraser :

- `/etc/synora/` et `/etc/synora/secrets/` ;
- `/var/lib/synora/auth/` ;
- `/var/lib/synora/state.json` ;
- `/var/lib/synora/vision/face/`, clips et logs ;
- les modèles locaux validés.

Le rollback doit restaurer le code et les templates compatibles, jamais les
secrets ou les données utilisateur.

Avant le premier OTA, la machine doit disposer d’un manifest signé, d’un
`image-version.json` non secret, d’un contrat de schema et d’un healthcheck
post-boot. Les migrations de `/etc/synora` sont versionnées, sauvegardées et
idempotentes ; elles ne réécrivent jamais `secrets/`. Un échec de validation,
de migration ou de service critique empêche `mark-good` et déclenche le
rollback de slot.

## Cas `weapon.rknn`

`weapon.rknn` est optionnel pour la première Founders Edition. Son absence
marque `weapon_detection` en `degraded/unavailable`, mais ne bloque pas le
vision-worker ni les autres capacités. `make install-plan` le signale comme
modèle manquant optionnel.
