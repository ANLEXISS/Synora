# Partition layout OTA Synora Founders Edition

Ce document décrit une cible de conception. Il ne partitionne aucune machine,
n’installe pas RAUC et ne modifie pas le bootloader Radxa.

## Contraintes observées

Le runtime actuel installe les binaires et services sous `/opt/synora`, les
unités sous `/etc/systemd/system`, les templates sous
`/opt/synora/config-templates`, et les modèles sous
`/var/lib/synora/models`. La webapp rootfs est servie depuis `/opt/synora/web`;
`/var/lib/synora/web` est seulement le fallback prototype configurable.

Les données runtime sont donc à traiter comme persistantes :

- `/etc/synora/`, y compris `secrets/`, configs device, résidents,
  automations et policies locales ;
- `/var/lib/synora/state/state.json` et ses backups ;
- `/var/lib/synora/clips/`, `/var/lib/synora/logs/`, `/var/lib/synora/debug/` ;
- `/var/lib/synora/vision/face/` ;
- `/var/lib/synora/cge/`, dont feedback et mémoires ;
- `/var/lib/synora/auth/sessions.json` ;
- `/var/lib/synora/connectivity/`, dont les identités ne doivent jamais être
  remplacées par une OTA ou un rollback ;
- `/var/lib/synora/models/` dans la cible recommandée.

`/run/synora` est éphémère et ne doit jamais être une source de rollback.

## Option 1 — A/B classique

```text
/boot       boot artifacts et metadata bootloader
/rootfs_A   système et runtime Synora version 1
/rootfs_B   système et runtime Synora version 2
/data       /etc/synora + /var/lib/synora, dont les modèles
```

Contenu de chaque rootfs : OS Debian minimal/Radxa OS, kernel et modules
compatibles, `/opt/synora/bin`, vision-worker, MediaMTX, unités systemd,
templates, webapp et outils de healthcheck.

Avantages : architecture simple, peu de mounts, rollback rapide et modèles
conservés sans téléchargement.

Risques : une image OTA peut grossir si elle embarque les modèles ; une
évolution de modèle et une évolution de runtime deviennent plus couplées ;
une corruption de `/data` affecte simultanément configs, données et modèles.

Taille indicative : 8 à 12 GiB par rootfs, `/boot` 512 MiB à 1 GiB, `/data`
au moins 32 GiB et davantage si les clips sont conservés localement.

## Option 2 — A/B avec modèles séparés

```text
/boot       boot artifacts et metadata bootloader
/rootfs_A   système et runtime Synora version 1
/rootfs_B   système et runtime Synora version 2
/data       /etc/synora et données utilisateur
/models     modèles RKNN et releases de modèles
```

Le point de montage compatible reste `/var/lib/synora/models`, par montage
direct, bind mount ou lien géré par l’image. Le code ne doit pas dépendre d’un
chemin alternatif non documenté.

Avantages : bundles système plus petits, mise à jour IA indépendante, deux
versions de modèles conservables, et rollback système sans réinstaller les
modèles.

Risques : mount supplémentaire obligatoire au démarrage, compatibilité
runtime/modèle à contrôler dans le manifest, et risque de démarrer un ancien
rootfs avec un modèle trop récent si l’activation n’est pas atomique.

Taille indicative : 8 à 12 GiB par rootfs, `/boot` 512 MiB à 1 GiB, `/data`
32 GiB minimum, `/models` 16 GiB minimum pour deux générations de modèles et
des fichiers temporaires. Ces tailles doivent être confirmées avec la taille
réelle des bundles, les clips et les marges d’écriture eMMC/NVMe.

## Choix cible

La cible Founders Edition est l’option 2. Les modèles sont volumineux,
évoluent à un rythme différent du code et leur absence ne doit pas empêcher un
rollback du système. Une release de modèle doit toutefois déclarer son
`rknn_runtime_expected`, ses modèles requis/optionnels et sa compatibilité de
schema.

La première implémentation peut conserver physiquement
`/var/lib/synora/models`, puis introduire une partition labelisée
`SYNORA_MODELS` montée à cet emplacement. Cette transition doit être traitée
comme une migration d’image avec copie vérifiée, jamais comme une suppression.

La webapp est un artefact rootfs sous `/opt/synora/web`. Pendant la transition,
l’API peut lire `/var/lib/synora/web` si le chemin rootfs n’existe pas, mais ce
répertoire n’est pas copié par le plan OTA. Le service API publie le chemin
effectivement configuré via le health de la webapp.

## Politique clips et logs

Les clips restent sur `/data` et ne sont pas supprimés par un rollback. Ils
sont supprimables uniquement par la politique de rétention locale ou une
action admin explicite. Une mise à jour ne doit pas confondre nettoyage de
rétention et rollback.

Les logs sont persistants pour le diagnostic OTA, mais bornés par rotation
(par exemple durée et quota configurables, avec une réserve pour les derniers
rapports de boot). Les journaux anciens peuvent être supprimés par rotation ;
les logs nécessaires au rapport d’un échec de boot doivent être conservés
jusqu’à leur remontée au serveur de graduation. Aucun log ne contient de
secret.

## Règles de montage et rollback

- `/data` et `/models` doivent être montés avant les services Synora ; leur
  absence bloque le healthcheck et empêche `mark-good`.
- `/etc/synora` doit être disponible en lecture avant `synora-api`, Core et
  Discovery ; les secrets ne sont jamais inclus dans un bundle RAUC.
- `/boot` ne doit pas mélanger un kernel d’un slot et un rootfs de l’autre.
  La relation exacte entre Radxa U-Boot, rk2312 et les slots RAUC reste à
  valider sur le matériel cible.
- Le rollback change le slot système et conserve `/data`, `/models`, les
  configs, secrets, sessions et données utilisateur.
- Les migrations qui changent un format persistant doivent créer un backup
  avant écriture et rester lisibles par l’ancien slot jusqu’au `mark-good`.
