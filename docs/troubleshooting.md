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

## Flask absent

Flask sert uniquement l'API debug locale du Vision Worker. S'il n'est pas
installé, le worker reste actif et expose `debug_http.status=unavailable` dans
ses capabilities. Installer les dépendances vision si cette API est requise ;
ce manque ne doit plus provoquer de redémarrage.

## Certificat de l'ingress vision absent

Sans `/etc/synora/certs/server.crt` ou `server.key`, l'ingress est marqué
`disabled` avec la raison `tls_cert_missing` et Discovery reste vivant. Pour un
environnement local explicitement autorisé, définir
`SYNORA_ALLOW_INSECURE_INGRESS=true` démarre l'ingress en HTTP dégradé.
Sinon générer les fichiers avec `make generate-discovery-cert` lors de la
préparation de la machine cible.

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

Le retour contient un `event_id`. Après le délai demandé, le risque manuel
repasse à `idle/none` et sa chaîne est fermée avec la raison
`manual_risk_expired`.
