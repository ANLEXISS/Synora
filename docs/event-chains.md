# Event Chains

Synora suit le flux `Camera -> Discovery -> Vision Worker -> Bus Unix -> Core -> CGE -> StateStore -> API/WebSocket`. Discovery publie chaque résultat Vision dès qu’il est disponible ; il ne retient pas les clips pour attendre une chaîne complète et ne décide ni du danger ni de l’état de la maison.

Le Core classe et agrège les events dans `internal/event.ChainManager`. Un event significatif rattache ou crée une chaîne ouverte, puis déclenche immédiatement une analyse CGE et une `ChainEvaluation`. Une chaîne garde toujours sa dernière évaluation connue.

## Significant et contextual

Les events significatifs par défaut comprennent `vision.identity`, `vision.unknown`, `vision.uncertain`, `vision.weapon`, `vision.fall`, `vision.fight`, `vision.tamper`, les pertes caméra et les ouvertures de porte/fenêtre. Les events contextuels comprennent notamment `vision.motion`, heartbeat, clips et frames.

`vision.motion` peut être attaché à une chaîne ouverte proche. Il incrémente les compteurs et met à jour `last_event_at`, mais ne met jamais à jour `last_significant_event_at`, ne crée pas de chaîne par défaut et ne produit pas de `ChainEvaluation`.

Une chaîne se ferme après 30 secondes sans event significatif, avec `closed_reason=significant_inactivity_timeout`. Les events significatifs ultérieurs peuvent maintenir une chaîne ouverte indéfiniment : aucun `max_duration` n’est appliqué.

Les valeurs par défaut peuvent être ajustées par environnement :

- `SYNORA_CGE_SIGNIFICANT_INACTIVITY_TIMEOUT`
- `SYNORA_CGE_CONTEXTUAL_COALESCE_WINDOW`
- `SYNORA_CGE_RECENT_EVENTS_LIMIT`
- `SYNORA_CGE_EVALUATIONS_LIMIT`
- `SYNORA_CGE_MOTION_EXTENDS_CHAIN`
- `SYNORA_CGE_MOTION_CREATES_CHAIN`

Les événements récents et évaluations sont bornés. Les événements contextuels répétitifs sont coalescés, tandis que le résumé roulant, les compteurs, la dernière évaluation et le niveau de danger maximal sont conservés.

## Mémoire critique

Une chaîne est critique dès qu’elle atteint `high`/`critical`, `intrusion`/`break-in`, ou contient weapon, fall, fight ou tamper. Le Core fusionne les motifs similaires par séquence significative, nœud et classe de danger dans `CriticalChainMemory`, avec occurrences, trajectoires d’état/danger et chaînes représentatives.

## API et WebSocket

- `GET /api/events/chains?status=open|closed|all&limit=50&since=...&severity=...&simulated=true|false`
- `GET /api/events/chains/{id}`
- `GET /api/cge/critical-chains`
- `GET /api/cge/critical-chains/{id}`

`/api/state` contient uniquement un résumé léger `event_chains`. Les détails restent dans les endpoints chains.

Le WebSocket `/api/ws` relaie `event.chain.created`, `event.chain.updated`, `event.chain.closed` et `engine.evaluation.updated`. Ces messages contiennent le résumé et les compteurs, pas la liste complète des events ; les détails sont récupérés via l’endpoint de chaîne.
