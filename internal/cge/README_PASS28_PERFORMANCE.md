# CGE — passe 28 : profilage et coûts

Ce document décrit la campagne reproductible de mesure précédant toute
orchestration Shadow. Les profils binaires sont générés dans `/tmp` et ne sont
pas versionnés.

## Complexité initiale observée

| opération | éléments parcourus | copies profondes | lectures journal | sérialisations | hashes | verrous | complexité estimée |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `PlanAssociation` | toutes les chaînes candidates | snapshots d’entrée | 0 | classement/payloads de politique | empreintes de plan selon politique | aucun | `O(n log n)` mesuré |
| `EvaluateObservation` | observations de la chaîne | faits/évidence intermédiaires | 0 | évaluations | empreintes d’évidence | aucun | `O(m log m)` après contrôle des doublons par map |
| `EvaluateBatch` | chaînes × observations bornées | résultats d’évidence | 0 | résultats par chaîne | empreintes par évaluation | aucun | environ `O(n*m)` |
| `Registry.AddObservation` | historique de la chaîne cible | chaîne, observations, historique | 0 | clone/validation de domaine | aucun global | mutex registre | `O(m log m)` (insertion ordonnée) |
| `Registry.AddContribution` | historique/contributions de la cible | chaîne et contributions | 0 | clone/validation de domaine | aucun global | mutex registre | `O(m)` |
| mutations `Coordinator` avant passe 28 | toutes les chaînes + hypothèses | restauration profonde de tous les registres | 1 `ReadAll` par append | JSON de chaque record | hash de chaque record relu | verrou coordinateur pendant clone/append/sync | `O(C+H)` profond par mutation, plus journal croissant |
| `FileJournal` append avant passe 28 | tous les records précédents | snapshots `ReadAll` | `O(R)` par append | parse de tout le journal + record courant | SHA de tout le préfixe | mutex journal, `Sync` | `O(R)` par append |
| `FileJournal.ReadHead` | dernier record | record local | 0 | JSON du dernier record | hash du dernier record | mutex journal | `O(taille dernier record)` |
| `ReadAll` / replay | tous les records | payloads défensifs + domaines reconstruits | 1 lecture complète | parse de `R` records | SHA de `R` records | verrou journal pendant lecture | `O(R + octets)` |
| `StateDigest` | toutes les chaînes/hypothèses | listes et snapshots défensifs | 0 | 2 JSON canoniques | 2 SHA-256 | verrous lecteurs | `O((C+H)log(C+H))` + octets sérialisés |
| `ValidateCoordinatorState` | toutes les chaînes/hypothèses + histoires | restores de validation | 0 | indirectes via restore | aucun digest global | verrous lecteurs | linéaire dans l’état et les histoires |

## Mesures

Les tailles pures et transactionnelles sont `10, 50, 100, 500, 1000`, avec
`5000` pour les fonctions pures. Les résultats exacts sont produits avec :

```bash
GOCACHE=/tmp/synora-gocache go test ./internal/cge/validation -run '^$' \
  -bench . -benchtime=1x -benchmem
GOCACHE=/tmp/synora-gocache go test ./internal/cge/validation -run '^$' \
  -bench 'BenchmarkPlanAssociation/chains-5000' -benchtime=100ms \
  -cpuprofile=/tmp/cge-pass28-cpu.pprof
GOCACHE=/tmp/synora-gocache go test ./internal/cge/validation -run '^$' \
  -bench 'BenchmarkCoordinatorValidationAndDigests/chains-500/digest' \
  -benchtime=100ms -benchmem -memprofile=/tmp/cge-pass28-mem.pprof
GOCACHE=/tmp/synora-gocache go run ./tools/dev/synora-cge-validation benchmark --json \
  --output /tmp/cge-pass28-report.json
```

Sur cette machine, les points représentatifs sont :

* `Coordinator.AddObservation`, 10→1000 chaînes : environ `0.34→0.48 ms`,
  et `28→79 KiB` alloués ; le temps reste presque indépendant du nombre de
  chaînes, tandis que la table de possession reste `O(C)`.
* `FileJournal` append, 10→1000 records : environ `0.26→0.44 ms` ; aucune
  relecture complète n’est déclenchée par l’append normal.
* `ReadAll`, 10→1000 records : environ `1.2→106 ms`, allocations
  `233 KiB→18.3 MiB` ; la relecture complète reste volontairement disponible.
* `StateDigest`, 500 chaînes : `5.6 ms`, `1.8 MiB` ; 1000 chaînes : `12.2 ms`,
  `3.6 MiB`.
* avant l’optimisation, `EvaluateObservation` était le principal hotspot pur
  (500 observations ≈`26.8 ms`, 1000 ≈`112 ms`) à cause du contrôle de
  doublons quadratique de `Chain.Validate`. Après passage à une map d’IDs,
  puis réutilisation de cette map pour les références historiques, il mesure
  environ `1.64 ms`/`2.95 ms`/`10.17 ms` pour 500/1000/5000 observations.
  `Registry.AddObservation` mesure environ `2.06 ms` à 500 et `4.26 ms` à
  1000. Aucune politique n’a été modifiée dans cette passe.

Le profil CPU est conservé temporairement dans `/tmp`; l’outil `pprof` n’est
pas disponible dans cette image Go, donc le rapport utilise les ratios
`-benchmem`, les profils produits et les courbes de croissance plutôt qu’un
top symbolique non reproductible. Le profil mémoire confirme les allocations
de JSON/snapshots dans les chemins `ReadAll`, digest et validation.

## Optimisations appliquées

* Le runner fait une validation locale des objets touchés et de la tête WAL
  après chaque étape. La validation globale (`ValidateCoordinatorState` et
  `ReadAll`) est conservée en fin de scénario, avant/après génération, après
  replay/récupération et dans les matrices significatives.
* Le journal mémorise sa tête après une validation complète et utilise ensuite
  une vérification bornée du dernier record, de sa séquence, de son hash et de
  la taille du fichier. Une modification historique arbitraire avec une queue
  inchangée n’est pas prétendument détectée par ce chemin ; `ReadAll` la
  détecte aux frontières complètes.
* Les candidats transactionnels font une copie de la table de possession et
  ne restaurent profondément que la chaîne ou l’hypothèse mutée. Les objets
  inchangés restent partagés entre candidats immuables ; toute mutation clone
  sa cible avant remplacement. Le WAL précède toujours la publication.
* Les validations de chaîne conservent toutes les vérifications, mais
  réutilisent l’index local des observations au lieu de le reconstruire pour
  chaque révision historique.
* La suite Go standard utilise un volume représentatif de 50 éléments. Le
  workload exhaustif `VolumeScenario(500)` reste exécuté par `qualify --full`.

## Optimisations refusées

* suppression de `fsync` : refusée, garantie de durabilité inchangée ;
* suppression des copies défensives de snapshots, observations, contributions,
  historiques ou payloads : refusée, elles protègent les frontières publiques ;
* cache d’objets mutables ou digest global permanent : refusé, risque de
  résultat périmé et invalidation complexe ;
* lecture partielle de `ReadAll` au démarrage, replay, récupération ou
  qualification exhaustive : refusée, la détection complète reste obligatoire ;
* modification des scores, seuils, règles, résolutions ou branchement Shadow :
  refusée par contrainte de passe.

## Budgets retenus

* `PlanAssociation` reste borné par `MaxCandidates`/`MaxRankedCandidates` au
  niveau du résultat ; le scoring parcourt néanmoins les candidats disponibles.
* Une mutation durable ne restaure plus profondément tous les agrégats ; son
  coût profond est borné aux objets touchés, avec le coût explicite de copie de
  la table de possession.
* Une observation et une contribution courantes ne déclenchent ni replay
  complet ni `ReadAll` complet.
* Les validations globales et digests ne sont pas exécutés pour chaque
  événement runtime ; elles restent des contrôles de frontière.
* Les ratios de croissance, et non des millisecondes absolues, sont les
  budgets portables : append tête quasi constant avec `R`, `ReadAll` linéaire
  avec `R`, digest linéaire/log-linéaire avec `C+H`, et mutations profondes
  dépendantes de l’historique de la cible.

## Flux Shadow synthétique

`BenchmarkShadowFlow` simule `PlanAssociation → ApplyAssociationPlan →
EvaluateBatch` sans résolution et sans branchement au runtime. Il couvre
`50, 200, 500, 1000` chaînes et `1, 5, 10` événements simulés par seconde,
sans temporisation réelle. À titre indicatif, le probe `1x` mesure environ
`2.0/8.1/19.2 ms` pour 50/500/1000 chaînes à un événement, et
`12.9/89.1/182.9 ms` pour 10 événements ; la mémoire reste proportionnelle
au nombre de chaînes et d’événements du probe. Ce benchmark ne sélectionne ni
n’applique aucune résolution automatique.

## Qualification

Le mode standard est la commande `qualify` et couvre le catalogue A–H, le
volume réduit, WAL, concurrence, collisions, idempotence, checkpoints et
replay. Le mode exhaustif `qualify --full` utilise 500 éléments, les matrices
complètes et les replays supplémentaires. Aucun mode n’est appelé au
démarrage du runtime et aucun profil binaire n’est versionné.
