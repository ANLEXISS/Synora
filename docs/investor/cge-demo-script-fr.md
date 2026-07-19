# Script présentateur — 8 à 10 minutes

Chaque chapitre est un arrêt possible. Les contrôles et les notes techniques
sont séparés de la phrase affichée ; `Voir la preuve` ouvre l’entrée de
`cge-claims.json` associée.

## 1 — Le problème · 45 s

À l’écran : comparaison entre une règle illustrative et le CGE.
Phrase : « Un événement n’a pas le même sens dans tous les domiciles. »
Transition : « Regardons ce que le moteur ajoute avant toute interprétation. »
Technique : rappeler que la comparaison à règles est conceptuelle, pas un
benchmark concurrentiel. Preuve : `local-first`, `routine-learning`.
Limite : aucune décision de sécurité n’est produite.

## 2 — Contexte · 50 s

À l’écran : événement → frame → observation et plan synthétique.
Phrase : « Le CGE prend en compte où, quand et dans quel état cela s’est
produit. » Technique : montrer le nœud, la zone et la qualité de contexte.
Preuve : snapshots `context-v1`. Limite : le domicile est synthétique.

## 3 — Mémoire · 70 s

À l’écran : timeline et routines qui apparaissent après warm-up.
Phrase : « Le modèle n’est pas générique : il se construit pour ce foyer. »
Technique : montrer jours distincts, bins temporels, transitions et révisions.
Preuve : `routine-learning`. Limite : la calibration terrain reste ouverte.

## 4 — Ambiguïté · 55 s

À l’écran : deux branches et l’indication aucune alternative sélectionnée.
Phrase : « Le CGE sait dire je ne sais pas encore. » Technique : l’événement
est soumis au vrai planner et l’hypothèse est durablement ouverte. Preuve :
`ambiguity-preservation`. Limite : pas de résolution automatique.

## 5 — Déviation · 75 s

À l’écran : routine historique, occurrence nocturne et trois facteurs.
Phrase : « Une déviation n’est pas une alarme ; c’est une mesure explicable. »
Technique : score 0–1000, couverture et facteurs provenant de `deviation-v1`.
Preuve : `pre-learning-deviation`. Limite : un épisode synthétique n’est pas
une mesure de sécurité.

## 6 — Adaptation · 60 s

À l’écran : déplacement de la distribution temporelle.
Phrase : « Le moteur ne fige pas la maison dans son passé. » Technique : une
occurrence atypique peut être apprise, puis la déviation diminue. Preuve :
`continuous-adaptation`. Limite : adaptation volontairement rapide à calibrer.

## 7 — Replay · 55 s

À l’écran : digest avant/après redémarrage.
Phrase : « La mémoire cognitive est versionnée, vérifiable et rejouable. »
Technique : expliquer le WAL global et le store de déviation éphémère. Preuve :
`deterministic-replay`. Limite : l’action système est volontairement absente.

## 8 — Local-first · 45 s

À l’écran : capteurs → centrale → CGE local.
Phrase : « Aucun cloud n’est requis pour l’apprentissage démontré. » Technique :
calcul, stockage, journal et export pseudonymisé. Preuve : `local-first`.
Limite : ce message ne prétend pas décrire toutes les communications de
l’écosystème Synora.

## 9 — Différenciation · 45 s

À l’écran : les dix briques et leurs preuves.
Phrase : « Le moat est une combinaison transactionnelle et contextualisée,
pas un écran de chat. » Technique : citer hypothèses, replay, routines,
déviation et Shadow Mode. Limite : ne pas parler d’infaillibilité.

## 10 — État réel · 50 s

À l’écran : prouvé / à prouver et le warning `synthetic_episode_not_separated`.
Phrase : « La mécanique est fonctionnelle et qualifiée ; la calibration
comportementale est le prochain travail terrain. » Technique : l’autorité de
sécurité reste `future`. Transition finale : « Le prochain jalon est un Shadow
Mode physique, isolé et mesurable. »
