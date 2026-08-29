# V1 — OTA caméra et récupération

`internal/cameraota` formalise l’agent OTA des Zero 3W : manifeste RSA/SHA-256
avec chaîne X.509 caméra, CRL, génération monotone, checksum, modèle et
version minimale du bootloader. Le transport réel est injecté derrière une
interface afin que la qualification locale simule les pertes radio et les
redémarrages sans prétendre disposer du matériel.

Une caméra hors ligne n’est pas considérée comme mise à jour : l’opération est
conservée en phase `pending` dans un fichier `0600` et sera rejouée lorsque le
transport la signalera en ligne. Les phases `installing`, `rebooting` et
`validating` sont récupérées par `Recover`; une interruption à ces étapes
demande `mark-bad`. Le healthcheck doit réussir et rester stable 60 secondes
avant `mark-good`. L’absence de Wi-Fi ou de centrale ne provoque pas à elle
seule un rollback ; un défaut matériel essentiel ou de stockage, oui.

`PrepareRecoveryImage` écrit une image vérifiée par staging et rename dans un
chemin local `0600`; elle sert de support de récupération hors ligne. La
caméra conserve trois tentatives de boot A/B et revient au dernier slot sain.
Son identité, son appairage, son spool clips et ses autres données persistantes
ne sont jamais dans les slots remplacés.

Procédure opérateur V1 :

1. vérifier `doctor` et `version` de la centrale ;
2. vérifier le manifeste et l’état `pending/good/rolled_back` de la caméra ;
3. lancer l’OTA lorsque la caméra est joignable ;
4. confirmer le healthcheck et `mark-good` ;
5. en cas de boucle de rollback, utiliser l’image de récupération et conserver
   le journal pour qualification.
