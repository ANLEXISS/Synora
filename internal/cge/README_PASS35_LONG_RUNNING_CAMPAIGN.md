# Passe 35 — campagnes Shadow longue durée

Le package `internal/cge/campaign` est un harnais de développement qui exécute
le vrai `ShadowEngine` sur une timeline simulée. Il ne constitue pas un second
CGE et n'est pas importé par le runtime de production.

Les profils A–H couvrent une maison stable à un ou deux résidents, un décalage
de routine, des variations bénignes, des épisodes synthétiques, des capteurs
dégradés, les redémarrages/checkpoints et une mémoire de 90 jours. Les labels
(`ordinary`, `benign_variation`, `synthetic_intrusion`, etc.) restent dans le
runner et les rapports : ils ne sont pas fournis au CGE comme observation,
contexte ou preuve.

La timeline est produite par une fonction déterministe à partir du profil et
de son seed. L'EventID dépend uniquement des données observables de l'événement
(date, résident synthétique, nœud et position déterministe), jamais du label.
Les résultats portent les statuts et bandes de déviation, les compteurs par
label, le warm-up, la croissance, les latences et les diagnostics de calibration.

Le runner utilise le chemin réel d'association, evidence, hypothèses, routines
et déviation. L'évaluation est faite avant `ApplyRoutineLearningPlan`. Une
réexécution contrôlée vérifie l'idempotence sans ajouter de record WAL. Le store
de déviation reste borné et éphémère ; il est vide après redémarrage, alors que
les chaînes, hypothèses et routines sont restaurées depuis le journal global.

Les checkpoints sont ceux du coordinateur existant. Les chaînes peuvent être
restaurées par génération puis journal, tandis que les routines et hypothèses
rejouent le journal complet. Cette asymétrie est volontaire et ne modifie pas
le format des générations.

## CLI

```text
go run ./tools/dev/synora-cge-validation campaign list
go run ./tools/dev/synora-cge-validation campaign run-all --json
go run ./tools/dev/synora-cge-validation campaign run-all --full --json --output /tmp/cge-pass35.json
go run ./tools/dev/synora-cge-validation campaign run stable_single_resident_30d --days 7 --events-output /tmp/events.ndjson
```

Sans `--full`, les campagnes utilisent sept jours simulés. `--full` conserve
les durées des profils (30, 45 ou 90 jours). Les fichiers sont écrits dans un
répertoire temporaire ou dans le chemin explicitement fourni, jamais dans les
répertoires runtime Synora.

Les taux de déviation bénigne, la séparation des épisodes synthétiques et la
vitesse d'adaptation sont des résultats expérimentaux. Un warning de calibration
n'est pas une alarme et ne modifie aucune politique. Il n'existe aucune notion
de menace, d'anomalie de sécurité ou d'autorité historique dans cette passe.
