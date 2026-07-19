# Passe 32 — durabilité des routines

La passe 32 ajoute les routines au journal cognitif global, sans créer de
journal parallèle. Les records `routine.created`, `routine.occurrence_added`
et `routine.status_changed` partagent la séquence, le `PreviousHash`, le
`Sync` et la vérification bornée de tête des chaînes et des hypothèses.

## Replay et checkpoints

Le coordinateur reconstruit trois registres avant de devenir publiable :

1. les chaînes depuis le journal seul, ou depuis une génération puis ses
   deltas post-checkpoint ;
2. les hypothèses depuis le journal complet ;
3. les routines depuis le journal complet.

Le format des générations de chaînes n’est pas modifié. Ainsi, les records de
routines antérieurs à un checkpoint sont volontairement rejoués même lorsque
la chaîne démarre depuis ce checkpoint. Les trois replays doivent constater
la même tête globale.

Le replay d’une occurrence reconstruit la mutation dans le domaine, vérifie
la révision locale, l’outcome statistique et le fingerprint complet du
snapshot. Aucun registre partiel n’est publié en cas d’erreur.

## Publication transactionnelle

Une occurrence clone uniquement la routine ciblée. Le candidat est validé,
encodé, appendé et synchronisé avant publication. Une création contient le
snapshot initial; les occurrences ultérieures ne répètent pas l’historique
complet mais journalisent l’occurrence, la révision et les statistiques
dérivées.

Une interruption peut laisser une présence durable sans transition durable.
Cet état est valide. Le retraitement produit les mêmes identifiants et ajoute
seulement ce qui manque.

## Shadow Mode

`ShadowRoutineConfig.Enabled` est désactivé par défaut. Lorsqu’il est activé,
le flux reste descriptif : association, evidence et hypothèses sont exécutées
comme avant, puis le plan de routines est appliqué par occurrence. Le contexte
partiel est configurable. Une topologie absente permet l’apprentissage de
présence et produit un skip borné pour les transitions.

`already_attached` relit la chaîne exacte lorsque l’orchestrateur la connaît et
relance le plan. Une association ambiguë ne rattache aucune observation et
n’apprend aucune routine. Aucune routine n’est consultée pour modifier
l’association ou l’evidence.

Les statuts de routine sont exclusivement explicites via
`SetRoutineStatus`. Aucun scheduler, retry persistant, goroutine permanente,
score de normalité/anomalie, hypothèse, résolution, lifecycle de chaîne,
décision de sécurité, action ou automation n’est produit.

## Limites

Les occurrences restent conservées intégralement en mémoire et dans le WAL;
le coût mémoire est linéaire avec leur nombre. Les routines ne sont pas
encore incluses dans les générations, et la topologie runtime complète reste
optionnelle. L’évaluation de déviation et toute politique de lifecycle sont
reportées à une passe ultérieure.
