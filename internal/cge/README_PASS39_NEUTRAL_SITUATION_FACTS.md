# Passe 39 — Neutral Situation Facts

## Objectif et frontière

`internal/cge/situationfacts` transforme un `episodes.EpisodeSnapshot` en
`FactSet` descriptif, versionné, déterministe et traçable. Les faits répondent
à « qu’est-ce qui est observé ou directement dérivable ? »; ils ne répondent
jamais à « que signifie la situation ? ».

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
  ↓
[future] Evidence Discrimination
```

La couche est in-memory, dérivée et expérimentale. Elle ne produit ni
hypothèse, ni intention, ni menace, ni décision de sécurité, ni action.

## Modèle neutre

Un `Fact` sépare `FactID`, `FactKey`, code stable, scope, sujet, prédicat,
valeur typée, origine, statut, intervalle, qualité et provenance. Les scopes
sont `episode`, `entity`, `observation`, `transition`, `context` et `memory`.
Les sujets sont des références minimales; une identité incertaine reste
incertaine et ses candidats ne sont jamais sélectionnés.

Les valeurs sont une union contrôlée : booléen, entier, permille, chaîne,
timestamp UTC, durée en millisecondes, ensemble de chaînes, séquence de
chaînes ou référence. Les ensembles sont triés/dédupliqués; les séquences
conservent l’ordre; les scores permille sont dans `[0,1000]`.

Les origines sont :

- `observed` : directement présent dans une observation expurgée;
- `derived` : calculé déterministement à partir de l’épisode;
- `carried` : référence CGE déjà calculée, notamment une déviation.

Les statuts sont `asserted`, `unknown`, `conflicting` et `retracted`.
Les contradictions sont conservées dans `ConflictSet`; elles ne sont jamais
résolues automatiquement.

La qualité décrit seulement complétude, fiabilité explicitement fournie,
nombre de sources et caractère partiel. Elle n’est pas une confiance de
situation; en l’absence de mesure de fiabilité fournie, la valeur reste à
zéro et n’est pas inventée par l’extracteur.

## Schéma et fingerprints

Le schéma statique est versionné `situation-facts-schema-v1:` et définit les
codes, scopes, types, multiplicités et règles de conflit. Les codes couvrent
les familles épisode, identité, spatial, temporel, contexte, continuité et
mémoire cognitive.

Les fingerprints sont déterministes :

- `fact-key-…` représente le hash de l’emplacement sémantique;
- `fact-…` représente le contenu canonique et sa provenance;
- `situation-facts-v1:…` représente un FactSet;
- `situation-facts-registry-v1:…` représente le registre;
- `situation-facts-policy-v1:…` représente la policy.

Les timestamps sont UTC canoniques. Les maps ne participent jamais à un ordre
non déterministe.

## Extraction

`Extract` reçoit explicitement l’épisode, une topologie optionnelle et
`ExtractedAt`. Il ne lit ni horloge, fichier, réseau ou registre. Il ne
réassocie aucune observation, ne recalcule aucune routine ou déviation, et ne
déclenche aucun effet.

Les faits produits décrivent notamment : statut et cardinalités de l’épisode,
présence d’identités, candidats, séquences de nœuds/zones, transitions
atteignables/inaccessibles/inconnues, durée et écarts temporels, changements de
contexte, tracks/activations/séquences partagés et références de chaînes,
routines et assessments de déviation.

Une topologie absente produit `spatial.topology_available = false` et des
transitions inconnues, jamais une transition inaccessible. Un contexte partiel
produit des faits de partialité ou d’absence, jamais un mismatch artificiel.
Une déviation est transportée et agrégée descriptivement; aucun facteur n’est
recalculé.

## Contradictions et inconnues

Deux résidents connus présents simultanément ne sont pas automatiquement en
conflit. Un changement temporel `away → home` est un changement descriptif,
pas un conflit. Un conflit est conservé lorsqu’une même clé logique reçoit des
valeurs incompatibles au même instant et dans le même contexte technique,
comme deux modes pour un même track.

Table de frontière :

| Fait neutre | Interprétation interdite |
| --- | --- |
| `identity.unknown_present` | intrus |
| `spatial.unreachable_transition` | comportement malveillant |
| déviation temporelle positive | activité dangereuse |
| `context.house_mode_conflict` | sabotage |
| `identity.multiple_known_entities` | groupe suspect |

Les codes, descriptions et constantes du schéma sont testés contre un
vocabulaire interprétatif interdit. Les mots peuvent apparaître dans cette
documentation uniquement pour expliquer leur absence du domaine.

## FactSet, diff et registre

Un `FactSet` est attaché à un EpisodeID et une EpisodeRevision obligatoires.
Ses faits et conflits sont triés, clonés aux frontières et fingerprintés.
`Diff` produit les ajouts, retraits, changements de valeur et évolutions de
conflits; une modification d’une valeur n’est pas représentée comme deux
changements incohérents.

Le registre est le propriétaire unique de sets en mémoire. `Apply` vérifie la
policy, le schéma, les IDs, les fingerprints et la révision d’épisode. Un même
FactSet est idempotent; une révision ancienne est refusée; deux sets différents
issus de la même révision produisent un conflit optimiste. L’application est
atomique et les snapshots sont défensifs.

## Limites et prochaines étapes

Les limites par défaut sont 256 faits par épisode, 64 provenances par fait,
256 runes par chaîne, 64 valeurs d’ensemble et 256 valeurs de séquence. Il
n’existe ni WAL, ni persistence, ni replay, ni checkpoint, ni historique
illimité de faits rétractés.

Le package n’est branché à aucun `ShadowEngine`, `ShadowOrchestrator`,
`synora-core`, endpoint API, action, automation, Live Lab ou Field Trial
Recorder. Il n’importe pas `episodes` inversement et ne modifie aucune policy
existante.

La prochaine passe pourra consommer les FactSets et leurs diffs pour préparer
des hypothèses de situation concurrentes. Cette passe ne crée aucun concept
de situation ou d’hypothèse.
