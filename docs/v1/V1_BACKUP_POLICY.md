# V1 — sauvegarde et restauration locale

`internal/backup` et `synora-backup` fournissent le périmètre V1 suivant :

- un snapshot contient une copie cohérente de `PersistedState`, couvrant donc
  incidents, clips, présence et métadonnées de photos, plus un manifeste signé
  par checksum SHA-256 des fichiers explicitement ajoutés ;
- la sauvegarde écrit dans un staging privé, synchronise les fichiers, écrit le
  manifeste en dernier, puis effectue un rename de répertoire ; une interruption
  avant ce rename ne crée pas de snapshot restaurable ;
- la restauration vérifie le manifeste et tous les checksums avant de remplacer
  l’état durable ; les fichiers de configuration optionnels sont restaurés par
  écritures atomiques ;
- la réserve minimale par défaut est de 512 MiB. Une expiration renomme d’abord
  le snapshot en `.delete`, afin qu’une interruption soit reprise par
  `RecoverExpiredDeletes` ; les snapshots invalides ne sont pas supprimés
  silencieusement ;
- les données restent locales. Un futur coffre Synora+ peut consommer un
  manifeste vérifié via un adaptateur explicite, mais aucun cloud n’est requis
  ni appelé par V1.

Exemples :

```text
SYNORA_BACKUP_ROOT=/var/lib/synora/backups synora-backup create
SYNORA_BACKUP_ROOT=/var/lib/synora/backups synora-backup restore SNAPSHOT_ID
SYNORA_BACKUP_ROOT=/var/lib/synora/backups synora-backup expire --age 30d
```
