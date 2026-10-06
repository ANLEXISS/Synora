# Intelligence V1 : trace MLP et surface Web

Le checkout V1 expose une surface de lecture seule pour l’Intelligence locale.
Le Core reste l’unique propriétaire de l’état et de l’écriture du Store. L’API
observe uniquement les événements `core.decision` ciblés vers `api`; elle ne
publie aucune commande et ne peut pas déclencher d’action physique.

## Contrat de trace

Le contrat est `synora.mlp-trace/v1`. Seules les traces marquées `redacted: true`
sont acceptées par l’API. La projection enlève les poids, embeddings, valeurs
brutes, médias, identités, secrets, commandes et chemins matériels.

Les bornes sont appliquées à la réception : 24 traces conservées, au plus
5 nœuds actifs par couche et 64 chemins actifs. La topologie est limitée aux
quatre heads MLP V1 et aux dimensions descriptives de leurs couches.

`runtime_mode` et `inference_status` sont conservés pour que l’interface ne
présente pas un calcul `active_dry_run` comme une action ou une inférence live.
Sans bundle, le Core reste fail-closed et l’interface affiche explicitement
l’absence d’inférence live.

## API et WebSocket

- `GET /api/intelligence/topology` retourne la dernière topologie redacted, ou
  une topologie vide si aucun bundle n’a produit de trace.
- `GET /api/intelligence/traces` retourne au plus 24 traces redacted.
- `GET /api/intelligence/events` retourne au plus 64 événements récents
  `synora.recent-event/v1`, dérivés exclusivement des traces redacted.
- `GET /api/ws` diffuse `intelligence.inference` avec la trace MLP et son
  résumé d’événement borné ; le snapshot initial contient aussi les événements
  conservés.

La page affiche une section « Événements récents ». Elle rend un état vide
explicite lorsqu’aucun flux n’a été observé et ne fabrique aucune entrée.

La webapp est volontairement réduite à la navigation Intelligence V1. Le build
est produit dans `synora-web/dist`; aucun `node_modules`, secret, configuration
machine ou artefact généré n’est une source de transfert depuis l’ancien arbre.

## Prérequis pour du live

Une inférence observée nécessite le bundle CPU V1 chargé par Core, le Bus actif
avec le service `api` autorisé, et une instance `synora-api` exécutant le binaire
issu de ce checkout. `active_dry_run` reste le mode par défaut et aucune action
physique n’est autorisée par cette surface.
