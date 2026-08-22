# V1 — OTA de la centrale

Le chemin nominal est `synora-ota apply BUNDLE` :

1. le manifeste détaché est décodé et sa signature Ed25519 est vérifiée ;
2. la taille, le SHA-256, la version minimale de Core et la compatibilité
   matérielle sont contrôlés ;
3. l’opération est inscrite atomiquement dans le journal OTA privé ;
4. RAUC installe la bundle et reste l’autorité pour la bascule de slot ;
5. le healthcheck readonly doit réussir avant `mark-good` ; sinon Synora
   demande `mark-bad` et laisse le bootloader effectuer le rollback ;
6. au redémarrage, `synora-ota recover` traite un journal resté en phase
   `installing` ou `installed` et demande le rollback avant de poursuivre.

`internal/migrations` fournit les plans de schéma versionnés à exécuter par la
chaîne de déploiement avant validation de la nouvelle version. Une migration
échouée est redacted côté erreur et ne vaut pas `mark-good`.

Le mécanisme est non bloquant pour Core : l’installateur est une commande
séparée, bornée par contexte/timeout, et aucun chemin d’exécution de décision
ou de livraison n’attend l’OTA.
