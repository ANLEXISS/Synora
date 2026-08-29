# M046 — préparation de release signée

Cette étape prépare le format de release sans arrêter de décision sur la
racine de confiance ni sur la stratégie opérationnelle de rollback.

Le manifeste JSON signé contient la version, la compatibilité matérielle, la
taille et le SHA-256 du bundle, ainsi que la cible de migration. La signature
Ed25519 est vérifiée après lecture du bundle et avant toute installation. Une
version candidate inférieure à la version Core courante, ou une cible de
migration inférieure au schéma déjà appliqué, est refusée.

Le contrôleur conserve un journal transactionnel, n’exécute le health gate
qu’après installation et ne demande le marquage sain qu’après le healthcheck
en lecture seule. Les échecs d’installation, d’espace ou de readiness
déclenchent le chemin de marquage mauvais déjà délégué au backend OTA.
L’infrastructure de migrations refuse une cible non planifiable avant toute
écriture et reste idempotente.

Les preuves locales couvrent manifeste non signé, signature invalide, bundle
corrompu, downgrade, migration downgrade/non planifiable, espace insuffisant,
health gate en échec et reprise d’un journal interrompu.

Décisions encore requises par Alexis avant M046 complet :

- racine de confiance, rotation et conservation des clés ;
- procédure de récupération et révocation ;
- politique opérationnelle de rollback central et caméras ;
- conditions matérielles de reprise et d’acceptation finale.

Aucune clé de production, racine système ou installation matérielle n’est
modifiée par cette préparation.
