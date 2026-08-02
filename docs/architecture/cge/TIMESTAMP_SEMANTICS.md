# Sémantique des temps CGE

Les définitions exécutables sont dans `timestamps.yaml`. Les temps ne sont pas
interchangeables :

| Temps | Signification |
|---|---|
| `observed_at` | instant observé ou déclaré par la source |
| `produced_at` | création du message par le producteur |
| `received_at` | réception par le composant destinataire |
| `ingested_at` | acceptation par l'ingest historique |
| `processed_at` | fin de transformation par le CGE |
| `committed_at` | acceptation logique par un workflow ou ledger |
| `persisted_at` | confirmation d'écriture durable selon la politique du store |

Les temps de création, mise à jour, changement d'état, fermeture, dernière
observation et recovery ont également des IDs séparés. Une structure qui ne
porte pas un temps déclare explicitement son absence ou une migration requise ;
un autre temps ne peut pas être substitué silencieusement.
