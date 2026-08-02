# Passe 55 — Historical Decision Comparison

## Objectif

Le package decisioncomparison compare une décision historique expurgée avec une
CognitiveSituation et un CognitiveRecommendationSet. Le résultat est un
diagnostic descriptif de calibration. Il ne choisit jamais un moteur et ne
modifie jamais le chemin historique.

La décision de production réelle est pkg/contract.Decision. Elle est construite
par internal/engine/engine.go (Engine.Analyze), puis appliquée par
cmd/synora-core/main.go (coreApp.processEvent). L’adaptateur expurgé est dans
cmd/synora-core/decision_comparison_adapter.go.

~~~text
Historical decision
        │ production authority retained
        ├─────────────┐
        │             │
        │     CognitiveSituation
        │             ↓
        │   CognitiveRecommendationSet
        │             │
        └──────┬──────┘
               ↓
    Historical Decision Comparison
      ├── continuity alignment
      ├── transition alignment
      ├── ambiguity posture
      ├── evidence posture
      ├── descriptive divergence
      └── comparability coverage
               ↓
       calibration diagnostic only
~~~

## Frontière d’autorité

La décision historique conserve toujours l’autorité de production. Les
marqueurs obligatoires sont :

~~~text
HistoricalDecisionRetainsAuthority = true
CognitiveRecommendationHasNoAuthority = true
DoesNotOverrideHistoricalDecision = true
CalibrationOnly = true
~~~

Un alignement ne prouve pas que les moteurs sont corrects. Une divergence ne
prouve pas une erreur. SignificantDivergence est un marqueur de calibration,
jamais une alerte.

## HistoricalDecisionRef

La référence contient uniquement des identifiants opaques, les états connus,
les indicateurs effectivement disponibles, un score borné éventuel, un
timestamp, une révision et un fingerprint. Les champs indisponibles restent
inconnus.

Elle ne contient ni identité, ni média, ni payload, ni commande, ni action.
Elle est clonée avant admission dans la queue Shadow.

## Dimensions et catégories

Les dimensions versionnées sont : continuité d’état, transition d’état,
transition cognitive, stabilité d’interprétation, posture d’ambiguïté,
posture d’observation, posture d’evidence, fraîcheur et timing.

Chaque dimension expose un statut aligned, partially_aligned, divergent,
incomparable, insufficient_information, stale ou invalidated. Les indices sont
descriptifs et bornés ; ils ne sont ni probabilités ni scores de sécurité.

Les catégories globales distinguent aligned, partially_aligned, divergent,
cognitive_more_conservative, historical_more_decisive,
cognitive_transition_only, historical_transition_only, incomparable,
insufficient_information, stale et invalidated.

## Calculs

Les poids de policy totalisent 1 000. Pour les dimensions comparables :

~~~text
coverage = Σ(poids × couverture × couverture) / Σ(poids × couverture)
alignment = Σ(poids × couverture × alignement) / Σ(poids × couverture)
divergence = Σ(poids × couverture × divergence) / Σ(poids × couverture)
~~~

Les dimensions non comparables sont exclues du dénominateur. Une divergence
significative exige une couverture minimale, au moins deux dimensions
comparables et un seuil de divergence. Une incomparabilité n’est jamais
transformée en divergence.

## Planner pur et validation

decisioncomparison.Compare est pur, déterministe et sans accès au Core, au bus,
au réseau, aux fichiers, à l’automation ou aux actions. Il valide les
fingerprints, la situation, le recommendation set, l’identité d’épisode et la
filiation.

Une situation stale produit CategoryStale et Comparable=false. Une situation
invalidated produit CategoryInvalidated et Comparable=false. Un état historique
manquant ou une recommandation sans signal utile produit incomparable.

## Intégration Shadow Workflow

~~~text
historical decision
→ redacted ShadowWorkflowInput
→ durable commit
→ CognitiveSituation
→ CognitiveRecommendationSet
→ HistoricalDecisionComparison
→ atomic volatile projection
~~~

La référence historique est optionnelle. Sans référence, la situation et les
recommandations sont construites normalement, aucune comparaison n’est
fabriquée et comparison_skipped_no_historical_ref est incrémenté.

Une erreur de comparaison après commit conserve la situation et les
recommandations déjà publiées, omet la comparaison invalide et dégrade
uniquement le Shadow Workflow.

Le snapshot atomique contient une révision commune pour situations,
recommandations et comparaisons. Les accès internes retournent des clones
défensifs.

## Persistence et recovery

Cette passe ne modifie pas le WAL durable. HistoricalDecisionRef et les
comparaisons sont conservés uniquement pendant le cycle et dans le cache
volatile. Après recovery, les situations et recommandations sont reconstruites,
mais aucune comparaison historique n’est fabriquée.

~~~text
ComparisonRecoverySupported = false
DurableCalibrationLedgerImplemented = false
~~~

La prochaine étape possible est un ledger append-only de calibration.

## Instrumentation

Le runtime conserve uniquement des agrégats :

~~~text
comparisons_total
comparisons_aligned
comparisons_partially_aligned
comparisons_divergent
comparisons_incomparable
comparisons_cognitive_more_conservative
comparisons_historical_more_decisive
comparisons_cognitive_transition_only
comparisons_historical_transition_only
comparisons_significant_divergence
comparisons_skipped_no_historical_ref
comparison_build_failures
comparison_build_duration_ns
~~~

Aucun EventID, EpisodeID, state code détaillé, reason code ou contenu cognitif
n’est enregistré par cette instrumentation.

## Fingerprints

~~~text
historical-decision-ref-v1:
historical-decision-comparison-dimension-v1:
historical-decision-comparison-v1:
historical-decision-comparison-explanation-v1:
historical-decision-comparison-snapshot-v1:
historical-decision-comparison-policy-v1:
~~~

Les dimensions et reason codes sont canoniques. L’ordre d’entrée ne change pas
le fingerprint logique.

## Tests et limites

Les tests couvrent continuité, transitions historiques et cognitives,
ambiguïté, evidence, références absentes ou forgées, staleness, invalidation,
filiation, explication, recompare, snapshots défensifs et concurrence.

Limites restantes : aucune comparaison historique durable, aucune calibration
automatique, aucun feedback vers Engine.Analyze ou coreApp.processEvent, aucune
exposition PublicSnapshot/endpoint, aucune alerte, décision, autorisation,
commande ou action.

ReadyForCalibrationLedger=true signifie seulement que le modèle est prêt à
être évalué pour une future persistance de calibration.

~~~text
Alignment does not prove correctness.
Divergence does not prove an error.
The historical decision retains production authority.
A comparison never changes the decision it evaluates.
Significant divergence is a calibration marker, not an alert.
~~~
