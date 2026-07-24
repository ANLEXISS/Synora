# Passe 57 — Calibration Analytics and Policy Evaluation

`internal/cge/calibrationanalytics` fournit une analyse déterministe,
descriptive et read-only des `CalibrationRecord`, `Snapshot` et
`CalibrationSummary` du ledger. Le package ne lit pas le NDJSON, ne connaît pas
le chemin du fichier et ne consomme aucune donnée du Core, du StateStore, des
événements bruts ou des actions.

## Garanties

L’analyse produit des résultats globaux, par catégorie, par cohorte de
fingerprints de policies, par fenêtres séquentielles, ainsi que des tendances,
variations statistiques, suffisances de données et comparaisons de cohortes.
Les fenêtres sont bornées par nombre de records, non par horloge. La moitié
initiale des fenêtres est la baseline et la moitié finale la période récente;
ce choix est déterministe. Lorsque le nombre de fenêtres dépasse la limite,
les fenêtres les plus récentes sont conservées.

Le drift signifie uniquement une variation statistique au-delà des seuils
analytics. Il ne signifie ni faute, ni danger, ni compromission, ni erreur du
CGE. Une cohorte de policy n'est jamais appelée meilleure, gagnante, sûre,
exacte ou prête pour la production. La cohorte de référence est seulement la
cohorte éligible la plus ancienne, avec le fingerprint comme départage neutre.

Les histogrammes et percentiles sont calculés par le helper read-only du
ledger, afin de conserver exactement la sémantique permille existante.

## Policy par défaut

Les valeurs par défaut sont 100 records, 50 records comparables, 50 records
par cohorte, 3 fenêtres minimales, fenêtres de 100 records, au plus 100
fenêtres, 32 cohortes et 32 catégories. Le drift exige 100 records par moitié,
avec seuils 100 permille pour la moyenne, 150 pour P95 et 100 pour les taux.

## Shadow Workflow

L'analytics est désactivé par défaut et dépend du ledger activé et disponible.
Il n'y a pas de worker périodique : après un append réussi, un recalcul est
déclenché tous les 100 records par défaut; `RecomputeCalibrationAnalytics` peut
le forcer à la demande. Le rapport est remplacé atomiquement sous verrou et
cloné à chaque lecture. Une erreur conserve le dernier rapport valide et
dégrade uniquement le status analytics.

Variables prises en charge :

- `SYNORA_CGE_CALIBRATION_ANALYTICS_ENABLED` : `false` ;
- `SYNORA_CGE_CALIBRATION_ANALYTICS_MIN_RECORDS` : `100` ;
- `SYNORA_CGE_CALIBRATION_ANALYTICS_MIN_COMPARABLE` : `50` ;
- `SYNORA_CGE_CALIBRATION_ANALYTICS_WINDOW_SIZE` : `100` ;
- `SYNORA_CGE_CALIBRATION_ANALYTICS_MAX_WINDOWS` : `100`.

Les APIs sont internes au runtime Shadow. Aucun endpoint HTTP, WebSocket ou
CLI de production n'a été ajouté : la CLI existante produit un rapport de
qualification différent et un nouvel outil aurait couplé artificiellement
l'analytics au format physique du ledger.

## Limites et non-autorité

The ledger records comparisons; it does not calibrate automatically.
The analytics records comparisons; it does not calibrate automatically.
Alignment does not prove correctness.
Divergence does not prove an error.
No threshold or weight is changed by the ledger.
No threshold or weight is changed by analytics.
Historical production authority remains unchanged.

Les rapports ne constituent ni accuracy modèle, ni ground truth, ni décision de
production, ni recommandation, ni autorisation, ni commande, ni action, ni
alerte. Aucun record individuel ni fingerprint source n'est sérialisé dans le
rapport; seuls les fingerprints de policies et d'artefacts analytics sont
conservés.

## Status, métriques et qualification

Le runtime expose seulement `CalibrationAnalyticsStatus()` et un rapport cloné
par `CalibrationAnalyticsReport()`. `RecomputeCalibrationAnalytics()` est une
opération interne read-only. Le status distingue `Enabled`, `Available` et
`Degraded`, conserve les compteurs de rapports/échecs et ne contient ni chemin
de ledger ni donnée utilisateur. Une erreur analytics conserve le dernier
rapport valide et n'arrête pas le Shadow Workflow.

Les métriques sont des compteurs ou projections agrégées : disponibilité,
rapports, échecs, insuffisance, durée de recompute, volumes, fenêtres, cohortes,
moyennes permille, taux permille et présence d'un drift descriptif. Aucun label
par record, comparaison ou fingerprint n'est utilisé. Les samples de
qualification ne recopient que ces mêmes projections compactes.

`calibrationanalytics.Readiness()` marque toutes les capacités descriptives,
la redaction, les bornes, l'isolation historique, l'isolation d'erreur et la
concurrence comme implémentées dans le périmètre logiciel. La valeur
`ReadyForControlledPolicyExperiments` est vraie pour l'analyse contrôlée; les
flags d'autorité restent faux : aucune calibration automatique, mise à jour de
seuil/poids, sélection/déploiement de policy, feedback de production,
override, commande, action ou autorité de sécurité n'existe.

## Validation et prochaines étapes

Les entrées sont clonées, triées par séquence et validées contre le modèle du
ledger. Les catégories, cohortes, fenêtres, séquences, permille, fingerprints
et marqueurs sont contrôlés; les limites de sortie sont 100 fenêtres, 32
cohortes et 32 catégories par défaut. Il n'existe pas de CLI dédiée dans cette
passe : la CLI de qualification existante traite un autre format, et ouvrir le
ledger physique depuis une nouvelle commande aurait élargi inutilement le
couplage. Une éventuelle étape suivante est une qualification contrôlée de
policies, avec décision humaine séparée et sans branchement automatique de
l'analytics vers le CGE.
