# Empreinte runtime Synora Founders Edition

Ce document décrit ce que `make install` installe réellement. La commande
`make install` n’a pas été exécutée pendant cette passe ; `make install-plan`
produit le même inventaire en lecture seule.

## Installation standard

Le runtime standard contient :

- les sept binaires de production dans `/opt/synora/bin/` : Bus, Core, API,
  Discovery, Actions, Runtime Manager et Network Config ;
- `synora-check`, diagnostic opérateur sobre, installé dans le même répertoire
  avec les permissions `root:root`, `0755` ;
- le vision-worker sous `/opt/synora/services/vision-worker/`, limité à
  `worker.py`, `requirements.txt` et aux packages `core`, `modules`, `utils`
  et `video` ;
- MediaMTX sous `/opt/synora/mediamtx/`, nécessaire aux flux caméra ;
- les modèles RKNN requis (`arcface_w600k_r50.rknn`, `det_10g.rknn`,
  `yolov8.rknn`) sous `/var/lib/synora/models/` ; cette destination correspond
  aux chemins réellement utilisés par le worker ; `weapon.rknn` reste
  optionnel et dégradé s’il est absent ;
- le build statique de `synora-web` sous `/opt/synora/web/` dans le rootfs ;
  `/var/lib/synora/web/` reste un fallback prototype et n’est plus une donnée
  persistante OTA ;
- les templates YAML sûrs de `configs/` sous `/opt/synora/config-templates/` ;
  `security.yaml`, `auth.yaml`, `network.yaml` et `devices.yaml` sont des
  modèles archivés puis générés dans `/etc/synora` par
  `synora-bootstrap-config`, sans écraser une configuration existante ;
- les unités systemd des services runtime, dont MediaMTX et
  `synora-connect` ;
- les répertoires persistants `state`, `clips`, `debug`, `logs`, `vision/face`
  et `auth` sous `/var/lib/synora/`.
- le répertoire `/var/lib/synora/connectivity/`, persistant hors rootfs A/B,
  contenant uniquement l’identité locale générée et l’état public de
  connectivité ; ses clés ne sont jamais copiées depuis Git.

Synora Lab reste inclus dans la webapp et l’API produit. Il est admin-only via
`lab:use` et `features.synora_lab_enabled`; il n’est pas classé comme un
simulateur développeur.

## Ce qui n’est pas installé par défaut

`make install` ne copie pas :

- `tools/dev/legacy-simulators/` ni les autres outils développeur ;
- le compagnon CLI historique de Synora Lab via `make dev-tools` ou
  `make install-dev-tools` ; la surface produit Lab reste web/API ;
- `tests/`, fixtures, scénarios, archives et tests Python du worker ;
- la documentation et les fichiers de test du vision-worker ;
- `synora-web/node_modules`, caches, `dist` source et fichiers temporaires.

Les simulateurs sont construits et installés uniquement par les targets
explicites `make dev-tools` et `make install-dev-tools`.

## Targets d’installation

- `make build` construit les binaires runtime dans `bin/` ;
- `make test` exécute les tests automatisés ;
- `make diagnostics` vérifie les scripts de diagnostic ;
- `make dev-tools` construit uniquement les simulateurs legacy ;
- `make install` installe le runtime, la webapp, les modèles, MediaMTX et
  `synora-check` ;
- `make install-dev-tools` installe explicitement les simulateurs legacy ;
- `make install-diagnostics` installe explicitement `synora-check` ;
- `make install-bootstrap-config` installe explicitement le générateur de
  configuration production ;
- `make install-plan` affiche sources, destinations, types, modes et
  propriétaires sans écrire dans le système.

Les dépendances OS de `install-deps` sont séparées des fichiers installés :
Python et ses bibliothèques sont nécessaires au worker ; npm ne sert qu’à
construire la webapp et ses dépendances de build ne sont pas copiées dans
`/opt/synora/web/`.

## Permissions et secrets

- binaires runtime et diagnostics : `0755 root:root` ;
- configs `/etc/synora` : `0640 root:synora`, sans écraser les fichiers déjà
  provisionnés ;
- données runtime : `synora:synora`, avec `vision/face` en `0750` et `auth` en
  `0700` ;
- fichiers secrets générés : `0600` ou `0640` stricts ; les fichiers hostapd
  temporaires sont générés en `0600` sous `/run/synora` ;
- web statique et worker : lisibles par `synora-api`/`synora`, sans fichiers
  de test ni secrets dans les exports.

Les templates Git doivent être considérés comme des exemples de bootstrap.
Le token API, les hashes de comptes, secrets caméra, PSK SynoraNet, certificats
TLS et tokens providers doivent être provisionnés ou tournés hors Git. Les
logs et `install-plan` n’affichent jamais leurs valeurs.

Voir [config-hardening.md](config-hardening.md) pour le first boot, la
validation et les règles de rotation avant OTA.

Les templates sous `/opt/synora/config-templates` peuvent être remplacés par
une OTA. Les configs runtime générées sous `/etc/synora`, les secrets sous
`/etc/synora/secrets`, les certificats et les données sous `/var/lib/synora`
restent hors de la partition rootfs remplaçable et doivent être conservés lors
d’un rollback.

## Données persistantes et rollback

Les chemins suivants ne doivent pas être écrasés par une OTA :

- `/var/lib/synora/state.json` ;
- `/var/lib/synora/auth/` ;
- `/var/lib/synora/vision/face/` ;
- `/var/lib/synora/clips/` et `/var/lib/synora/logs/` ;
- `/etc/synora/` et ses secrets ;
- les certificats et fichiers réseau générés sous `/etc/synora/` et
  `/run/synora/`.

Les binaires, unités systemd, web statique et code du vision-worker sont les
parties remplaçables. `install-config` conserve une configuration existante et
les opérations de migration doivent utiliser les mécanismes atomiques déjà
présents dans le runtime.

## Cible OTA

Pour RAUC, le rootfs A/B doit contenir l’OS, le kernel/modules compatibles
Radxa, `/opt/synora/bin`, le vision-worker, MediaMTX, les unités systemd, les
templates, la webapp rootfs et les healthchecks. `/etc/synora`, les secrets,
`/var/lib/synora` et les modèles restent hors du slot système.

Le layout cible Founders Edition sépare en plus `/models` de `/data`, tout en
conservant `/var/lib/synora/models` comme chemin compatible. La webapp est
désormais un artefact rootfs sous `/opt/synora/web`; l’API garde un fallback
explicite vers `/var/lib/synora/web` pour le prototype.

Le fichier de version, le manifest signé, le manifest modèles, les migrations et le rapport de
boot-healthcheck sont des artefacts d’OTA non secrets. Les sessions, clips,
état, feedback CGE, secrets de pairing et modèles ne sont jamais remplacés par
un rollback automatique.

La même règle s’applique à `/var/lib/synora/connectivity/`: les identités et
`state.json` ne sont ni inclus dans le rootfs, ni régénérés par le healthcheck.
