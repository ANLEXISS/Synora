# Passe 40 — Situation Facts Incrementalization and Performance Hardening

## Résumé

Cette passe optimise `internal/cge/situationfacts` sans changer son contrat
sémantique. Les faits, conflits, provenances, identifiants et fingerprints
restent identiques à ceux de la passe 39. La couche reste in-memory, dérivée,
expérimentale, sans WAL, sans persistence, sans runtime integration et sans
hypothèse de situation.

Chaîne conservée :

```text
Event
  ↓
Chain association
  ↓
Routine / deviation
  ↓
Episode Working Memory
  ↓
Neutral Situation Facts
  ↓
[future] Competing Situation Hypotheses
```

## Baseline et profilage

La baseline reproductible est conservée dans `/tmp/cge-pass40-before.txt` et
la mesure finale dans `/tmp/cge-pass40-after.txt`. Les profils CPU et mémoire
ont été produits pour `Extract100`, `DiffMedium` et `Snapshot100`, avant et
après, dans `/tmp/cge-pass40-*.pprof`.

L'environnement ne fournit pas l'outil `go tool pprof` : `go tool pprof` a
retourné `go: no such tool "pprof"`. Les profils sont donc disponibles mais
leur tableau de fonctions n'a pas pu être généré avec l'outillage installé.
Les benchmarks `-benchmem` et les tests restent indépendants de cette
limitation.

Médianes de la machine de validation (AMD Ryzen 9 5900HS) :

| Benchmark | Avant | Après | Évolution |
|---|---:|---:|---:|
| Extract100 | 22.68 ms / 14.10 MB / 144440 allocs | 8.40 ms / 4.54 MB / 10810 allocs | 2.70×, -67.8 %, -92.5 % |
| DiffMedium | 7.62 ms / 7.39 MB / 10463 allocs | 7.80 ms / 4.43 MB / 9793 allocs | temps non atteint, -40.1 %, -6.4 % |
| Snapshot100 | 24.51 ms / 19.13 MB / 38367 allocs | 1.72 ms / 3.63 MB / 8247 allocs | 14.2×, -81.0 %, -78.5 % |
| Apply | 0.644 ms / 344 KB / 1266 allocs | 0.574 ms / 255 KB / 1044 allocs | 1.12×, -25.9 %, -17.5 % |
| Provenance canonicalization | 3.27 ms / 1.47 MB / 33022 allocs | 8.70 µs / 13.7 KB / 4 allocs | 376×, -99.1 %, -99.99 % |
| Fact canonicalization | 495 ns / 152 B / 6 allocs | 229 ns / 80 B / 2 allocs | 2.16×, -47.4 %, -66.7 % |

Les objectifs Extract100 et Snapshot100 sont atteints. Les objectifs stricts
de temps DiffMedium, de temps/allocations Apply et de 5× pour l'incrémental ne
le sont pas. Aucun résultat n'a été falsifié et les validations n'ont pas été
supprimées.

`Incremental99To100` mesure 12.86 ms / 7.10 MB / 15675 allocs, contre
`Extract100` à 8.40 ms / 4.54 MB / 10810 allocs dans la même série. Le chemin
est toutefois sûr et équivalent : il reconstruit les faits dérivés nécessaires
et produit le diff exact. Il ne revendique pas de réutilisation de faits quand
la provenance et les FactID doivent changer.

## Optimisations

### Builder et allocations

L'extraction utilise maintenant un `extractionBuilder` interne qui conserve les
brouillons par clé sémantique, valide et canonicalise une seule fois, fusionne
les provenances déjà canoniques, puis effectue le tri final. Les valeurs
canonicales et les `FactKey` sont conservés dans le brouillon afin d'éviter les
recalculs. Les capacités sont estimées à partir de l'épisode et bornées par la
policy ; `MaxFactsPerEpisode` n'est jamais alloué intégralement par défaut.

Le schéma compilé est mis en cache dans un `sync.Once` immuable avec un index
par `FactCode`. `Schema()` conserve une copie défensive publique.

### Canonicalisation et provenances

Le format historique des valeurs et des provenances n'a pas changé. Les
conversions entières utilisent `strconv`, les listes sont écrites sans
`fmt.Sprintf`, et la canonicalisation de provenance détecte les slices déjà
canoniques. La comparaison de provenances évite les chaînes concaténées. La
fusion de provenances triées utilise une fusion linéaire et dédupliquée.

### Fingerprints et hashing

Les fingerprints continuent d'utiliser exactement les préfixes et payloads
canoniques de la passe 39. Les slices déjà triées ne sont plus clonées avant
`json.Marshal`; le tri défensif n'est fait que pour des entrées non canoniques.
Il n'y a pas de version de fingerprint v2.

### Diff indexé

`Diff` conserve la validation publique des fingerprints et dispose d'un chemin
linéaire pour les FactSets déjà triés, indexé par groupes de `FactKey` et par
`ConflictSet.ID`. Le chemin de compatibilité map-based reste disponible pour
les entrées non canoniques. Les sorties publiques sont clonées aux frontières,
et l'ordre reste déterministe.

### Extraction incrémentale

`ExtractIncremental` expose les modes `incremental`, `full_fallback` et
`idempotent`. Le chemin incrémental est limité aux append-only stricts : même
EpisodeID, révision croissante, préfixe d'observations inchangé, policy et
schéma identiques, FactSet précédent valide et topologie absente/non impliquée.

Les familles maintenues dans le chemin append-only sont les compteurs,
ensembles, séquences spatiales, bornes et moyennes temporelles, continuités,
contexte et mémoire portée. Les provenances sont reconstruites exactement pour
ne pas produire un FactID incorrect.

Le fallback complet est obligatoire en cas de suppression, modification ou
insertion historique, changement de révision incohérent, FactSet forgé,
fingerprint incompatible, policy ou schéma différent, topologie fournie dont
l'identité n'est pas prouvable, ou autre condition non démontrée sûre. Il
appelle `Extract` et n'émet jamais un résultat partiel silencieux.

### Registre, snapshots et digest

Le registre invalide un cache de digest uniquement après mutation. Les lectures
répétées d'un état stable réutilisent le digest de la révision. Le snapshot
public garde ses clones défensifs complets. Une `planningSnapshot` interne,
non exportée, partage les FactSets immuables et est destinée aux futurs
planificateurs. Elle ne modifie pas l'API publique.

## Équivalence et corpus golden

`TestGoldenCorpusDeterministic` couvre : épisode simple, 10 et 100
observations, identités connue/inconnue/incertaine, deux entités, changement et
conflit de mode, topologie absente/inaccessible, contexte partiel, hors ordre,
déviation nulle/positive, plusieurs assessments et continuité forte.

`TestIncrementalEquivalenceCorpus` compare extraction complète et extraction
incrémentale sur append simple, 10→100, 99→100, changements de contexte,
identités, conflit et assessments. Les FactSet, facts, conflits, provenances,
FactKey, FactID, reports et fingerprints doivent être égaux.

Les tests de robustesse couvrent aussi un FactSet précédent forgé, une
observation historique modifiée, une topologie fournie et les modes idempotent
et fallback.

## Concurrence et limites

Le registre reste propriétaire, verrouillé et défensif. Les snapshots publics,
les snapshots internes, les extractions complètes et incrémentales, les
applications concurrentes et le digest concurrent ont été exécutés sous
`-race`. Une seule application concurrente d'une même révision peut réussir ;
l'autre reçoit le conflit optimiste attendu.

Les limites de facts, de provenance et de chaînes restent celles de la policy
de la passe 39. Les entrées non démontrées sûres prennent le fallback complet.
La réutilisation comptée est donc honnêtement `0` pour l'append courant : les
faits existants peuvent changer de provenance et de contenu, et ne sont pas
réutilisés artificiellement.

## Readiness

Les validations réalisées permettent de marquer comme vraies l'équivalence
sémantique, la stabilité du corpus golden, l'optimisation de l'extraction
complète, le fallback incrémental, le diff, le registre, les snapshots, le
cache de digest, la concurrence et l'immutabilité des fingerprints et IDs.

Les valeurs de sécurité restent :

```text
RuntimeIntegrated              = false
Durable                        = false
SituationHypothesesImplemented = false
SecurityAuthority              = false
```

La readiness est vraie pour la prochaine passe de situation hypotheses au sens
de la structure de readiness, avec les limitations de performance
incrémentale et de diff indiquées ci-dessus.

## Limites et frontière de passe

Cette passe n'ajoute ni `SituationHypothesis`, ni score de menace, ni intention,
ni demande d'observation, ni action, ni automation. Elle ne modifie aucune
politique de chaîne, routine, déviation, épisode, readiness terrain,
fingerprint de déploiement, Field Trial Recorder ou runtime Shadow.

Situation Facts remain in-memory, derived and experimental. Aucun WAL,
replay, checkpoint, persistence ou journal global n'a été ajouté. La prochaine
étape est l'élaboration de competing situation hypotheses à partir des
FactSets et de leurs diffs.
