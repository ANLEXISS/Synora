# OTA V1 — release signée et rollback

Synora délègue l’intégrité du bundle et la sélection des slots à RAUC. La
centrale et les caméras utilisent des keyrings X.509 distincts : un bundle
caméra ne peut pas être accepté par la centrale, et inversement. Les templates
correspondants sont dans `deployments/rauc/` ; ils ne sont pas installés par
les tests et leurs partitions doivent être confirmées sur le matériel.

Le manifeste détaché contient le produit, la cible, la version, la génération
de sécurité monotone, les migrations, les checksums et l’identité du signer.
Le préflight vérifie la chaîne RSA/SHA-256, l’usage code signing, la CRL, la
compatibilité et l’anti-downgrade. RAUC revérifie le bundle avec
`check-purpose=codesign` et `check-crl=true` avant d’écrire le slot inactif.

La centrale démarre le nouveau slot avec trois tentatives maximum. Les
migrations transactionnelles précèdent le health gate ; `mark-good` exige un
healthcheck readonly puis 120 secondes de stabilité. La caméra suit le même
contrat avec trois tentatives et 60 secondes de stabilité. Un échec demande
`mark-bad` et le bootloader revient au dernier slot sain. `/data`, l’identité,
les secrets d’appairage, les incidents, les clips et les modèles persistants
ne sont jamais effacés par ce rollback.

Les clés privées de production restent hors dépôt, hors CI et hors appareil.
`tools/ota/create_test_pki.sh` ne produit que du matériel jetable pour les
tests locaux. Une compromission de racine nécessite la procédure de récupération
physique hors ligne ; elle ne peut pas être réparée par la racine compromise.
