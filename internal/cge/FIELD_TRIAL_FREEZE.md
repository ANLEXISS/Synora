# Cognitive freeze — première campagne physique

Versions gelées :

```text
context-v1
routine-extraction-v1
deviation-v1
```

La configuration de référence conserve `association-v1` et `evidence-v1`,
les réévaluations Shadow bornées à 8, au plus 64 candidats deviation, deux
assessments par observation, les buckets routines de 15 minutes et les bandes
deviation-v1 : aligned ≤200, low ≤400, moderate ≤700, high >700. Les seuils
de readiness sont `3` occurrences, `2` jours locaux distincts et `6h` de span;
les statuts éligibles sont `candidate`, `active`, `declining` et `dormant`.

Les valeurs et l’empreinte réellement utilisées doivent être enregistrées par
`preflight` et `prepare`. **Aucune modification de politique ne sera réalisée
pendant la campagne physique initiale.** Une mauvaise séparation, un taux
élevé de déviation bénigne ou une adaptation rapide restent des résultats
expérimentaux.

Seules les corrections de crash, corruption, fuite de données, perte de
télémétrie, récupération ou régression historique pourront être considérées,
avec nouvelle revue et nouvelle empreinte avant reprise.
