# Pass 53 — Cognitive Situation Consolidation

## Objet

`internal/cge/cognitivesituation` est une projection dérivée et volatile de
`durableworkflow.WorkflowState`. Elle rassemble, par épisode, les résultats
déjà calculés par les couches cognitives expérimentales. Elle ne recalcule
aucune règle métier et ne possède ni WAL ni checkpoint propre.

```text
Committed Durable Workflow State
                ↓
       Cognitive Situation Builder
       ├── knowledge coverage
       ├── hypothesis ambiguity
       ├── missing evidence
       ├── advisory requests
       ├── capability availability
       └── authorization constraints
                ↓
        CognitiveSituation
       ├── phase
       ├── leading hypothesis optional
       ├── alternatives
       ├── missing information
       ├── recommendation readiness
       └── explanation
                ↓
       [future] Recommendation Planner
```

Références principales : `internal/cge/cognitivesituation/builder.go` (`Build`),
`internal/cge/durableworkflow/model.go` (`WorkflowState`,
`EpisodeWorkflowState`) et `internal/cge/shadowworkflow/pipeline.go` (`commit`).

## Contrat de consolidation

`Build` est pur, déterministe et limité à l’état fourni. Il ne lit ni horloge,
ni fichier, ni réseau, ne contacte aucun provider et ne modifie pas son entrée.
La filiation est d’abord validée par `durableworkflow.ValidateWorkflowState`.
Les références conservées sont des fingerprints, des IDs et des compteurs
bornés ; les facts, médias, payloads, grants et inventaires complets ne sont
pas recopiés.

Les profondeurs attendues sont explicites : `episode`, `situation_facts`,
`situation_hypotheses`, `evidence_discrimination`, `advisory_requests`,
`capability_mapping` et `authorization_boundary`. Une couche non configurée est
marquée `layer.not_configured`; elle n’est pas transformée en panne.

## Phases et précédence

La classification est déterministe et suit cette précédence :

```text
invalidated
→ stale
→ incomplete
→ awaiting_evidence
→ capability_unavailable
→ authorization_constrained
→ ambiguous
→ coherent
→ building / observing
```

`coherent` signifie seulement qu’un leader amont existe avec la couverture et
la marge configurées. Il ne signifie pas vrai. `ambiguous` conserve les
alternatives ; aucun leader n’est créé par cette couche. Une couche stale ne
contribue jamais comme fresh.

## Résumés

`KnowledgeSummary` compte les couches attendues, fresh, stale, absentes et
invalidées. Sa couverture est le ratio borné des couches attendues fresh, avec
un poids identique par couche ; elle n’est pas une moyenne de plausibilité,
utility, compatibility ou eligibility. Les facts unknown, asserted,
conflicting et le contexte partiel restent comptés sans interprétation ajoutée.

`HypothesisSummary` reprend le leader et la marge fournis par
`situationhypotheses`, conserve des alternatives bornées et présente
`PlausibilityPermille` comme score descriptif. La plausibilité n’est pas une
probabilité.

`EvidenceSummary` résume discrimination, redondance, utility et gain de
couverture déjà calculés. Il ne déclenche aucune observation.

`AdvisorySummary` compte les statuts existants et les besoins de mapping ou
d’autorisation. Une demande consultative n’est pas une commande.

`CapabilitySummary` indique si la profondeur est configurée, si un mapping est
disponible ou ambigu et conserve le candidat préféré abstrait. `preferred` ne
signifie pas sélectionné et aucune capacité n’est réservée.

`AuthorizationSummary` agrège éligibilité, refus, confirmation, différé et
default-deny. `eligible` ne signifie pas autorisé ; un fingerprint ou une
référence de grant ne devient pas un token d’exécution.

## Readiness et explication

`RecommendationReadiness` indique uniquement si une future couche de
recommandation pourrait être planifiée. Elle peut être bloquée par la
staleness, une invalidation, une couche manquante, une ambiguïté, une capacité
indisponible ou une contrainte d’autorisation. Elle ne produit aucune
recommandation dans cette passe.

`Explain` produit une structure compacte et déterministe : phase, codes de
raison, états de couches, types d’hypothèses, informations manquantes et
readiness. Aucun texte libre sensible n’est généré.

## Fingerprints et comparaison

Les préfixes sont :

```text
cognitive-situation-policy-v1:
cognitive-situation-v1:
cognitive-situation-readiness-v1:
cognitive-situation-diff-v1:
cognitive-situation-explanation-v1:
cognitive-situation-snapshot-v1:
```

Les slices et snapshots sont canonicalisés avant SHA-256. `Compare` détecte
les changements de phase, leader, couverture, advisory, capability,
autorisation et readiness, sans décider si un changement mérite une action.

## Intégration Shadow Workflow

Après `durableworkflow.Coordinator.Commit`, `shadowworkflow.Runtime` reconstruit
la situation de l’épisode modifié dans `shadowworkflow/cognitive_situation.go`.
Au démarrage, le recovery durable est terminé avant la reconstruction complète
du cache. Le cache est ensuite publié sous verrou, défensivement cloné et
recalculable ; il n’est pas persisté séparément et ne modifie pas le WAL.

Les méthodes internes sont `Runtime.CognitiveSituation` et
`Runtime.CognitiveSituations`. La première retourne également un clone. Un
échec de reconstruction post-commit dégrade le workflow Shadow ; il ne revient
pas dans le moteur historique.

## Invariants et limites

```text
A CognitiveSituation is not a decision.
Coherent does not mean true.
Plausibility is not probability.
Eligibility is not authorization.
A stale layer blocks recommendation readiness.
```

La couche ne produit ni décision, ni autorisation, ni commande, ni action, ni
automation, ni invocation, ni réservation. Elle n’ajoute aucun endpoint et
n’expose rien dans `PublicSnapshot`. Le workflow historique n’importe pas ce
package et ses fingerprints restent inchangés.

Les situations sont reconstruites depuis l’état durable après replay. Il n’y a
pas de persistence dérivée ; une corruption ou une incohérence de l’état source
reste une erreur du workflow durable, pas une occasion de fabriquer une
situation partielle.

## Readiness

`cognitivesituation.Readiness()` confirme les capacités de consolidation,
résumés, déterminisme, comparaison, explication, reconstruction après recovery,
intégration Shadow et validation de concurrence couvertes par les tests de
cette passe. La valeur `ReadyForRecommendationPlanning` est `true` : elle
autorise uniquement la planification future d’une recommandation, pas la
production d’une recommandation ou d’un effet.

Les valeurs d’autorité restent toujours `false` pour l’intégration décisionnelle,
le moteur de recommandation, l’exécution et l’autorité de sécurité.

## Prochaine étape

La suite naturelle est `Cognitive Recommendation Planning`, séparée de cette
projection et soumise à une nouvelle frontière d’autorité.
