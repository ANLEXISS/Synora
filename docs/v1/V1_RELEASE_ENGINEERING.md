# Release engineering V1

J24 fournit un manifeste source reproductible, un inventaire SBOM/licences,
un plan de provisioning, un protocole burn-in, des diagnostics support
expurgés et une matrice réglementaire sans fausse certification.

```sh
python3 -B -m unittest tools.tests.test_v1_release_engineering -v
python3 -B tools/v1_release_engineering.py check
python3 -B tools/v1_release_engineering.py manifest
python3 -B tools/v1_release_engineering.py sbom
python3 -B tools/v1_release_engineering.py provisioning
python3 -B tools/v1_release_engineering.py burn-in
python3 -B tools/v1_release_engineering.py support
```

Le manifeste ne fabrique pas une image: il hache les fichiers suivis du dépôt,
sans timestamp ni donnée d’hôte. La signature est `blocked_external_key_required`
car aucune clé privée ne doit être présente dans le dépôt. Le SBOM relève les
licences disponibles dans `package-lock.json`; les licences Go restent à
résoudre par la chaîne d’approvisionnement, ce qui est déclaré comme limite.

Le plan d’installation reste sans mutation et garde les secrets hors Git. Le
burn-in et la coupure réelle exigent une unité cible et doivent produire des
preuves opérateur. CE, RED, RoHS et WEEE sont tous `external_evidence_required`;
la matrice n’est pas une certification.
