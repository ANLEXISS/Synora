# V1 — OTA caméra et récupération

`internal/cameraota` formalise l’agent OTA des Zero 3W : manifeste Ed25519,
checksum, modèle et version minimale du bootloader. Le transport réel est
injecté derrière une interface afin que la qualification locale simule les
pertes radio et les redémarrages sans prétendre disposer du matériel.

Une caméra hors ligne n’est pas considérée comme mise à jour : l’opération est
conservée en phase `pending` dans un fichier `0600` et sera rejouée lorsque le
transport la signalera en ligne. Les phases `installing`, `rebooting` et
`validating` sont récupérées par `Recover`; une interruption à ces étapes
demande `mark-bad`. Le healthcheck doit réussir avant `mark-good`.

`PrepareRecoveryImage` écrit une image vérifiée par staging et rename dans un
chemin local `0600`; elle sert de support de récupération, sans téléchargement
cloud obligatoire.

Procédure opérateur V1 :

1. vérifier `doctor` et `version` de la centrale ;
2. vérifier le manifeste et l’état `pending/good/rolled_back` de la caméra ;
3. lancer l’OTA lorsque la caméra est joignable ;
4. confirmer le healthcheck et `mark-good` ;
5. en cas de boucle de rollback, utiliser l’image de récupération et conserver
   le journal pour qualification.
