# V1 — OTA de la centrale

Le chemin nominal est `synora-ota apply BUNDLE` :

1. le manifeste détaché et la signature RSA/SHA-256 sont vérifiés contre la
   chaîne X.509 centrale et la CRL ;
2. le produit, la cible, la taille, le SHA-256, la génération de sécurité, la
   version minimale de Core et la compatibilité matérielle sont contrôlés ;
3. l’opération est inscrite atomiquement dans le journal OTA privé ;
4. RAUC installe le bundle et reste l’autorité pour la bascule de slot ;
5. les migrations ordonnées s’exécutent avec checkpoint avant le health gate ;
6. le healthcheck readonly et la stabilité de 120 secondes doivent réussir
   avant `mark-good` ; sinon Synora demande `mark-bad` et laisse le bootloader
   effectuer le rollback ;
7. au redémarrage, `synora-ota recover` traite un journal resté en phase
   `installing` ou `installed` et demande le rollback avant de poursuivre.

`internal/migrations` fournit les plans de schéma versionnés, idempotents et
restaurables depuis un checkpoint. Une migration échouée est redacted côté
erreur, restaurée si nécessaire et ne vaut pas `mark-good`.

Le mécanisme est non bloquant pour Core : l’installateur est une commande
séparée, bornée par contexte/timeout, et aucun chemin d’exécution de décision
ou de livraison n’attend l’OTA. Le keyring central n’accepte pas les bundles
signés par la chaîne caméra ou la chaîne de développement.
