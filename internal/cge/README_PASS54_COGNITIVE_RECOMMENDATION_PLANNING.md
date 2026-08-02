# Pass 54 — Cognitive Recommendation Planning

## Objet

`internal/cge/cognitiverecommendation` transforme une `CognitiveSituation`
validée en un ensemble borné de recommandations cognitives descriptives. Il
ne lit aucune couche amont et ne possède ni WAL, ni checkpoint, ni autorité de
production.

```text
Committed WorkflowState
        ↓
CognitiveSituation
        ↓
Cognitive Recommendation Planner
        ├── continue observation
        ├── maintain interpretation
        ├── preserve ambiguity
        ├── request additional evidence
        ├── reassess later
        ├── flag cognitive transition
        └── no recommendation
        ↓
CognitiveRecommendationSet
        ├── recommendations ranked
        ├── primary optional
        ├── ambiguity preserved
        ├── explanation
        └── no production authority
        ↓
[future] Historical Decision Comparison
```

## Frontière sémantique

Une recommandation cognitive est une orientation descriptive destinée à une
couche supérieure. Elle n’est ni une décision, ni une alerte, ni une
autorisation, ni une commande, ni une action. `request_additional_evidence`
référence une demande consultative existante ; elle ne crée ni demande,
capacité, grant, réservation ou observation.

`maintain_current_interpretation` ne rend pas l’interprétation vraie.
`preserve_ambiguity` n’élit aucun leader. Une recommandation primaire reste
optionnelle et non exécutable.

## Kinds et cibles

Les kinds sont fermés et versionnés : observation continue, maintien de
l’interprétation, demande d’évidence, réévaluation après changement de
contexte ou nouvelle observation, préservation de l’ambiguïté, transition
cognitive et absence de recommandation.

Les cibles sont limitées à `situation`, `hypothesis`, `evidence_request`,
`context` et `future_observation`. Elles contiennent seulement des IDs opaques,
codes et fingerprints.

## Règles par phase

| Phase | Résultat descriptif |
| --- | --- |
| `observing` | observation continue, ou absence selon policy |
| `building` | observation continue et réévaluation après observation |
| `coherent` | maintien de l’interprétation |
| `ambiguous` | préservation de l’ambiguïté et evidence existante si disponible |
| `incomplete` | réévaluation après contexte, evidence uniquement si référencée |
| `awaiting_evidence` | demande d’évidence existante obligatoire |
| `capability_unavailable` | ambiguïté préservée et réévaluation contextuelle |
| `authorization_constrained` | maintien descriptif et réévaluation ; aucun contournement |
| `stale` | réévaluation bloquée |
| `invalidated` | `no_recommendation` invalidée uniquement |

Les règles ne calculent aucun score cognitif amont. Les quatre indices bornés
(`ApplicabilityPermille`, `InformationValuePermille`, `StabilityPermille` et
`UrgencyPermille`) sont descriptifs. L’urgence signifie ici une priorité de
revue cognitive, jamais un danger ou une urgence physique.

## Classement et ambiguïté

Le classement est canonique : statut applicable, applicabilité, valeur
d’information, stabilité, priorité de revue, kind, fingerprint de cible puis
ID. Le primaire n’est produit que pour un candidat applicable dont la marge
respecte la policy. Il est supprimé en cas d’ambiguïté, d’égalité logique,
de marge insuffisante, de staleness ou d’invalidation.

## Filiation et lifecycle

Chaque recommandation référence le fingerprint et la révision de la situation
source. Une demande d’évidence utilise uniquement
`AdvisorySummary.PreferredRequestID`, déjà transporté par
`CognitiveSituation`. Une référence absente produit `ErrMissingAdvisoryReference`.

Lorsqu’une situation évolue, le nouveau set est recalculé ; les références
précédentes peuvent être indiquées comme remplacées dans le résultat dérivé.
L’ancien set n’est jamais muté et aucun lifecycle temporel autonome n’est créé.

## Intégration Shadow Workflow

Le runtime publie maintenant un `CognitiveProjectionSnapshot` combiné :

```text
1. durable commit
2. publication CognitiveSituation
3. planification CognitiveRecommendationSet
4. publication atomique des deux caches dérivés
```

Le snapshot contient une révision commune, le digest du snapshot de situations,
les sets indexés par épisode et un digest global. Les méthodes internes
`CognitiveSituation`, `CognitiveSituations`, `CognitiveRecommendation` et
`CognitiveRecommendations` retournent des clones défensifs. Le cache est
reconstruit après recovery et ne possède aucune persistence séparée.

Une erreur de reconstruction dégrade le Shadow Workflow seulement. Le moteur
historique ne lit jamais le résultat du planner.

## Comparaison et explication

`Compare` détecte les changements de primaire, d’ambiguïté,
d’applicabilité, d’ajout, de retrait et de statut. `Explain` fournit des codes
déterministes et les marqueurs d’absence d’autorité ; aucun texte libre
sensible n’est produit.

Fingerprints SHA-256 :

```text
cognitive-recommendation-policy-v1:
cognitive-recommendation-v1:
cognitive-recommendation-set-v1:
cognitive-recommendation-diff-v1:
cognitive-recommendation-explanation-v1:
cognitive-recommendation-snapshot-v1:
```

## Instrumentation Shadow Workflow

Après une publication réussie, le runtime ne conserve que des agrégats
expurgés : nombre de sets et de recommandations, recommandations applicables
ou bloquées, ambiguïtés, demandes d'évidence, transitions, primaires et
échecs. recommendation_build_duration_ns cumule la durée de projection côté
runtime ; le planner lui-même reste sans horloge et sans effet de bord. Aucun
identifiant d'épisode, de situation, d'hypothèse ou de demande n'est enregistré
dans cette instrumentation.

## Invariants

```text
A recommendation is not a decision.
A recommendation is not an alert.
Requesting evidence is not invoking a capability.
Maintaining an interpretation does not make it true.
A primary recommendation is optional and non-executable.
```

Il n’existe aucune recommandation depuis une situation stale applicable,
aucun fait ou hypothèse inventé, aucune capacité ou autorisation fabriquée,
aucune commande, action, automation, endpoint, invocation ou comparaison avec
la décision historique dans cette passe.

## Readiness et limites

`cognitiverecommendation.Readiness()` indique
`ReadyForHistoricalDecisionComparison=true` après validation du planner, du
classement, de l’atomicité de projection, du recovery, de l’incrémental, des
snapshots et des race tests.

Restent explicitement faux :

```text
ProductionDecisionIntegrated
HistoricalDecisionComparisonImplemented
AutomationIntegrated
ActionExecutionImplemented
SecurityAuthority
```

La recommandation n’est pas persistée séparément. La prochaine étape est
`Historical Decision Comparison`, encore soumise à une frontière d’autorité
distincte.
