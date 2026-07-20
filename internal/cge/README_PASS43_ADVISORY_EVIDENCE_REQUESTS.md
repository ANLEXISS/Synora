# Pass 43 — Advisory Evidence Requests

Cette passe ajoute `internal/cge/advisoryrequests`. Le domaine consomme
exclusivement `evidencediscrimination.DiscriminationAssessment` et transforme
certains `EvidenceCandidate` descriptifs en propositions cognitives actives.
Il est entièrement in-memory, dérivé, consultatif, non exécutant et non
intégré au runtime.

## Frontière

Un `EvidenceCandidate` signifie qu’une information supplémentaire pourrait
être utile. Une `AdvisoryEvidenceRequest` signifie que ce besoin mérite d’être
conservé comme proposition consultative versionnée. Cette passe ne choisit
aucun capteur, ne commande aucune observation et ne prend aucune décision de
sécurité.

```text
DiscriminationAssessment
          ↓
Useful EvidenceCandidates
          ↓
Semantic request deduplication
          ↓
AdvisoryEvidenceRequests
  ├── proposed
  ├── acknowledged
  ├── deferred
  ├── suppressed
  ├── satisfied
  ├── expired
  ├── cancelled
  └── invalidated
          ↓
[future] Domain Capability Mapping
          ↓
[future] External Authorization
          ↓
[future] Authorized Observation Execution
```

`Acknowledged does not mean authorized.`
`Proposed does not mean executable.`
`Preferred does not mean mandatory.`
`Satisfied does not prove that a sensor command occurred.`

## Identité et générations

`AdvisoryRequestKey` est une identité sémantique stable fingerprintée avec
SHA-256 sous `advisory-request-key-`. Il dépend de l’`EpisodeID`, du kind, de
la dimension, des FactCodes canoniques et des paires d’hypothèses canoniques.
L’ordre des codes et des paires ne change donc pas la clé.

`AdvisoryRequestID` est l’occurrence de cette clé et de sa `Generation`, sous
le préfixe `advisory-request-`. Une requête terminale n’est jamais réactivée.
Si le besoin réapparaît après `satisfied`, `expired`, `cancelled` ou
`invalidated`, la même clé reçoit une génération supérieure et un nouvel ID.

Les paires sont transportées sous la forme compacte
`AdvisoryHypothesisPair{FirstID, SecondID}` : elles sont ordonnées, uniques et
ne contiennent ni hypothèse complète, ni FactSet, ni score de sécurité.

## Modèle et marqueurs

Une requête conserve seulement l’ID du candidat, les codes, les paires, les
scores descriptifs bornés dans `[0,1000]`, les classes, les fingerprints, les
timestamps et les métadonnées de lifecycle. Elle ne conserve aucun
`DiscriminationAssessment`, `PotentialOutcome`, ensemble de faits ou ensemble
d’hypothèses complet.

Les cinq marqueurs sont obligatoirement vrais :

```text
NotACommand = true
NotAProbability = true
NoSecurityMeaning = true
RequiresExternalMapping = true
RequiresExternalAuthorization = true
```

## Statuts et lifecycle

Les statuts non terminaux sont `proposed`, `acknowledged`, `deferred` et
`suppressed`. Les statuts terminaux sont `satisfied`, `expired`, `cancelled`
et `invalidated`. Le lifecycle pur refuse les transitions identiques, toute
sortie d’un terminal et toute réactivation avec le même ID.

Les dispositions sont explicites : `acknowledge`, `defer`, `cancel` et
`restore_proposal`. Elles portent un acteur borné, une date UTC, une révision
source optimiste et, pour `defer`, une date future obligatoire. Elles ne
constituent aucune autorisation externe.

## Policy et planner

`DefaultPolicy` utilise des limites conservatrices : 4 demandes actives par
épisode, 32 stockées, 32 ReasonCodes, 32 FactCodes, 64 paires, utilité
minimale 250, discrimination minimale 200, couverture minimale 100,
redondance de suppression 700, marge préférée 75, TTL 15 minutes et
réévaluation différée 5 minutes. La haute sensibilité est exclue par défaut
et les demandes supprimées sont conservées.

La policy est validée et fingerprintée sous
`advisory-evidence-policy-v1:`. Ce fingerprint n’est pas un fingerprint de
déploiement.

`Plan` est pur : l’heure est fournie par `EvaluatedAt`, et il ne lit ni horloge,
fichier, réseau, événement brut ou registre mutable. Il déduplique par clé,
met à jour une occurrence active, crée une génération si la dernière
occurrence est terminale, applique les limites, calcule le classement et
produit un `AdvisoryPlan` complet pour l’épisode.

La sélection exige l’utilité minimale et soit le pouvoir discriminant, soit
le gain de couverture. Une paire non vide ou un gain de couverture significatif
est requis. La sensibilité haute, la redondance excessive, l’utilité faible,
les limites atteintes et les seuils insuffisants peuvent conduire à
`suppressed` sans invalider la source.

Une disparition de candidat devient `satisfied` seulement si le nouvel
assessment indique que l’ambiguïté descriptive n’est plus utile. Une requête
simplement acknowledged, différée ou supprimée n’est jamais satisfaite par
administration. L’expiration dépend uniquement de `EvaluatedAt` et de
`ExpiresAt`, avec respect d’un defer encore futur.

## Classement et explication

Le classement est déterministe : utilité décroissante, discrimination
décroissante, couverture décroissante, redondance croissante, sensibilité,
coût, latence, kind, clé puis ID. Seules les demandes actives reçoivent un
rang. `PreferredRequestID` reste optionnel et exige la première place ainsi
qu’une marge suffisante sans égalité logique.

`Explain` produit des codes déterministes et recopie les marqueurs négatifs.
Il ne produit pas de texte libre, de probabilité, de score de menace ou de
commande.

## Registre et snapshots

`Registry` est le propriétaire thread-safe des requêtes. `ApplyPlan` et
`ApplyDisposition` sont atomiques, optimistes et défensifs. Une application
identique est idempotente ; deux plans incompatibles construits depuis la même
révision donnent `ErrSourceRevisionConflict`. Les snapshots copient les
requêtes, les index par ID, clé et épisode, et leur digest stable.

Les fingerprints SHA-256 sont versionnés sous
`advisory-request-v1:`, `advisory-plan-v1:` et
`advisory-request-registry-v1:`. Les slices et maps ne sont jamais exposées
par référence mutable. Les diffs signalent ajouts, mises à jour, transitions
et suppressions explicites ; le chemin normal conserve les objets terminaux.

## Invariants, tests et readiness

Les validations couvrent les IDs et clés dérivés, les fingerprints source,
les scores, les codes triés, les paires canoniques, les limites, les marqueurs,
les transitions, les générations, l’idempotence et les conflits optimistes.
Les tests couvrent candidats utiles et multiples, seuils, redondance,
sensibilité, mise à jour, satisfaction, expiration, réapparition, dispositions,
égalité de classement, assessment forgé, concurrence et absence de sémantique
exécutable. Des benchmarks dédiés couvrent planification, application,
snapshot, digest et explication.

La readiness annonce le modèle, le lifecycle, la déduplication, les
générations, le registre, la concurrence et le stockage compact comme prêts.
Runtime, durabilité, mapping de capacité, autorisation externe, observation
active, commandes de capteur et autorité de sécurité restent explicitement
faux. La prochaine étape possible est `Domain Capability Mapping`.

## Limites et exclusions

Cette passe n’ajoute aucun `CameraCapability`, `MicrophoneCapability`,
`PresenceSensorCapability`, `NetworkProbeCapability`, `DeviceCommand`, bus,
dispatcher, executor, token d’autorisation, action, automation, vision,
réseau, périphérique, MQTT, Bluetooth, Zigbee, API, WAL, checkpoint, replay,
persistence, migration, Shadow, Field Trial Recorder ou intégration runtime.

La correspondence entre une dimension descriptive et une capacité matérielle
appartient à un futur domain pack et à une future passe d’autorisation.
