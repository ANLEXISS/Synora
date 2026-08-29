# M046 — release signée et rollback V1

La politique humaine M046 est validée. La release de production utilise une
PKI RAUC X.509 distincte pour la centrale et les caméras ; la chaîne de
développement est séparée et n’est jamais acceptée en production.

Le manifeste JSON signé contient l’identifiant produit, la cible centrale ou
caméra, la version, la génération de sécurité monotone, la compatibilité
matérielle, la taille, le SHA-256, le signer et la cible de migration. La
signature RSA/SHA-256 et la chaîne X.509 sont vérifiées avant installation ;
RAUC effectue la vérification finale du bundle avec `check-purpose=codesign`
et `check-crl=true`. Le chemin Ed25519 reste uniquement un adaptateur de
compatibilité pour les consommateurs existants.

Le contrôleur conserve un journal transactionnel, refuse les downgrades de
version, génération ou migration, installe dans le slot géré par RAUC et
n’exécute le health gate qu’après installation. `mark-good` exige un
healthcheck readonly puis une stabilité durable de 120 secondes ; les échecs
d’installation, d’espace ou de readiness demandent `mark-bad`. Les caméras
utilisent le même profil de release, restent autonomes et exigent 60 secondes
de stabilité avant validation. Les migrations sont ordonnées, idempotentes,
transactionnelles et restaurables depuis leur checkpoint.

Les preuves locales couvrent manifeste non signé ou modifié, chaîne et signer
révoqués, mauvaise cible, rotation progressive de racine, bundle corrompu,
downgrade, migration downgrade/non planifiable, espace insuffisant, coupure,
health gate et stabilité en échec, compteur/fallback simulé, rollback caméra
et préservation des données persistantes.

La politique de conservation, rotation, révocation et récupération est
documentée dans `docs/ota-rollback.md`. Aucun secret réel n’est présent dans
le dépôt : les fixtures et le script `tools/ota/create_test_pki.sh` ne
produisent que des clés de test à la demande. La validation matérielle RK3588
reste le périmètre humain de M047 et n’est pas simulée ici.
