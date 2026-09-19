# Architecture Synora V1 — état final

Ce document fixe le runtime actif après la purge finale. Les contrats restent
versionnés et toute évolution sémantique exige une nouvelle décision
d’architecture.

## Autorité

- Core, EventStore, Topology et Device Store sont les sources de vérité dans
  leurs domaines respectifs.
- Discovery est l’unique serveur externe et transmet des événements au Core.
- La Vision fournit uniquement des observations et résumés validés ; elle ne
  fournit jamais une commande.
- Le MLP V1 est la seule source de danger, de décision cognitive et de
  proposition d’action.
- Safety Gate, Action Ledger et limites physiques filtrent et bornent ces
  propositions. Ils n’ajoutent aucune intention cognitive.
- `active_dry_run` est le défaut et `physical_action_executed` reste toujours
  faux dans cette version.

## Flux nominal

```text
Discovery → Core → CognitiveSnapshot → MLP CPU → Safety Gate → Universal Store → Discovery
```

Les événements Vision sont réduits en faits déterministes avant l’appel du
MLP. Le Core est l’unique écrivain du Universal Store. Aucun Web ou
périphérique n’écrit directement dans le Store.

## Frontières

- aucune frame, bbox, crop, embedding biométrique ou média brut sur le bus ;
- contrats, dimensions, ordres de features et manifestes versionnés ;
- épisodes, pistes, files, outbox et ledgers bornés ;
- P0 réservé au Core ;
- P1 Vision prioritaire mais non décisionnel ;
- aucune exécution physique ;
- aucun retour MLP → CognitiveSnapshot → MLP dans le même cycle.

Le Safety Gate refuse les capacités absentes, les topologies non autorisées,
les dépassements de budget et toute requête qui ne respecte pas le mode
`active_dry_run`. Il ne remplace pas la proposition du MLP par une décision
indépendante.

## Périmètre V1

Le dépôt contient uniquement les services, contrats et validations nécessaires
au flux nominal décrit ci-dessus. Toute extension s’ajoute par un contrat
versionné et une décision d’architecture dédiée.
