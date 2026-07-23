# Passe 64.1 — redaction des identifiants durables CGE

La passe physique 64 a révélé qu'un `vision.identity` pouvait atteindre les
fichiers CGE durables avec ses identifiants bruts. Le défaut était dans
`event_adapter.go` : `observationFromEvent` copiait directement l'événement
Core dans `chains.ObservationRef`. Le coordinateur historique, le workflow et
le ledger réutilisaient ensuite cette référence.

## Frontière et format

La frontière unique est `internal/cge/durableids`. Elle est appelée pendant
l'adaptation `cge.Event` vers les modèles CGE persistables, avant le
coordinateur, les épisodes ou le workflow. Le namespace versionné est
`synora.cge.durable-id.v1` et le format est :

```text
cgeid-v1:<domain>:<sha256-hex>
```

La protection est une pseudonymisation déterministe SHA-256 avec séparation
de domaine (`observation`, `entity`, `device`, `clip`, `track`, `activation`,
`sequence`). Elle n'est pas un chiffrement : aucune clé ou secret n'est
introduit et le résultat n'est pas réversible par ce package. La sortie est
bornée, ASCII, sans saut de ligne et ne contient pas la valeur brute.

Les identifiants d'observation, d'identité, d'appareil, de clip, de track,
d'activation et de séquence sont protégés. Les références historiques copiées
dans le workflow sont également protégées. Les IDs internes déjà dérivés par
empreinte restent inchangés lorsqu'ils ne contiennent aucune valeur brute.
`NodeID`, `ZoneID`, les types d'événements, les timestamps, les scores,
`HouseMode`, `Occupancy`, la topologie et les états `known`/`unknown`/
`uncertain` restent sémantiques.

Une valeur vide reste vide. `Protect` reconnaît le format v1 et ne protège pas
une seconde fois une valeur déjà protégée. Une même valeur brute est stable
après redémarrage et produit des valeurs distinctes selon le domaine.

## Compatibilité et vérification

Le replay accepte les anciens records valides, y compris ceux qui contiennent
encore des identifiants bruts. Aucun journal existant n'est réécrit, migré ou
supprimé automatiquement : les anciens fichiers restent potentiellement
sensibles. Tous les nouveaux records passent par la frontière avant d'atteindre
le journal historique, les snapshots/générations, le workflow WAL, le
checkpoint ou le Calibration Ledger.

Le test end-to-end hermétique utilise `t.TempDir()`, `ShadowEngine`,
`FileJournal`, le coordinateur durable, le `ShadowWorkflow FileStore` et le
Calibration Ledger. Il parcourt récursivement les fichiers réguliers sous le
DataDir, le répertoire workflow et le chemin du ledger, y compris
`journal.ndjson`, les générations, `workflow.wal` et
`workflow.checkpoint.json`. Toute sentinelle signale le fichier, le type de
record et le champ JSON.

Cette passe reste sans autorité cognitive, sans action, sans commande et sans
automation. Le moteur historique, le StateStore et les règles d'admission ne
sont pas modifiés.
