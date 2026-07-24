# Passe 41 — Competing Situation Hypotheses

## Frontière conceptuelle

Une hypothèse d'association répond à :

```text
À quelle chaîne cette observation appartient-elle ?
```

Une hypothèse de situation répond à :

```text
Quelle explication descriptive est compatible avec les faits de l'épisode ?
```

`internal/cge/situationhypotheses` ne consomme que `situationfacts.FactSet` et
`situationfacts.FactSetDiff`. Il n'importe ni `episodes`, ni `chains`, ni
`routines`, ni `deviation`, ni le package historique `hypotheses`.

Chaîne de cette passe :

```text
FactSet
  ↓
Hypothesis rules
  ↓
Competing hypotheses
  ├── support
  ├── contradiction
  ├── missing information
  └── plausibility / coverage
  ↓
[future] Evidence Discrimination
  ↓
[future] Active Observation Requests
```

## Modèle

`SituationHypothesis` contient un ID déterministe, l'épisode, le kind, le
statut, les révisions de faits, les contributions de support et contradiction,
les informations manquantes, quatre scores descriptifs et un fingerprint.

`PlausibilityPermille` est explicitement un indice de plausibilité descriptif.
Il ne représente ni une probabilité, ni une certitude, ni une signification de
sécurité.

`CompetingHypothesisSet` conserve plusieurs hypothèses, le fingerprint du
FactSet, la révision de registre de faits, le leader éventuel, sa marge,
`Ambiguous`, `InsufficientInformation`, sa révision et son fingerprint.

Un leader est seulement la première hypothèse selon l'ordre canonique. Il ne
résout pas l'ensemble et ne déclenche aucune suite automatique. Si la marge
minimale n'est pas atteinte, le leader reste vide.

## Hypothèses du schéma v1

Le schéma statique contient exactement :

```text
pattern_consistent
isolated_deviation
possible_pattern_shift
identity_resolution_failure
coherent_unrecognized_activity
context_or_sensor_inconsistency
multi_entity_activity
insufficient_information
```

Ces kinds peuvent coexister. En particulier, une activité techniquement
cohérente peut rester concurrente d'une insuffisance de résolution identitaire
ou de contexte.

Les labels, codes et descriptions du schéma restent descriptifs. Les termes
de signification sécuritaire sont volontairement exclus du schéma et testés
automatiquement.

## Règles et contributions

Les `EvidenceRule` sont typées et versionnées. Elles référencent un
`FactCode`, un `FactScope`, un opérateur, une valeur typée éventuelle, un poids
borné et un `ReasonCode`.

Les opérateurs sont :

```text
exists
not_exists
equals
not_equals
greater_than
greater_or_equal
less_than
contains
set_contains
status_is
conflict_exists
```

Il n'y a ni script, ni réflexion arbitraire, ni exécution de code dynamique.
Les types de valeurs sont vérifiés contre le schéma `situationfacts`.

Une `Contribution` contient son rôle, la règle, le reason code, les FactID
triés/dédupliqués, son poids et le fingerprint du FactSet source. Toute
contribution de support ou de contradiction possède au moins un FactID connu.

Une `MissingRequirement` décrit uniquement une dimension absente. Elle ne
constitue pas une demande d'observation active.

## Évaluation et scoring

`Evaluate` valide le FactSet, trie les faits par FactID, évalue les règles dans
un ordre canonique et produit un résultat sans horloge ni effet externe.

Les quatre valeurs sont séparées :

```text
SupportPermille
ContradictionPermille
CoveragePermille
PlausibilityPermille
```

Le support et la contradiction dédupliquent les FactID avant accumulation ; un
même fait ne renforce donc pas artificiellement un résultat lorsqu'il satisfait
plusieurs règles. Les dimensions inconnues ne sont pas des contradictions et
réduisent la couverture. La plausibilité est calculée à partir du support, de
la contradiction puis de la couverture ; elle n'est jamais exposée comme une
probabilité.

Policy expérimentale par défaut :

```text
MaxHypothesesPerEpisode          = 16
MaxContributionsPerHypothesis   = 128
MaxMissingRequirements           = 64
MinCandidateCoveragePermille    = 150
MinSupportedPlausibilityPermille = 600
MinLeadingMarginPermille         = 100
ContradictedThresholdPermille   = 700
MaxFactIDsPerContribution        = 64
```

Ces valeurs ne sont pas calibrées sur le terrain. Le fingerprint de policy est
préfixé par `situation-hypotheses-policy-v1:` et n'est pas ajouté au
fingerprint cognitif de déploiement.

## Statuts et lifecycle

Les statuts sont :

```text
candidate
supported
weakened
contradicted
insufficient_information
invalidated
```

`invalidated` est terminal. Une hypothèse ne devient pas résolue parce qu'elle
est leading. Une hypothèse n'est pas invalidée simplement parce qu'une autre
est mieux classée ; l'invalidation vient d'une incompatibilité ou de la
disparition explicite de ses supports lors de la réévaluation.

`EvaluateLifecycle` expose une décision pure et vérifie les transitions
autorisées, sans mutation.

## Planner et réévaluation par diff

`Plan` compare le résultat d'évaluation au snapshot courant et produit des
créations, mises à jour et invalidations. Il ne modifie aucun registre.

`ReevaluateFromDiff` vérifie EpisodeID, révisions et fingerprints du diff. Il
réévalue depuis le FactSet courant avec le même moteur de règles ; il ne relance
pas l'extraction de faits et n'appelle pas `situationfacts.ExtractIncremental`.
Le résultat complet et le résultat par diff sont canoniques et testés pour être
identiques.

En cas de doute sur les références, un fingerprint incohérent ou une révision
obsolète produit une erreur typée plutôt qu'un résultat partiel silencieux.

## Classement et explications

Le classement est :

```text
PlausibilityPermille décroissant
CoveragePermille décroissant
ContradictionPermille croissant
HypothesisKind
HypothesisID
```

Une égalité ou une marge inférieure à `MinLeadingMarginPermille` rend le set
ambigu et vide le leader.

`Explain` produit uniquement des codes, FactID, FactCode, valeurs typées et
poids. Les champs `NotAProbability` et `NoSecurityMeaning` rendent la
frontière explicite. Aucun texte libre non déterministe n'est généré.

## Identifiants et fingerprints

Les IDs utilisent SHA-256 :

```text
situation-hypothesis-...
situation-contribution-...
```

Les fingerprints utilisent :

```text
situation-hypotheses-schema-v1:
situation-hypotheses-policy-v1:
situation-hypothesis-v1:
competing-hypothesis-set-v1:
situation-hypothesis-registry-v1:
```

Les maps ne participent pas à l'ordre logique. Les Facts, contributions,
hypothèses et sets sont ordonnés avant fingerprint. Les clones défensifs
empêchent une modification publique de changer l'état interne.

## Registre et snapshots

Le registre est in-memory, thread-safe et propriétaire. Il indexe les épisodes
et les hypothèses dans les snapshots publics, applique les plans avec une
révision optimiste et remplace atomiquement le set d'un épisode.

L'application du même plan est idempotente, y compris lorsque le plan est
réappliqué après que la révision du registre a avancé. Deux plans différents
issus de la même révision produisent un conflit optimiste ; aucune mutation
partielle n'est conservée.

Les snapshots contiennent des copies défensives des sets, des index et du
digest. `RegistryDigest` est stable pour un état logique identique.

## Invariants

Les validations garantissent notamment :

```text
tous les supports et contradictions référencent des FactID existants
aucun FactID dupliqué dans une contribution
scores bornés dans [0,1000]
couverture bornée dans [0,1000]
aucun leader lorsque la marge est insuffisante
aucune dimension inconnue transformée en contradiction
aucun conflit résolu automatiquement
aucune hypothèse sécuritaire
fingerprints déterministes
révisions strictement croissantes après mutation
digest inchangé après application idempotente
```

## Qualification

Les tests couvrent : pattern aligné, déviation isolée, changement possible de
pattern, identité incertaine, activité inconnue cohérente, contexte conflictuel,
multi-entité, information partielle, forte déviation avec continuité,
réévaluation full/diff, idempotence, conflits optimistes, snapshots,
explications et readiness.

Les tests déterministes vérifient aussi les codes interdits, l'ordre des règles,
les IDs et les fingerprints. Les tests de concurrence couvrent les évaluations,
snapshots et applications concurrentes sous `-race`.

Benchmarks disponibles :

```text
EvaluateSmall / Medium / Maximal
ReevaluateFromDiffAdded / Modified10
FullEvaluationEquivalent
Plan8Hypotheses
ApplyPlan
PublicSnapshot10 / PublicSnapshot100
RegistryDigest
Explanation
```

## Readiness et limites

La readiness `ReadyForEvidenceDiscrimination` devient vraie seulement lorsque
le schéma, l'évaluation, la concurrence, les contributions, les contradictions,
les missing requirements et l'équivalence full/diff sont validés.

Les valeurs de frontière restent :

```text
RuntimeIntegrated                    = false
Durable                              = false
ActiveEvidenceRequestsImplemented   = false
SituationResolvedAutomatically      = false
SecurityAuthority                    = false
```

Competing Situation Hypotheses are in-memory, derived, experimental and not
runtime-integrated. Aucun WAL, checkpoint, replay, persistence, endpoint de
production, intégration Shadow, demande d'observation, action ou automation
n'est ajouté. La prochaine étape est Evidence Discrimination.
