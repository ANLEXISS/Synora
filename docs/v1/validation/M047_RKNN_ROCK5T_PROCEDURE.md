# M047 — validation Vision sur Rock 5T

Ce document est une procédure de recette matérielle pour la centrale Rock 5T.
Aucun résultat ne doit être produit à partir d’un fixture, d’un autre hôte ou
d’une simulation. Tant que la carte n’est pas identifiée et que les mesures ne
sont pas jointes, M047 reste `blocked_no_target_results`.

La campagne couvre le chemin RKNN/NPU des modèles `.rknn` et les mesures CPU
du pipeline. Le CPU est utilisé pour les étapes de capture, prétraitement,
post-traitement, orchestration et charge de référence. Une inférence complète
sur CPU nécessiterait des artefacts explicitement compatibles CPU ; aucun
modèle hors contrat V1 n’est ajouté ou inventé ici.

## Préconditions

- carte Rock 5T identifiée par son numéro d’inventaire et le modèle lu dans
  `/proc/device-tree/model` ;
- Debian 12/Radxa OS, alimentation stable, refroidissement installé ;
- trois sources vidéo de test non biométriques ou flux synthétiques locaux ;
- modèles fournis par la release, avec manifeste et SHA-256 ;
- sortie dédiée sous `docs/v1/validation/runs/M047-<date>/`, permissions 0700,
  ne contenant ni image faciale, ni embedding, ni token, ni secret.

## Identification et état initial

Exécuter sur la carte et conserver uniquement les sorties expurgées :

```sh
set -eu
test "$(tr -d '\000' </proc/device-tree/model)" = "Radxa ROCK 5T"
uname -a
cat /etc/os-release
tr -d '\000' </proc/device-tree/model
lscpu
free -h
df -B1 /var/lib/synora /var/lib/synora/models
find /var/lib/synora/models -maxdepth 1 -type f -name '*.rknn' -printf '%f\n' | sort
sha256sum /var/lib/synora/models/arcface_w600k_r50.rknn \
  /var/lib/synora/models/det_10g.rknn \
  /var/lib/synora/models/yolov8.rknn
python3 -c 'from rknnlite.api import RKNNLite; print("rknnlite_import=ok")'
```

Un modèle requis absent, une empreinte différente, un runtime importable mais
non initialisable ou une carte non conforme arrête la recette.

## Modèles et NPU

Pour chacun de SCRFD (`det_10g.rknn`), ArcFace 512
(`arcface_w600k_r50.rknn`) et YOLOv8n (`yolov8.rknn`) :

1. charger le modèle avec `RKNNLite.load_rknn` ;
2. initialiser le runtime sur chacun des cœurs NPU prévus par la configuration ;
3. exécuter au moins 100 inférences avec une entrée de test non biométrique ;
4. libérer le runtime et répéter après redémarrage du worker ;
5. enregistrer seulement succès/échec, code d’erreur, durée, mémoire et
   compteur d’inférence.

`weapon.rknn` est optionnel : son absence est `degraded`, jamais un échec M047.
Une sortie de modèle n’est pas enregistrée dans les preuves.

## Campagne CPU et charge mixte

Mesurer le même worker avec le chemin CPU disponible pour les étapes non-NPU,
sans modifier les seuils métier ni masquer une absence de backend :

```sh
python3 -c 'import cv2, numpy; print("cpu_prepost=ready")'
pidof -x worker.py
ps -o pid,etime,%cpu,%mem,rss,cmd -C python3
free -b
cat /proc/loadavg
```

Comparer, pour une même campagne de trois sources, le temps CPU de capture,
prétraitement et post-traitement avec le temps RKNN/NPU d’inférence. Toute
demande d’inférence complète CPU doit être marquée `blocked_cpu_model_artifact`
tant qu’un modèle CPU versionné, checksumé et explicitement autorisé par le
manifeste n’existe pas. `VISION_FORCE_CPU` ne doit pas être présenté comme une
réussite si le runtime ne fournit pas ce backend.

## Charge et mesures

Avec le worker réel et trois sources locales, mesurer séparément puis ensemble :

- latence p50/p95/p99 par modèle et par source ;
- RSS du worker, mémoire libre et mémoire NPU au début et à la fin ;
- utilisation CPU/NPU, température maximale et fréquence observée ;
- erreurs, retries, files et arrêt propre ;
- durée de fonctionnement minimale de 30 minutes pour la campagne courte ;
- démarrage, arrêt, redémarrage du worker et retour à `ready`.

Commandes d’observation non destructives :

```sh
for f in /sys/class/thermal/thermal_zone*/temp; do printf '%s=' "$f"; cat "$f"; done
cat /sys/kernel/debug/rknpu/load 2>/dev/null || true
```

Le seuil de température et les budgets sont ceux de la carte et de la release
validés par Alexis ; aucune modification de seuil métier n’est permise dans
M047. Tout throttling, crash, croissance mémoire, fuite de descripteur ou
retour non autonome à `ready` est un échec.

## Dataset et trois sources

Vérifier que le dataset facial actif, ou l’état explicite « aucun dataset
configuré », est chargé sans exposer de biométrie. Faire tourner les trois
sources en parallèle, puis en déconnecter une à la fois. Une perte de source
ne doit ni bloquer les deux autres ni faire perdre silencieusement la santé du
worker. Rejouer après rotation de version du dataset et redémarrage.

## Critères de décision

Le rapport final doit être un JSON signé ou checksumé localement, avec :

```json
{
  "schema_version": 1,
  "milestone": "M047",
  "target": "Radxa ROCK 5T",
  "physical_qualification": "pass|fail|blocked_no_target_results",
  "cpu_inference": "measured|blocked_cpu_model_artifact",
  "models": {},
  "sources": 3,
  "latency_ms": {},
  "memory": {},
  "temperature_c": {},
  "restarts": [],
  "errors": [],
  "biometric_payloads_stored": false
}
```

`pass` exige des mesures réelles pour tous les modèles requis, les trois
sources, les redémarrages, les budgets, le chemin CPU demandé et le retour à
`ready`. En l’absence de mesures, le seul statut valide est
`blocked_no_target_results`. Codex ne crée pas ce rapport à l’avance et ne
marque pas M047 réussi sur la seule présence de cette procédure.
