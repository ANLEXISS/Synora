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

## Danger courant et décroissance temporelle

Le score exposé dans `SystemState.danger_score` est recalculé par
`DangerRuntime` toutes les cinq secondes à partir des chaînes récentes et
actives. La configuration par défaut est dans `configs/cge_profile.yaml` :
fenêtre de 30 minutes et demi-vie de 10 minutes. Une chaîne ancienne conserve
son historique mais ne contribue plus au danger courant après la fenêtre.

Le risque manuel reste une contribution fixe jusqu'à son expiration. Une
intrusion ou une chaîne critique non résolue reste verrouillée lorsque
`lock_intrusion_until_reset` est actif ; le reset explicite reste la règle de
sortie. Les champs `danger_decay`, `danger_score_current`,
`danger_score_peak`, `danger_score_updated_at` et `danger_reasons_current`
sont disponibles dans l'état public et les diagnostics runtime.

## Contrôles admin

`POST /api/system/state/reset` remet l'état à `idle`, danger `none`, crée un
événement d'audit et conserve les historiques. `POST /api/intrusion/reset` est
le raccourci historique. `POST /api/cge/manual-risk` injecte un signal manuel ;
avec `test:true`, le signal est explicitement simulé et dry-run.
