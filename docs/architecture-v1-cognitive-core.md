# Synora V1 — Cognitive Core

Ce document définit le runtime décisionnel V1 de la branche
`integration/synora-v1-cognitive-core`. Le worktree stable et les worktrees de
préparation ne sont pas modifiés.

## Flux cible

```text
source/capteur/Vision
        │
        ▼
Discovery ── normalise, valide, surveille, sert le Web
        │ événement versionné
        ▼
Core ── snapshot canonique → State Encoder → MLP CPU
        │                     danger → incident → tâche → action
        │                     Safety/Execution Gate
        ▼
Universal Store ── commit atomique et outbox persistée
        │
        └──────────────► Discovery ── demande abstraite ──► périphérique
                                      résultat = nouvel événement
```

Le Core est l’unique propriétaire des écritures métier du Store. Discovery est
l’unique frontière avec le monde extérieur et ne fait pas de raisonnement de
danger, de calcul MLP, d’écriture métier directe ni de mutation matérielle
après un appel Web.

## Autorité

- Core, EventStore, Engine V1, Topology et Device Store sont autoritaires pour
  les faits et la cohérence d’exécution.
- La Vision publie uniquement des observations et résumés validés.
- Le MLP est l’unique source de décision normale ; il produit des sorties
  cognitives et des intentions abstraites.
- Le Safety/Execution Gate ne choisit jamais une stratégie de remplacement. Il
  autorise, bloque, limite ou rend sûre l’intention MLP selon les capacités,
  la topologie, l’autorisation, l’idempotence, le cooldown et le mode sûr.

Il n’existe pas de teacher, de fallback CGE, de `shadow`, de
`advisory_shadow`, ni de seconde source de danger ou d’action dans le runtime
V1.

## Contrats et limites

Tous les messages portent un identifiant, une version, un timestamp et une
corrélation d’épisode. Les files et journaux sont bornés. Aucune image, frame,
bbox, crop, embedding biométrique, média brut ou identifiant matériel ne passe
sur le bus, dans le snapshot cognitif ou dans la capture.

P0 est réservé au Core. La Vision peut signaler P1 mais reste non
décisionnelle. Le MLP est CPU uniquement ; le NPU RK3588 reste réservé à la
Vision. Les actions physiques sont désactivées en V1 (`dry_run`) et un modèle
absent ou incompatible démarre en `active_dry_run` fail-closed.

## Boucle d’événements

Un événement accepté suit `Discovery → Core → Store`. Une intention autorisée
est placée dans l’outbox, puis envoyée à Discovery. Un succès, échec, timeout ou
état indisponible revient comme nouvel événement `Discovery → Core → Store`.
Un doublon est reconnu par son identifiant et ne réouvre pas la boucle. Un
timeout calme et une fin de clip sont des événements ordinaires qui permettent
au MLP de réduire le danger.

## Capture et apprentissage

La capture V1 enregistre le snapshot canonique, le vecteur encodé, la décision,
le résultat éventuel et le label de scénario dans un JSONL versionné. Elle est
bornée et non bloquante. Aucun entraînement en ligne n’est introduit. Un futur
bundle devra déclarer les versions de schéma, la dimension, l’ordre des labels,
les hashes et la parité export/runtime avant promotion explicite.

## Rollback

Le rollback de cette intégration est purement Git et ne touche pas la stable :

```sh
git -C /home/rock/Synora-v1 log --oneline --decorate -8
git -C /home/rock/Synora-v1 worktree remove /home/rock/Synora-v1
git -C /home/rock worktree add /home/rock/Synora-v1 <commit-validé>
```

Avant promotion, valider `go test ./...`, `go vet ./...`,
`go build -buildvcs=false ./...`, les tests Python, les scénarios E2E et
`git diff --check`.
