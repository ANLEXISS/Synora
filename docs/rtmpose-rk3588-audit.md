# Audit RTMPose-s RK3588

Audit exécuté le 5 octobre 2026 sur `/home/rock/Synora-v1`, directement sur
`master`, sans modification de `/opt/synora/models` ni `/var/lib/synora`.

## Chaîne réellement détectée

| Élément | Résultat observé |
| --- | --- |
| Carte / architecture | Rockchip RK3588, `aarch64` |
| Noyau | `6.1.43-26-rk2312`, Debian 12 bookworm |
| Pilote NPU | plateforme `fdab0000.npu`, pilote noyau `RKNPU` chargé, groupe IOMMU présent |
| Service | `rknpu2.service` actif, `/usr/bin/rknn_server` |
| Runtime | `rknnlite 2.2.0`, `librknnrt.so.2.2.0` |
| Cœurs exposés | `NPU_CORE_0`, `NPU_CORE_1`, `NPU_CORE_2`, `NPU_CORE_0_1_2`, `NPU_CORE_AUTO` |
| Toolkit2 de conversion | indisponible : `import rknn.api` échoue, aucun paquet Toolkit2 détecté |
| ONNX Python / ONNX Runtime | indisponibles dans l’environnement courant |
| MMPose / MMDeploy / PyTorch | indisponibles dans l’environnement courant |

Le runtime Lite est donc présent et instanciable, mais il n’existe aucun
modèle RTMPose `.rknn` à charger et aucun Toolkit2 local pour convertir un
ONNX. La chaîne de conversion est bloquée sur cette machine.

## Source et artefacts

La source officielle OpenMMLab est décrite dans
`configs/rtmpose-s.manifest.json` : RTMPose-s body7, Apache-2.0 pour le dépôt,
17 keypoints COCO, SimCC, entrée 192×256 (largeur×hauteur), normalisation
RGB, target `rk3588`. Les poids ont une licence de redistribution non
explicitée par l’archive et ne sont pas ajoutés à Git.

Artefacts présents uniquement sous `build/rtmpose-s/` (répertoire ignoré) :

| Artefact | SHA-256 | État |
| --- | --- | --- |
| `source/rtmpose-s-official.zip` | `7673922e531014906ca4f0f239b7e233b740146a10b632deaa2a28d45470d802` | archive officielle |
| `source/rtmpose-s-body7.pth` | `acd4a1efd8f01669cadd699ce0cceeebfc0f57a008074858e0d3063110c73c94` | checkpoint body7 |
| `source/20230831/.../end2end.onnx` | `9aeb635b83f86aea45cf45d85798f7eba1a162de8e0d721c44e54fe5eebaf47d` | ONNX officiel présent |
| `rknn/rtmpose-s.rknn` | — | non généré |

L’ONNX officiel expose `simcc_x` et `simcc_y`, 17 keypoints et une dimension
spatiale statique 192×256, mais conserve un axe batch dynamique. L’export
statique demandé n’a pas pu être produit sans MMPose/MMDeploy et l’import
Toolkit2 n’a donc pas été tenté comme succès implicite.

Commandes reproductibles et réellement essayées :

```text
python3 tools/rtmpose_build.py --work-dir build/rtmpose-s export-onnx
  -> blocked: SYNORA_MMDEPLOY_ROOT, SYNORA_MMPOSE_ROOT and SYNORA_RTMPOSE_TEST_IMAGE are required

python3 tools/rtmpose_build.py --work-dir build/rtmpose-s convert-rknn
  -> blocked: static ONNX is unavailable; dynamic official archive is not accepted

SYNORA_RTMPOSE_MODEL_PATH=/tmp/nonexistent.rknn \
  python3 tools/rtmpose_build.py --work-dir build/rtmpose-s verify-runtime
  -> unavailable: model path is missing or not a file
```

## Parité, performance et clips

La parité ONNX/RKNN n’est pas mesurée : aucun RKNN RTMPose n’a été généré.
La latence NPU, prétraitement, post-traitement et totale est donc
`not_measured`. Aucune stratégie mono-cœur ou multi-cœurs n’est retenue sans
benchmark obligatoire.

Aucun clip réellement annoté de chute n’est présent dans ce dépôt. Les 40
scénarios `rtmpose_runtime` ajoutés au harnais couvrent le contrat
d’indisponibilité et restent des signaux synthétiques déclaratifs ; ils ne
sont pas une exécution RTMPose ni une validation de chute. Les autres familles
du harnais couvrent les agrégats de posture et mouvement sans revendication
de qualification.

## Harnais et état de qualification

`make test-central-v1` contient désormais 401 scénarios : 106 statiques, 90
pose/mouvement, 70 face historique du harnais, 50 communication, 45 caméra et
40 disponibilité RTMPose. Le rapport marque RTMPose `unavailable`, expose la
latence pose à zéro pour les agrégats synthétiques et distingue le backend
réel du mode simulé. Aucun keypoint brut, image, crop ou identifiant local ne
sort vers le bus ou le Store.

État RTMPose : `unavailable`. V3 reste `active_dry_run`, sans promotion,
audio ou action physique. La qualification réelle exige un hôte doté de
Toolkit2 compatible, l’export statique, un RKNN chargé sur ce RK3588, la
parité indépendante et des clips labellisés séparés.
