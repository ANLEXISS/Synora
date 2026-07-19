# OTA et rollback Synora Founders Edition

Cette stratégie vise une centrale Radxa OS/Debian minimal avec kernel Radxa
rk2312 et RAUC sur rootfs A/B. Elle est conceptuelle : aucune installation
RAUC, écriture de slot, partitionnement, reboot ou modification système n’est
réalisée dans cette passe.

## Invariants

Un bundle OTA ne contient jamais `/etc/synora/secrets`, tokens, PSK, mots de
passe, clés privées ou données de maison. Il peut contenir les templates safe,
mais l’activation se fait contre la configuration runtime persistante.

Le rootfs contient l’OS, kernel/modules associés, binaires Synora, worker,
MediaMTX, unités systemd, webapp rootfs, templates, `synora-check`,
`synora-connect` et le healthcheck. `/data`, `/models` et
`/var/lib/synora/connectivity` restent hors des slots.

Une mise à jour n’est considérée bonne qu’après migration réussie et
healthcheck post-boot complet. Avant ce point, le bootloader doit conserver un
compteur d’essais et pouvoir revenir au slot précédent.

## Transaction OTA

1. Télécharger le manifest et le bundle dans un espace temporaire persistant.
2. Vérifier signature, certificat de confiance, taille, hash et `bundle_id`.
3. Vérifier la carte cible, la version minimale du bootloader/kernel, le
   schema de config, le modèle RKNN et l’espace disponible.
4. Vérifier que `/data`, `/models` et les backups nécessaires sont lisibles.
5. Installer le bundle dans le slot inactif avec RAUC.
6. Positionner le slot inactif en `pending`, avec nombre d’essais limité.
7. Au prochain boot, exécuter migration puis `synora-boot-healthcheck`.
8. Appeler `mark-good` uniquement si toutes les conditions bloquantes sont
   satisfaites.

Une panne avant reboot ne change pas le slot actif et ne déclenche pas de
rollback système. Elle doit laisser le slot précédent et la configuration
runtime intacts.

## Contrat de décision

| Condition | Founders par défaut | Action |
|---|---|---|
| Échec téléchargement, signature ou hash | fatal | Ne pas installer |
| Échec écriture du slot inactif | fatal | Rester sur le slot actif |
| Boot du nouveau slot impossible | fatal | Bootloader vers slot précédent |
| `synora-bus` ou `synora-core` KO | fatal | Rollback |
| `synora-api` KO ou health API inaccessible | fatal | Rollback |
| `/etc/synora` illisible | fatal | Rollback |
| `/var/lib/synora` ou `/models` illisible | fatal | Rollback |
| Driver Wi-Fi principal absent avec SynoraNet enabled | fatal | Rollback |
| SynoraNet AP indisponible | fatal par défaut ; degraded seulement si policy explicite | Décision du manifest |
| NPU/RKNN runtime absent pour les modèles requis | fatal | Rollback |
| Vision worker degraded pour une capacité optionnelle | acceptable | `mark-good` avec warning |
| `weapon.rknn` absent | acceptable | `weapon_detection=degraded` |
| WhatsApp/provider absent ou dry-run | acceptable | Warning |
| HTTPS 8443 absent | acceptable uniquement prototype | Fatal en Founders final |
| Config runtime incompatible sans migration | fatal | Rollback |
| Migration échouée ou backup impossible | fatal | Rollback |
| Modèle requis absent | fatal | Rollback ou bloquer activation modèle |

Le mode degraded réseau doit être explicitement déclaré dans le manifest et
dans la policy locale ; il ne peut pas être déduit d’un timeout arbitraire.

## Version et manifest

Chaque rootfs doit embarquer un fichier non secret, par exemple
`/opt/synora/version.json` :

```json
{
  "image_version": "semver-or-build-id",
  "synora_version": "semver-or-build-id",
  "git_commit": "commit-id",
  "build_time": "RFC3339 timestamp",
  "target_board": "rock-5-itx",
  "os_base": "radxa-debian-bookworm",
  "kernel_expected": "6.1.43-26-rk2312",
  "rknn_runtime_expected": "runtime-version",
  "config_schema_version": 1,
  "bundle_id": "opaque-bundle-id"
}
```

Le manifest signé associé ajoute les hashes de chaque artefact, la liste des
unités/services, `config_schema_min`, `config_schema_max`, migrations requises,
modèles RKNN requis/optionnels, policy healthcheck et policy rollback. Il doit
indiquer clairement si la webapp est dans le rootfs ou dans un chemin de
compatibilité transitoire.

Le code expose désormais `GET /api/system/version`; `synora-boot-healthcheck
run --readonly` vérifie cette route avant de permettre `mark-good`. Le manifest
ne contient ni token, ni PSK, ni certificat privé. Les valeurs
d’exemple ci-dessus sont uniquement des formats.

## Migrations persistantes

Les migrations sont versionnées et idempotentes, par exemple :

```text
migrations/
  0001_network_security.yaml
  0002_features_flags.yaml
  0003_cge_decay.yaml
```

Chaque migration doit :

- déclarer sa version source et cible ;
- valider avant écriture ;
- créer un backup atomique de la config ou donnée concernée ;
- ne jamais modifier ou régénérer un secret ;
- être rejouable après interruption ;
- laisser l’ancien rootfs capable de lire la donnée pendant la fenêtre
  `pending`, ou fournir une restauration sûre avant `mark-good`.

Une migration échouée est fatale. Les backups restent persistants et ne sont
pas supprimés par un rollback automatique. Les messages de migration sont
redacted et ne mentionnent jamais le contenu des secrets.

## Intégration RAUC future

La configuration RAUC cible devra définir un `system.conf` avec une paire de
slots système, par exemple `rootfs.0` et `rootfs.1`, associés aux partitions
`rootfs_A` et `rootfs_B`. Le backend bootloader reste à investiguer sur la
combinaison Radxa/RK2312/U-Boot ; il faut confirmer la lecture/écriture de
l’état slot, le compteur d’essais et la relation `/boot` avant toute mise en
production.

Structure illustrative à valider sur le matériel, non installable telle quelle :

```ini
[system]
compatible=Synora Radxa Founders Edition
bootloader=uboot
boot-attempts=3

[keyring]
path=/etc/rauc/synora-bundle-ca.pem

[slot.rootfs.0]
device=/dev/disk/by-partlabel/SYNORA_ROOT_A
type=ext4
bootname=A

[slot.rootfs.1]
device=/dev/disk/by-partlabel/SYNORA_ROOT_B
type=ext4
bootname=B
```

Les noms de partitions, la syntaxe bootloader et les chemins de clé doivent
être confirmés dans une phase dédiée Radxa/RAUC.

Le bundle RAUC devra contenir au minimum :

- manifeste RAUC et manifest Synora signé ;
- rootfs complet ou delta validé par la politique ;
- version, commit et board cible ;
- hashes des artefacts et migrations ;
- policy post-install et post-boot.

Un certificat public de vérification est embarqué dans l’image de confiance ;
la clé privée de signature reste hors de la centrale. Un hook post-install
vérifie le contenu du slot inactif sans démarrer de services persistants. Un
hook post-boot ou une unité de santé lance migration puis
`synora-boot-healthcheck`. Seul le contrôleur de boot, après sortie zéro,
appelle `mark-good`.

La commande opérationnelle future sera fournie par l’orchestrateur RAUC, par
exemple `rauc install <bundle>`, mais elle ne fait pas partie de cette passe.

## Scénarios demandés

- Échec avant reboot : le slot courant reste actif ; le bundle temporaire est
  marqué failed et conservé pour diagnostic sans conserver de secret.
- Boot échoué : le bootloader consomme un essai ; après le seuil, il reprend le
  slot précédent.
- API/Core/Bus indisponible : le healthcheck retourne 1, aucun `mark-good`.
- SynoraNet KO : fatal si le profil Founders l’exige ; sinon degraded explicite
  dans le rapport.
- Driver Wi-Fi absent : fatal lorsque SynoraNet est activé ; aucune action de
  réparation driver automatique dans le healthcheck.
- NPU indisponible : fatal pour les modèles requis ; weapon seul reste
  optionnel.
- Config incompatible : migration versionnée avant `mark-good`; sinon retour
  au slot précédent sans réécriture des secrets.
- Modèle manquant : `weapon.rknn` warning/degraded ; modèle primaire manquant
  fatal.

## Rollback et données

Le rollback ne restaure pas `/etc/synora`, `/data` ou `/models` avec une copie
du bundle. Il revient au code précédent et laisse les données persistantes à
leur version migrée. Une migration doit donc être rétrocompatible pendant la
fenêtre pending, ou fournir un mécanisme de restauration de backup avant
`mark-good`.

Après `mark-good`, un retour arrière est une opération opérateur distincte :
le serveur doit conserver le slot précédent, le manifest et le rapport de
healthcheck, sans supprimer les données utilisateur.

## Critères d’acceptation OTA

Une version n’est graduée que si le healthcheck retourne zéro, que les logs
récents ne contiennent pas de panic/fatal runtime, que la version exposée
correspond au manifest, et que le rapport JSON est écrit de façon atomique
dans `/var/lib/synora/logs/` sans bearer token, PSK, mot de passe ou contenu de
clé privée.
