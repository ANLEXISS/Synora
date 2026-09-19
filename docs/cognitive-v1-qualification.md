# Qualification Cognitive Core V1

`make qualify-cognitive-v1` produit
`build/qualification/cognitive-v1-report.json` et des journaux par gate.

La qualification couvre le corpus disjoint, le bundle CPU V1 chargé par Core,
les scénarios hermétiques, le cycle Discovery–Core–Store–Discovery, le replay
réel Vision RKNN de `/home/rock/test3.mp4`, le redémarrage et le replay du
Store, la compaction, la surface Web Discovery, les modèles absent/incompatible,
la saturation et le benchmark avant/après.

La gate finale exige notamment : zéro fuite de split, métriques indépendantes
au-dessus du seuil défini par le pipeline, modèle Vision réel chargé, replay du
Store identique, sortie fonctionnelle du benchmark identique, mode
`active_dry_run` et `physical_action_executed: false`.
