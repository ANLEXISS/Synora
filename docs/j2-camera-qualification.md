# J2 — Rapport de qualification caméra

Contrôle read-only exécuté le 2026-10-06 depuis le commit `0b6aae0` :

```text
make qualify-camera OUT=/tmp/synora-camera-qualification-current.json
decision=not_qualified cameras=2 active_paths=0
```

## Résultat

| Élément | Résultat |
| --- | --- |
| Caméras déclarées | `cam_03`, `cam_04` |
| Caméras actives et appairées | 0 |
| Identités caméra actives | 0 |
| Endpoints RTSP/ingress configurés | 0 |
| Chemins MediaMTX actifs | 0 |
| Santé Discovery | `503 degraded` |
| Qualification | `not_qualified` |

Motifs communs aux deux caméras : confiance réseau différente de `paired`,
identité active absente, endpoint absent et aucun chemin MediaMTX actif.

Le rapport est volontairement non bloquant pour le système : il ne tente ni
pairing, ni modification de `devices.yaml`, ni ajout de chemin MediaMTX. Une
caméra ne pourra passer à `transport_qualified` que lorsque l’identité active,
la confiance réseau, l’endpoint et le chemin MediaMTX seront tous observés.
Cette qualification transport ne suffira pas encore à clôturer J2 : il faudra
ensuite la preuve du parcours réel Discovery → Vision → Core, la reprise après
panne et la rétention sur le RK3588.

Le harnais central apporte désormais une **preuve E2E de transport et
résilience simulés** avec ingress Discovery HTTP local et worker de test sans
modèle. Ce jalon n’est pas une preuve d’inférence Vision et ne modifie pas la
décision ci-dessus : J2, J3 et J4 restent non validés.

## Préflight et extension J2 (2026-10-16)

Le préflight matériel en lecture seule a trouvé `/dev/video0` piloté par
`rk_hdmirx`, un récepteur de capture HDMI (pas une caméra autonome identifiée).
V4L2 annonce les capacités de capture, mais son contrôle `power_present` vaut
0 ; aucun flux n’est présent. MediaMTX et Discovery de production sont actifs,
mais ils n’ont pas été interrogés par un flux de qualification, modifiés,
redémarrés ou utilisés. Aucun worker Vision de production n’est actif. Le
backend RKNN disponible ne constitue pas un backend de santé caméra.

Décision du rapport central :

```text
camera_j2_status=prepared_blocked_on_real_camera
camera_j2_source=unavailable
```

Le profil logiciel isolé est exécuté par `make test-central-v1` ; pour écrire
un rapport dédié sans remplacer le rapport central par défaut, la même commande
du harnais peut être lancée avec `go run ./cmd/synora-central-test --out
/tmp/synora-camera-j2-report.json`. Chaque
parcours conserve son bus, son Store et son état sous `/tmp`, démarre Discovery,
Core et le worker de test en processus, et utilise des listeners éphémères
loopback pour l’ingress et l’API `/api/system/state`. L’API de test exige un
jeton éphémère ; le rapport ne contient pas ce jeton. Le worker ne charge aucun
modèle, ne lit/décode pas de média et renvoie `unavailable`. Les 18 nouveaux
cas déclaratifs sont répartis entre `camera_ingress_j2` et `camera_health_j2`.
Ils traversent le chemin central avec provenance `simulated_test` ; ce sont
des contrôles logiciels, pas des observations du récepteur HDMI. Les autres
parcours mock du harnais testent l’acceptation HTTP, disponibilité contrôlée,
timeout, doublon et reprise.

`camera_health_detail` est une extension additive et fermée
`synora.camera.health/v1` dans Evidence V1. Ses seules sorties sont `healthy`,
`stream_missing`, `frozen_frame`, `black_or_occluded`, `degraded`,
`unavailable` et `recovered`. Elle ne transporte que provenance, horodatage,
fenêtre, confiance et qualité bornées. Aucun état détail n’est projeté dans les
86 dimensions V3 : il est conservé dans l’évidence validée et le Store.
Validation Go et Python partagée par le golden fixture ; les inconnus, scores
hors limites, temps incohérents et données brutes sont rejetés.

### Protocole opérateur à exécuter ultérieurement

Il faut d’abord confirmer par l’opérateur que la source HDMI est une caméra
autorisée, puis ouvrir un profil J2 séparé qui ne pointe que vers cette source.
Les essais manuels à consigner sont :

1. scène normale stable et ACK ingress ;
2. débranchement/perte de lien et mesure de la détection ;
3. reconnexion et mesure de récupération ;
4. cache de lentille ;
5. scène immobile volontairement sur la fenêtre convenue ;
6. retour à une scène normale.

Le protocole est préparé, mais aucun de ces gestes n’a été exécuté par le
harnais. Les futures mesures doivent étiqueter explicitement leur provenance
(`real_camera`, `real_local_stream`, `local_replay` ou `simulated_test`) et
rapporter ACK ingress, première Evidence, détection de perte, récupération,
temps par segment, profondeur de file, segments ignorés/rejetés, CPU/RAM/NPU,
écritures Store et doublons. Aucun seuil de réussite n’est inventé ici.

Promotion possible uniquement après un parcours `real_local_stream` vérifié,
puis essais `real_camera` avec les étapes opérateur, mesures reproductibles,
review redaction et vérification de non-régression des services de production.
Une source replay ne devient jamais `real_camera`. J3/J4, inférence Vision,
pose, chute et modèles RKNN restent hors qualification.
