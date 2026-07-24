# Pass 44 — Domain Capability Mapping

Cette passe ajoute `internal/cge/capabilitymapping`. Elle consomme une
`advisoryrequests.AdvisoryEvidenceRequest`, un `CapabilityCatalog` et un
`CapabilityInventory` explicitement fournis. Elle produit un
`CapabilityMappingAssessment` descriptif, in-memory, dérivé,
non autorisant, non exécutant et non intégré au runtime.

## Frontière

```text
AdvisoryEvidenceRequest
          ↓
CapabilityRequirement
          ↓
Domain Capability Catalog
          +
Capability Inventory
          ↓
Compatibility evaluation
  ├── kind
  ├── status
  ├── quality
  ├── scope
  ├── constraints
  ├── cost
  ├── latency
  └── sensitivity
          ↓
CapabilityMappingAssessment
  ├── compatible candidates
  ├── incompatible candidates
  ├── preferred candidate optional
  └── explanations
          ↓
[future] External Authorization Boundary
          ↓
[future] Authorized Capability Invocation
```

Une requête consultative décrit un besoin cognitif. Le mapping décrit les
capacités abstraites déclarées qui pourraient fournir l’information. Une
autorisation externe et une invocation concrète sont hors de cette passe.

```text
Compatible does not mean authorized.
Preferred does not mean selected.
Available does not mean permitted.
Mapped does not mean executable.
```

## Capacités abstraites

Le catalogue v1 contient uniquement :

```text
identity_observation
identity_continuity_observation
spatial_relation_observation
context_state_observation
source_consistency_observation
temporal_repetition_observation
pattern_alignment_observation
entity_multiplicity_observation
information_completeness_observation
```

Ces kinds ne sont ni des caméras, microphones, scanners, capteurs, réseaux,
notifications ou commandes. Ils décrivent seulement un type d’information
qu’un domaine pourrait déclarer fournir.

Le catalogue est statique, déterministe, versionné sous
`capability-catalog-v1:` et indépendant de tout matériel. Les définitions ne
contiennent ni adresse, endpoint, protocole, credential, script, plugin ou
procédure d’exécution.

Les kinds et textes publics contenant une capacité concrète, une commande ou
une signification sécuritaire sont refusés : capture, enregistrement, probe,
scan, verrouillage, alarme, intrusion, menace, autorisation, exécution et
équivalents. Les termes négatifs apparaissent uniquement dans les tests et
cette documentation de frontière.

## Instances et inventaire

Une `CapabilityInstance` possède un ID, un kind, des identifiants opaques de
domaine et fournisseur, un statut, une qualité déclarée, des classes
descriptives, des scopes, des contraintes, une révision et des fingerprints.
`ProviderID` n’est pas un endpoint et ne peut contenir d’adresse, chemin,
secret ou identifiant biométrique.

Les statuts d’instance sont `available`, `degraded`, `unavailable`, `unknown`,
`retired` et `invalidated`. `retired` et `invalidated` sont terminaux pour
l’instance concernée. `available` signifie seulement que le domaine la
déclare utilisable ; cela ne signifie pas qu’elle est autorisée.

L’inventaire est explicite et validé contre le fingerprint du catalogue. Il ne
découvre aucun appareil, ne lit aucun événement et ne conserve aucune
référence mutable échappée. Les instances, scopes et contraintes sont
canoniques et bornés.

## Qualité, scopes et contraintes

`CapabilityQuality` transporte fiabilité, complétude, fraîcheur, calibration et
nombre de sources. Les valeurs sont bornées dans `[0,1000]`. Une qualité non
calibrée reste marquée comme telle ; aucune fiabilité n’est inventée et aucun
score ne devient une probabilité.

Les scopes sont des couples génériques `Kind/Ref`, par exemple
`domain/home`, `zone/entry`, `entity-kind/person` ou `source-group/identity`.
Ils ne représentent pas une adresse physique détaillée.

Les contraintes sont fermées sur sept opérateurs : `equals`, `not_equals`,
`contains`, `minimum`, `maximum`, `present`, `absent`. Il n’existe aucun
moteur d’expression libre. Les contraintes hard échouées rendent le mapping
incompatible ; une contrainte soft échouée réduit son score et reste
expliquée.

## Exigences et table de mapping

`BuildRequirement` dérive une `CapabilityRequirement` compacte à partir de la
requête : kind abstrait requis, dimension, FactCodes, qualité minimale, classes
maximales et admission éventuelle d’un état dégradé.

```text
identity_confirmation             → identity_observation
identity_continuity_confirmation  → identity_continuity_observation
spatial_continuity_confirmation   → spatial_relation_observation
context_confirmation              → context_state_observation
source_consistency_confirmation   → source_consistency_observation
temporal_repetition_confirmation  → temporal_repetition_observation
pattern_alignment_confirmation    → pattern_alignment_observation
entity_count_confirmation         → entity_multiplicity_observation
context_completeness_confirmation → information_completeness_observation
```

La table est statique et ne mappe jamais une dimension vers un dispositif
concret.

## Compatibilité et incompatibilités

Un candidat est compatible lorsque le kind et la dimension correspondent, que
le statut est admissible, que les contraintes hard sont satisfaites, que le
scope est compatible ou explicitement autorisé comme inconnu, que la qualité
est suffisante et que les classes ne dépassent pas les limites déclarées.

Les résultats distinguent `compatible`, `compatible_degraded`, `unavailable`,
`incompatible`, `obsolete` et `invalidated`. Les raisons restent visibles,
notamment :

```text
capability.kind_mismatch
capability.unavailable
capability.status_unknown
capability.retired
capability.quality_insufficient
capability.quality_uncalibrated
capability.scope_mismatch
capability.scope_unknown
capability.constraint_failed
capability.sensitivity_exceeded
capability.cost_exceeded
capability.latency_exceeded
capability.catalog_mismatch
capability.inventory_stale
```

Une capacité inconnue ou indisponible ne devient jamais préférée. Une qualité
inconnue n’est pas silencieusement convertie en qualité nulle ou en succès.

## Scoring et policy

Les scores sont des indices descriptifs. La formule implémentée suit la
pondération : compatibilité 350, qualité 250, contraintes 150, scope 150,
disponibilité 100, puis pénalités coût 250, latence 250 et sensibilité 500.
Les résultats sont clampés dans `[0,1000]` et ne sont ni probabilités,
autorisations ni décisions de sécurité.

La policy par défaut utilise :

```text
MaxCandidatesPerRequest       = 16
MaxStoredMappingsPerRequest   = 32
MinCompatibilityPermille      = 600
MinQualityPermille            = 400
MinUtilityPermille            = 350
MinPreferredMarginPermille    = 75
AllowDegradedCapabilities     = true
AllowUnknownStatus            = false
AllowUnknownScope             = true
RequireCalibratedQuality      = false
MaximumCostClass              = high
MaximumLatencyClass           = extended
MaximumSensitivityClass       = moderate
PreserveIncompatibleCandidates = true
```

Les poids positifs totalisent 1000 et les pénalités totalisent 1000. Le
fingerprint de policy est `capability-mapping-policy-v1:` et n’est pas intégré
au fingerprint cognitif de déploiement.

## Analyse, classement et préféré

`Analyze` est pure et déterministe. Elle ne lit aucune horloge, fichier,
réseau, découverte ou couche cognitive amont. Elle indexe les définitions par
kind, évalue l’inventaire fourni et conserve plusieurs candidats jusqu’à la
limite de policy.

Le classement est : compatible avant incompatible, utility décroissante,
compatibilité décroissante, qualité décroissante, contraintes décroissantes,
scope décroissant, sensibilité, coût, latence, kind, instance ID puis candidate
ID. L’ordre de l’inventaire n’a aucune influence.

`PreferredCandidateID` est optionnel. Il exige une compatibilité réelle, les
seuils, une marge suffisante et l’absence d’égalité logique. Il indique la
meilleure correspondance descriptive dans l’inventaire fourni, jamais une
sélection ou une réservation.

Les requêtes `proposed`, `acknowledged` et `deferred` sont analysées. Les
requêtes `suppressed` exigent l’option explicite de policy. Les requêtes
`satisfied`, `expired`, `cancelled` et `invalidated` sont refusées par
`ErrRequestTerminal` et ne sont jamais réactivées.

## Assessment, réévaluation et lifecycle

`CapabilityMappingAssessment` conserve les fingerprints sources, l’exigence,
les candidats, les indicateurs de disponibilité/ambiguïté et un preferred
optionnel. Il ne modifie jamais la requête ou l’inventaire.

`Reevaluate` accepte une requête ou un inventaire modifié, recalcule le résultat
complet et augmente la révision lorsque l’assessment précédent est valide.
Une capacité ajoutée, retirée, dégradée ou rendue indisponible est donc visible
au prochain assessment.

Le lifecycle descriptif autorise les évolutions entre candidate, compatible,
compatible_degraded, unavailable et incompatible. `obsolete` et `invalidated`
sont terminaux pour l’assessment concerné.

## Plan, registry et snapshots

`MappingPlan` porte les fingerprints de requête et d’inventaire, la révision du
registry, les créations, mises à jour, invalidations et l’assessment résultant.
Il ne réserve ni capacité, ne change aucun statut d’instance et ne produit
aucune autorisation ou commande.

`Registry` est thread-safe, propriétaire unique, atomique et optimiste. Les
plans identiques sont idempotents ; deux plans incompatibles issus de la même
révision produisent `ErrSourceRevisionConflict`. Les snapshots copient les
assessments et les index par requête et instance de capacité.

Les fingerprints SHA-256 sont :

```text
capability-definition-v1:
capability-instance-v1:
capability-inventory-v1:
capability-requirement-v1:
capability-mapping-candidate-v1:
capability-mapping-assessment-v1:
capability-mapping-plan-v1:
capability-mapping-registry-v1:
```

## Explications, stockage compact et invariants

`Explain` renvoie des codes déterministes et quatre marqueurs toujours vrais :

```text
NotACommand = true
NotAuthorization = true
NotAProbability = true
NoSecurityMeaning = true
```

Le registry ne conserve ni catalogue complet, ni inventaire complet, ni
assessment source complet, ni Facts, hypothèses, endpoints, credentials ou
commandes. Il conserve seulement IDs, fingerprints, exigences, candidats,
scores, raisons et statuts.

Les validations couvrent IDs uniques, kinds connus, fingerprints, qualité,
scopes, contraintes, scores bornés, statuts, transitions, limites,
idempotence, absence de mutation d’entrée et absence de mapping vers une
exécution.

## Readiness et limites

Le readiness expose le catalogue, l’inventaire, les exigences, la compatibilité,
la qualité, les scopes, les contraintes, le classement, les explications, la
réévaluation, le registry, la concurrence et le stockage compact comme
implémentés.

Restent explicitement faux :

```text
RuntimeIntegrated
Durable
ConcreteDeviceMappingImplemented
ExternalAuthorizationImplemented
CapabilityReservationImplemented
CapabilityInvocationImplemented
ActiveObservationImplemented
SecurityAuthority
```

`ReadyForExternalAuthorization=true` signifie seulement que la frontière
descriptive est suffisamment structurée pour une future boundary
d’autorisation.

Cette passe n’ajoute aucun dispositif concret, driver, endpoint, réseau,
discovery, vision, action, automation, token, consentement, réservation,
commande, observation active, WAL, checkpoint, replay, persistence, migration,
ShadowEngine ou intégration runtime.

La prochaine étape possible est `External Authorization Boundary`.

**Domain Capability Mapping is in-memory, derived, descriptive,
non-authorizing, non-executing and not runtime-integrated.**
