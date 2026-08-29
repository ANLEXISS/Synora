# V1 — sauvegarde et restauration locale

`internal/backup` et `synora-backup` fournissent le périmètre V1 suivant :

- un snapshot contient une copie cohérente de `PersistedState`, couvrant donc
  incidents, clips, présence et métadonnées de photos, plus les comptes,
  configurations persistantes, topologie et sources/datasets faciaux
  explicitement collectés par `synora-backup` ;
- `SYNORA_BACKUP_SECRET` est obligatoire pour créer ou restaurer via la CLI ;
  l’état et les fichiers sont chiffrés par AES-256-GCM, et le manifeste porte
  les checksums SHA-256 des données chiffrées sans jamais contenir le secret ;
- la sauvegarde écrit dans un staging privé, synchronise les fichiers, écrit le
  manifeste en dernier, puis effectue un rename de répertoire ; une interruption
  avant ce rename ne crée pas de snapshot restaurable ;
- la restauration vérifie le manifeste, les versions, tous les checksums et le
  secret avant toute mutation ; les fichiers de configuration sont restaurés
  par écritures atomiques avec refus des symlinks/traversals et rollback si une
  interruption survient ;
- la réserve minimale par défaut est de 512 MiB. Une expiration renomme d’abord
  le snapshot en `.delete`, afin qu’une interruption soit reprise par
  `RecoverExpiredDeletes` ; les snapshots invalides ne sont pas supprimés
  silencieusement ;
- les données restent locales. Un futur coffre Synora+ peut consommer un
  manifeste vérifié via un adaptateur explicite, mais aucun cloud n’est requis
  ni appelé par V1.

Exemples :

```text
SYNORA_BACKUP_ROOT=/var/lib/synora/backups SYNORA_BACKUP_SECRET='local-secret' synora-backup create
SYNORA_BACKUP_ROOT=/var/lib/synora/backups SYNORA_BACKUP_SECRET='local-secret' synora-backup restore SNAPSHOT_ID
SYNORA_BACKUP_ROOT=/var/lib/synora/backups synora-backup expire --age 30d
```
