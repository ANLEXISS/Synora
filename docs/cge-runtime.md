# CGE runtime

Le chemin runtime attendu est :

`événement réel significatif → chaîne → évaluation → état/danger → automation → action.request → Actions → action.result`.

Les événements de sécurité (`vision.unknown`, `vision.weapon`, `vision.fall`,
tamper, offline caméra critique, présence et accès) peuvent créer ou mettre à
jour une chaîne. Motion et heartbeats restent contextuels ; les événements de
supervision Discovery sont ignorés par l'agrégateur de chaînes par défaut.

Les chaînes portent séparément les compteurs réels et simulés. Une simulation
peut alimenter Live et un dry-run, mais ne devient pas une action réelle et ne
doit pas être confondue avec une mémoire critique réelle.

## Décisions et actions

Une évaluation expose ses actions recommandées et, lorsque rien n'est
exécuté, `action_decision`/`blocked_actions`. Les causes courantes sont
`no_matching_automation`, `simulated_input` et
`action_service_unavailable`. Les événements simulés conservent leurs
métadonnées `simulated`/`dry_run` jusqu'au service Actions.

Le profil de sécurité continue d'influencer score, seuils et zones sensibles.
Il ne transforme jamais un test simulé en entrée réelle.

## Contrôles admin

`POST /api/system/state/reset` remet l'état à `idle`, danger `none`, crée un
événement d'audit et conserve les historiques. `POST /api/intrusion/reset` est
le raccourci historique. `POST /api/cge/manual-risk` injecte un signal manuel ;
avec `test:true`, le signal est explicitement simulé et dry-run.
