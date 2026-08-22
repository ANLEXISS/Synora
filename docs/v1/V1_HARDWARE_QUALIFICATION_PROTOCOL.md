# J21 — protocole de qualification hardware

Le dépôt ne contient pas de BOM matérielle complète et jointe. Le fichier
`V1_HARDWARE_BOM_REFERENCE.json` fige seulement les références déjà prouvées
dans le dépôt et énumère les champs externes obligatoires. Le harness refuse
d’inférer un numéro de série, une révision, un modèle d’alimentation, une
endurance SSD ou un capteur.

## Harness

```text
python3 tools/v1_hardware_qualification.py doctor
python3 tools/v1_hardware_qualification.py soak --output /run-local/synora-hw --duration 900 --interval 5
python3 tools/v1_hardware_qualification.py power-cut --output /run-local/synora-hw --phase after_sample_write
python3 tools/v1_hardware_qualification.py report --output /run-local/synora-hw
```

`sample`/`soak` collectent température thermique, charge, capacité libre,
compteurs réseau et secteurs écrits par bloc. Un fixture JSON permet une
qualification logicielle reproductible. Chaque mesure porte sa source et le
rapport reste `blocked_until_target_unit_and_bom_are_attached` tant qu’une
unité physique et sa BOM n’ont pas été confirmées.

Le scénario `power-cut` est assisté : il journalise une interruption à une
phase précise et vérifie la reprise atomique du journal. Il ne coupe jamais
l’alimentation automatiquement et ne prétend donc pas qualifier une coupure
réelle.

Les seuils thermiques, endurance SSD, charge, réseau et durée de soak doivent
être renseignés avec la fiche constructeur et les mesures de la BOM jointe;
aucun seuil absent n’est inventé par V1.
