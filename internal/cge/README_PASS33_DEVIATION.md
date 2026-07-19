# Passe 33 — évaluateur descriptif de déviation

La passe 33 ajoute `internal/cge/deviation`. Le package compare une
`routines.Occurrence` à une baseline de `routines.Snapshot` fournie par son
appelant. Il ne connaît ni le registre, ni le coordinateur durable, ni le WAL,
ni le ShadowEngine.

## Sens du score

Le score entier `[0,1000]` est un indice descriptif : `0` signifie aucune
déviation mesurée par rapport à la baseline et `1000` l’écart maximal selon la
politique. Ce score n’est ni une probabilité, ni une confiance, ni une alarme,
ni un niveau de menace.

La politique `synora.cge.deviation/deviation-v1` combine, lorsque disponibles,
les facteurs structurel, temporel et d’intervalle avec les poids `600/300/100`.
Les valeurs inconnues sont retirées du dénominateur ; elles ne deviennent pas
des divergences. La couverture sépare la disponibilité des facteurs de la
qualité du contexte.

## Baseline et déterminisme

La baseline doit être capturée avant l’apprentissage de l’occurrence. Après
son application à une routine, la même occurrence retourne
`already_evaluated` et aucun score n’est produit.

Les routines sont filtrées par sujet, type, statut autorisé et readiness
(occurrences, jours distincts, étendue temporelle). Les candidats sont bornés,
triés par score, couverture, support puis `RoutineID`. Un écart inférieur ou
égal à `AmbiguityMargin` entre les deux meilleurs candidats donne le statut
`ambiguous`, sans créer d’hypothèse.

La distance temporelle est circulaire sur la semaine. Le facteur d’intervalle
est uniquement disponible pour une occurrence postérieure à la dernière
observation et une routine disposant d’au moins deux intervalles. Les
observations tardives ne reçoivent pas de pénalité artificielle.

Chaque résultat conserve uniquement des références de routine, facteurs,
scores, couverture, raisons bornées et fingerprints. Aucun snapshot complet,
payload, nœud, zone ou donnée brute n’est retourné.

## Contextes partiels

Avec `AllowPartialContext`, les dimensions connues restent comparables et la
couverture diminue. Avec cette option désactivée, le résultat est
`not_applicable`. Aucune inconnue n’est transformée en mismatch.

## Limites et suite

Les assessments ne sont pas persistés et ne sont pas exécutés par le
ShadowEngine. Ils ne modifient ni routines, ni association, ni evidence, ni
hypothèses, ni lifecycle. Les futures passes pourront ajouter une intégration
en lecture seule, puis éventuellement une politique de déviation distincte.
