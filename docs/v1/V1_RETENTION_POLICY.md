# Politique de rétention V1

## Inventaire et limites

| Donnée | Propriétaire | Politique V1 | Protection |
| --- | --- | --- | --- |
| Clips `.mp4` | Core + clip spool | 24 h, 500 fichiers, 5 GiB | preuves d’incidents actifs ; fichiers orphelins réconciliés avant purge |
| Incidents | StateStore | 90 jours, 200 entrées | incidents `new`/`viewed` ; références détachées atomiquement à l’expiration |
| Événements récents | Core/EventStore + StateStore | 7 jours, 200 entrées | événements référencés par un incident conservé |
| Logs opérateur | journal système / déploiement | 14 jours, 512 MiB | l’application ne supprime pas un journal externe qu’elle ne possède pas |
| Outbox terminale | Outbox/Dispatcher | 7 jours, 10 000 entrées, 256 MiB | `pending`, `retry_wait` et `in_flight` jamais supprimés |
| Temporaires/uploads | Discovery et datasets | 1 heure, quotas locaux | les fichiers `.part` et staging sont supprimés seulement après expiration |

La centrale conserve au minimum 512 MiB libres. Une nouvelle ingestion est
refusée avant écriture si elle ferait franchir cette réserve. Les quotas de
clips et la réserve sont indépendants : libérer des entrées terminales ne
réduit jamais la garantie at-least-once des livraisons actives.

## Ordre et intégrité

Les expirations utilisent l’horloge UTC fournie par le composant et un ordre
stable `(date, identifiant)`. Core expire d’abord les incidents terminaux,
puis les événements non référencés, tandis que les clips actifs restent
protégés par leurs incidents. La suppression d’un incident détache ses IDs des
clips dans la même mutation avant persistance. Un clip expiré conserve son
métadonnée et son identifiant, mais son fichier physique est supprimé.

Les états temporaires avec TTL (`tracks`, présence, fenêtres) restent gérés
par `StateStore.Cleanup`; la politique commune ne remplace pas leurs invariants
de decay ou de tracking.

## Limites explicites

Les logs sont délégués au journal système du déploiement, car Synora ne
possède pas de fichier de journal global dans cette V1. Les extraits de
support doivent passer par le redactor de sécurité J15. Aucun cloud ni coffre
Synora+ n’est requis pour la rétention locale.
