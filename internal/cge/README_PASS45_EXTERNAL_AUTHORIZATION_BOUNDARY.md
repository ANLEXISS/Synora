# Passe 45 — External Authorization Boundary

Cette couche sépare strictement l'utilité cognitive, le mapping abstrait et
l'éligibilité selon des politiques externes.

```text
CapabilityMappingAssessment
          ↓
AuthorizationContext
          +
AuthorizationPolicySet
          +
ExternalGrantSnapshot
          ↓
Authorization Boundary
  ├── explicit allow rules
  ├── explicit deny rules
  ├── purpose limitation
  ├── scope limitation
  ├── sensitivity limitation
  ├── valid grants
  ├── expired/revoked grants
  └── default deny
          ↓
AuthorizationBoundaryAssessment
  ├── eligible candidates
  ├── denied candidates
  ├── confirmation required
  ├── policy conflicts
  └── preferred eligible candidate optional
          ↓
[future] Separate grant issuance
          ↓
[future] Authorized invocation
```

## Frontière

`CapabilityMappingAssessment` décrit une capacité abstraite compatible.
`AuthorizationBoundaryAssessment` indique seulement si cette correspondance
est éligible, refusée, différée ou soumise à une confirmation externe.

`Eligible` ne signifie pas autorisé. Une référence de grant ne signifie pas un
token d'exécution. `Preferred` ne signifie pas sélectionné. L'utilité
cognitive ne peut jamais contourner une policy d'autorisation. En l'absence
d'information, le refus par défaut s'applique.

## Contexte, finalité et scope

Le contexte porte une finalité neutre, des scopes canoniques, une fenêtre
temporelle, un domaine et des classes opaques d'acteur et d'origine. Les
finalités v1 sont dérivées des kinds abstraits: identité, continuité
identitaire, continuité spatiale, contexte, cohérence de source, répétition
temporelle, alignement de pattern, multiplicité d'entités et complétude.

Les scopes ne contiennent ni endpoint, IP, MAC, chemin, credential ou donnée
biométrique.

## Policies et précédence

La policy par défaut est `deny`. Les effets sont limités à `allow_eligibility`,
`deny`, `require_external_confirmation` et `defer`. Un `deny` applicable
l'emporte sur un `allow`; les conflits sont conservés et expliqués. Aucun
effet d'exécution, réservation, invocation, override ou consentement implicite
n'existe.

## Grants externes

Les grants sont fournis par un `ExternalGrantSnapshot`. Leur finalité, kind,
domaine, scopes et fenêtre temporelle sont vérifiés. Les états `missing`,
`expired`, `not_yet_valid`, `revoked`, `purpose_mismatch`,
`capability_mismatch`, `scope_mismatch` et `domain_mismatch` restent visibles.
Cette passe ne vérifie pas de signature cryptographique et n'accède à aucun
système externe.

## Assessment et classement

Chaque mapping conserve les règles appliquées, les refus, les confirmations,
les grants satisfaits ou rejetés, les conditions, les conflits, les raisons et
des scores descriptifs bornés. Plusieurs candidats éligibles sont conservés.
Un préféré est facultatif et nécessite une marge suffisante; il ne produit
aucun droit d'utilisation.

## Réévaluation et registre

`Analyze`, `Plan` et `Reevaluate` sont purs et déterministes. Le registre est
in-memory, thread-safe, atomique, idempotent et soumis à révision optimiste.
Les snapshots et fingerprints sont défensivement copiés et déterministes.

Le registre ne conserve pas les policy sets complets, les inventories, les
snapshots de grants complets, les credentials, tokens, signatures ou couches
cognitives amont. Il conserve seulement les IDs, fingerprints, conditions,
statuts, raisons, références de règles/grants et scores compacts.

## Lifecycle

Les états globaux sont `active`, `eligible`, `denied`,
`confirmation_required`, `deferred`, `obsolete` et `invalidated`.
`obsolete` et `invalidated` sont terminaux pour la révision concernée. Une
éligibilité nouvelle doit toujours être réévaluée; elle ne devient pas une
permission persistante.

## Exclusions

Le domaine ne contient aucun dispositif concret, endpoint, driver, token,
réservation, commande, invocation, observation active, action, automation,
API de production, WAL, persistence ou intégration runtime.

Cette couche est in-memory, descriptive, non autorisante au sens d'une
émission de grant, non exécutante et non intégrée au runtime. La prochaine
étape possible est la durabilité cohérente du workflow cognitif, puis une
frontière séparée d'émission de grants si elle est explicitement conçue.
