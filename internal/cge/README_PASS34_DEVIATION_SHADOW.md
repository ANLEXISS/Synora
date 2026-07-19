# Passe 34 — déviation Shadow en lecture seule

La passe 34 branche l’évaluateur pur `deviation-v1` après l’extraction du
`LearningPlan` et avant son application durable. La baseline est lue par
`subject + kind` via les index du registre de routines. L’occurrence n’est
jamais évaluée après son apprentissage.

Le module est désactivé par défaut. Il est indépendant de l’apprentissage des
routines : l’évaluation peut fonctionner sans écriture durable, et
l’apprentissage peut fonctionner sans assessment. La politique, ses poids et
ses seuils restent centralisés dans `deviation.DefaultPolicy()`.

Les statuts et bandes sont descriptifs (`insufficient_history`, `partial`,
`ambiguous`, `aligned`, `low`, `moderate`, `high`). Un score est un indice
entier de déviation comportementale `[0,1000]`, jamais une probabilité, une
alarme ou un niveau de menace. Une bande, y compris `high`, ne modifie ni
l’association, ni evidence, ni une hypothèse, ni une routine.

Les assessments sont conservés uniquement dans un FIFO mémoire borné par
`RecentAssessmentLimit` (0 à 4096). Le store est défensif, concurrent et n’a
aucun WAL, snapshot générationnel ou replay. Après redémarrage, les routines
sont restaurées mais le store est vide. Les snapshots Shadow n’exposent que
des compteurs et la taille du store, jamais les identifiants ou le contexte
domestique.

La limite actuelle est de deux assessments par observation, avec au plus 64
routines candidates selon la politique. Une routine de plusieurs milliers
d’occurrences peut rendre la validation de snapshot et le calcul des
statistiques coûteux ; aucun cache global mutable n’est introduit. Les erreurs
de l’évaluateur sont isolées et n’empêchent pas l’apprentissage descriptif.

Variables de configuration :

* `SYNORA_CGE_SHADOW_DEVIATION_ENABLED`
* `SYNORA_CGE_SHADOW_DEVIATION_RECENT_LIMIT`
* `SYNORA_CGE_SHADOW_DEVIATION_MAX_ASSESSMENTS_PER_OBSERVATION`

Les prochaines étapes sont une campagne Shadow longue durée et une calibration
des distributions. Toute transformation en hypothèse, décision, sécurité ou
action est explicitement hors de cette passe.
