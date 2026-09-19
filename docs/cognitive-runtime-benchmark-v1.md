# Benchmark runtime cognitif V1

`make perf-baseline-v1` sépare désormais deux mesures :

1. le replay réel de `/home/rock/test3.mp4`, qui mesure décodage, détecteur RKNN, tracking, segments, observations et résumés ;
2. `synora-cognitive-runtime-bench`, qui mesure uniquement le runtime Go déjà construit : chargement froid du bundle CPU, initialisation Core, première décision, steady-state, quantiles des quatre heads, RSS, commit Store et dispatch d’action simulé.

La compilation du binaire et la construction/export du bundle ne sont jamais incluses dans ces temps. Le rapport JSON contient `vision_rknn` et `runtime_benchmark` séparément, le mode `active_dry_run`, et `physical_action_executed: false`.

Les lectures RSS renvoient zéro seulement lorsque `/proc/self/status` n’est pas disponible. Les files et les scénarios du benchmark sont bornés ; le replay conserve au plus trois frames en vol et la sortie fonctionnelle doit rester identique.

Rollback : supprimer la cible et le binaire de benchmark, puis restaurer l’ancien script de collecte. Le replay Vision et le runtime Core ne dépendent pas de ce benchmark.
