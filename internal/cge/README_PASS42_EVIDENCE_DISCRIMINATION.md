# Pass 42 — Evidence Discrimination

Cette passe ajoute `internal/cge/evidencediscrimination`. Le domaine consomme
exclusivement un `situationfacts.FactSet`, un
`situationhypotheses.CompetingHypothesisSet` et son schéma. Il produit des
besoins de preuve descriptifs et contrefactuels, jamais une acquisition.

## Frontière

Une hypothèse décrit une explication compatible avec les faits. Un candidat
de preuve décrit une dimension qui pourrait affecter différemment plusieurs
hypothèses. Il ne choisit pas d’hypothèse et ne transforme pas un besoin en
commande.

```text
FactSet + Competing Hypotheses
             ↓
Unresolved hypothesis pairs
             ↓
Missing and conflicting dimensions
             ↓
Evidence candidates
             ├── potential outcomes
             ├── discrimination
             ├── coverage gain
             ├── redundancy
             └── utility
             ↓
Ranked descriptive evidence needs
             ↓
[future] Advisory Evidence Requests
             ↓
[future] Authorized domain orchestration
```

## Modèle

`EvidenceCandidate` possède un ID déterministe, une dimension, des FactCodes
requis, des `PotentialOutcome`, des paires d’hypothèses, des classes
descriptives de coût/latence/sensibilité, ainsi que des scores bornés dans
`[0,1000]`. Chaque paire est canonique et chaque résultat potentiel est
contrefactuel : aucun résultat n’est injecté dans un registre de faits.

Le catalogue est statique, versionné et fingerprinté sous
`evidence-discrimination-catalog-v1:`. Les types couvrent l’identité, sa
continuité, la continuité spatiale, le contexte, la cohérence des sources, la
répétition temporelle, l’alignement de pattern, la multiplicité d’entités et
la complétude de l’information.

Les classes coût, latence et sensibilité sont des étiquettes descriptives.
Elles ne désignent aucun appareil ni capacité concrète.

## Analyse et classement

L’analyse regroupe les `MissingRequirement`, les conflits et les contributions
des hypothèses. Une paire est pertinente lorsque les hypothèses sont actives,
proches ou insuffisamment couvertes. Le pouvoir de discrimination combine la
séparation de paires et le contraste entre résultats possibles. Le gain de
couverture vient des dimensions inconnues, conflictuelles ou manquantes. La
redondance diminue l’utilité lorsqu’une dimension est déjà établie.

Ces nombres sont des indices structurels déterministes. Ils ne sont ni des
probabilités, ni des scores de décision. Une information inconnue n’est jamais
convertie en contradiction.

Le classement est stable : utilité décroissante, discrimination décroissante,
gain de couverture décroissant, redondance croissante, puis classes et IDs.
`BestCandidateID` reste vide si le seuil ou la marge ne sont pas atteints.

## Diff, plan et registre

`ReevaluateFromDiff` vérifie les fingerprints, les épisodes et les révisions,
puis exige l’équivalence avec une analyse complète. Il ne relance pas
l’extraction incrémentale des faits. `EvidencePlan` est un plan descriptif
atomique ; il ne réserve ni capteur ni observation.

Le registre est in-memory, propriétaire, thread-safe et optimiste. Les
snapshots sont défensifs, les listes sont ordonnées et le digest est
déterministe. Une application identique est idempotente ; deux plans
incompatibles issus de la même révision produisent un conflit de révision.

## Invariants et limites

Les FactCodes et types sont vérifiés contre le schéma des faits. Les paires et
les outcomes sont dédupliqués, les scores sont bornés, les IDs sont dérivés
par SHA-256 et les fingerprints ne contiennent aucune donnée directement
lisible. Les explications portent toujours les marqueurs
`NotACommand`, `NotAProbability` et `NoSecurityMeaning` à `true`.

La couche ne possède pas de WAL, replay, checkpoint, persistence, endpoint,
runtime ou demande active d’observation. Elle ne consomme aucun événement
brut, aucun épisode et aucun capteur.

## Exemple

```text
Hypotheses:
- isolated_deviation
- possible_pattern_shift

Current facts:
- one temporal deviation
- routine reference available
- no repeated shifted occurrence yet

Evidence candidate:
- temporal_repetition_confirmation

Potential discrimination:
- repeated similar divergence supports possible_pattern_shift
- absence of repetition preserves isolated_deviation

No observation is executed.
No command is issued.
No security meaning is produced.
```

**Evidence Discrimination is in-memory, derived, advisory-only, non-executing
and not runtime-integrated.** La prochaine étape est une couche consultative
d’Advisory Evidence Requests, soumise à une orchestration autorisée.
