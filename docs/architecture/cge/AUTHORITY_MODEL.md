# Modèle d’autorité

## Plafond

Le catalogue fixe `authority_ceiling: advisory` pour le namespace `synora.cge`.
Les niveaux `authorized_decision` et `authorized_action` existent dans le
vocabulaire global pour décrire le système historique, mais aucun contrat dont
le propriétaire est CGE ne les utilise.

La décision historique est produite et reste détenue par le Core historique.
Lorsqu’elle est observée par le CGE, sa copie porte des références protégées et
le marqueur `HistoricalDecisionRetainsAuthority`. La comparaison est une
sortie de calibration/advisory, pas une décision de production.

## Invariants

Les sorties cognitives actuelles doivent conserver, selon leur modèle :

```text
NotADecision=true
NotAnAction=true
NoSecurityMeaning=true
HistoricalDecisionRetainsAuthority=true
DoesNotOverrideHistoricalDecision=true
```

Le workflow, les épisodes, hypothèses, situations, recommandations, analytics
et ledger ne publient pas d’`ActionRequest`. `action.request` est catalogué
comme contrat historique/automation distinct, avec `owner: automation`, et est
interdit aux consommateurs CGE.

Les diagnostics sont read-only. Une erreur ou un état `degraded` ne devient pas
une commande. Le recovery reconstruit l’état ; il ne republie pas d’action et
ne modifie pas rétroactivement l’autorité historique.

## Vérification

`internal/cge/contractcatalog` vérifie le plafond, les autorités des contrats,
les stores durables et les événements allowlistés. Les tests refusent tout
contrat CGE `authorized_decision` ou `authorized_action`, tout store durable
autorisant un secret/biométrique en clair, et toute admission de
`vision.motion` au workflow.
