# Qualification caméra et réseau V1

J22 fournit un protocole et un rapport reproductibles pour les trois caméras
V1 déclarées dans `configs/devices.yaml`. Le fixture versionné est explicitement
`synthetic_fixture_not_physical_evidence`: il permet de vérifier le calcul et
le format, pas de déclarer un résultat obtenu sur une caméra.

## Exécution

```sh
python3 -B -m unittest tools.tests.test_v1_camera_network_qualification -v
python3 -B tools/v1_camera_network_qualification.py \
  --fixture docs/v1/V1_CAMERA_NETWORK_FIXTURE.json \
  --output /run-local/synora-camera-network/report.json
```

Le rapport couvre pour chaque caméra les phases jour/nuit, les transitions
IR-cut, le nombre envoyé/livré/perdu, les latences observées et les tentatives
de reconnexion SynoraNet. Il impose les identités `cam_01`, `cam_02`, `cam_03`
et vérifie que les données sont parseables. Les pertes et latences sont
rapportées sans seuil inventé.

## Décisions de périmètre

- PIR et Doppler sont `not_attached` dans le fixture : aucune détection physique
  ne peut être qualifiée ici.
- Le microphone de caméra est `disabled_for_camera_v1`; les microphones des
  relais vocaux existants ne sont pas réattribués aux caméras.
- Chute, arme et tamper sont `disabled_pending_qualification` jusqu’à disposer
  de données et d’un protocole cible. Le rapport ne les transforme pas en
  capacités disponibles.
- Le statut global reste
  `fixture_observed_physical_qualification_blocked` et
  `blocked_no_target_confirmation` tant qu’une unité cible, son BOM confirmé
  et les mesures jour/nuit/IR-cut/réseau réelles ne sont pas joints.

La qualification réelle doit compléter ce même schéma avec les numéros de
série et les mesures horodatées des trois caméras, sans modifier les décisions
de capacité par omission.
