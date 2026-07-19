# Pass 31 — routines contextuelles en mémoire

Cette passe ajoute `internal/cge/routines`, un domaine descriptif et
volontairement non durable. Le package ne dépend ni du journal, ni du replay,
ni du coordinateur durable, ni du ShadowEngine, ni du Core.

## Modèle

Une routine est identifiée par `subject + kind + pattern structurel` :

- `context_presence` décrit un nœud, une zone, le type de nœud, l’entrée/
  extérieur, l’occupation et le mode du domicile ;
- `context_transition` décrit les contextes source/cible, l’entrée/sortie,
  l’extérieur, l’adjacence, la distance connue et les états avant/après.

Le timestamp exact, le weekday, le bucket, la timezone et la révision de
topologie ne font pas partie de l’identité de la routine. Ils restent dans
les occurrences et leurs statistiques. Une entité connue forme un sujet
`entity`; sans `ObservationRef.EntityID`, le sujet est la chaîne source. Les
chaînes inconnues ne sont jamais regroupées dans un sujet global.

Les IDs sont déterministes :

```text
cge-routine-<sha256>
cge-routine-occurrence-<sha256>
```

Une occurrence de présence référence une observation ; une transition
référence `previous → current` dans cet ordre causal.

## Extraction explicite

`ExtractPresenceOccurrence`, `ExtractTransitionOccurrence` et `PlanLearning`
ne mutent rien. La politique par défaut est :

```text
namespace = synora.cge.routines
version = routine-extraction-v1
bucket = 15 minutes
partial context = accepté
transition gap = 15 minutes maximum
same topology revision = requise
```

Un contexte absent, partiel interdit, une topologie absente, un changement de
révision ou un gap excessif produit un skip typé. Un plan peut donc contenir
zéro, une ou deux occurrences sans transformer une absence de contexte en
fausse routine.

## Agrégation et registre

`Routine` conserve toutes les références d’occurrence, son historique local,
les bins weekday/bucket, les day-parts, les jours et semaines locales, ainsi
que min/max/total/moyenne des intervalles. Les occurrences tardives sont
réinsérées dans l’ordre `ObservedAt → OccurrenceID` et les statistiques sont
reconstruites déterministement.

Le `Registry` est propriétaire, protégé par mutex, et maintient des index
dérivés par sujet, type et sujet actif. `ApplyOccurrence` crée ou enrichit
explicitement une routine ; un doublon est idempotent et une même ID avec un
contenu différent est une collision. `ApplyLearningPlan` est transactionnel
par occurrence mais non atomique globalement et retourne le résultat de chaque
occurrence.

La création historique exige `MutationContext.At == Occurrence.ObservedAt`.
Les ajouts suivants exigent une mutation monotone et acceptent les
observations tardives. Aucune opération ne modifie automatiquement le statut ;
`invalidated` est terminal.

## Limites

Cette passe ne persiste aucune routine et ne modifie aucun journal, snapshot
générationnel, coordinateur ou ShadowEngine. Elle ne calcule aucun score de
normalité/anomalie, ne produit aucune contribution ou hypothèse, ne résout
rien, ne change aucun lifecycle et ne déclenche aucune action ou décision de
sécurité. La liste d’occurrences est volontairement sans pruning ni
compaction ; le coût mémoire est linéaire.

Les futures passes pourront ajouter la durabilité, les réattributions,
fusions/séparations, le lifecycle explicite et l’évaluation de déviation.
