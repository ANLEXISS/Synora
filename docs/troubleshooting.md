# Troubleshooting runtime

## Discovery redémarre

Vérifier d'abord `/api/system/health`, puis `/api/runtime/diagnostics`. Un
`vision_worker: unavailable` avec `model file is missing` signifie que le
worker est vivant mais que sa capability modèle est désactivée. Ce n'est pas
une raison pour redémarrer Discovery.

Un `network: degraded` avec hostapd en erreur indique un mode réseau dégradé.
Les autres sources d'événements restent utilisables ; corriger hostapd et
relancer le service selon la procédure d'exploitation lorsque le réseau local
est nécessaire.

## Trop d'événements worker

Les événements `discovery.worker.started/crashed` sont des diagnostics. Ils ne
créent pas d'intrusion. Un redémarrage répété est coalescé en
`runtime.component.flapping`; consulter le statut runtime et les logs du
worker plutôt que le CGE.

## Aucune action

Consulter `blocked_actions` dans l'évaluation et `blocked_actions_recent` dans
les diagnostics. `no_matching_automation` signifie qu'aucune règle active ne
correspond au type, niveau, nœud ou conditions. `action_service_unavailable`
indique un échec de publication vers Actions. `simulated_input` est attendu
pour les tests et ne doit jamais déclencher une action réelle.

## Tester sans polluer le réel

Utiliser `POST /api/cge/manual-risk` avec `test:true`, une raison explicite et
une durée courte. Le signal est visible dans le CGE mais reste simulé/dry-run.
Les événements bruts et les historiques ne sont jamais supprimés par un reset
d'état.
