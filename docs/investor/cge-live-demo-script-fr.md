# Script Live Lab CGE — français

Durée cible : cinq minutes. Les résultats ci-dessous sont des points
d’observation, pas des valeurs promises : dire « observons le résultat réel »
si la politique courante choisit une autre bande ou association.

## 0. Cadre — 20 secondes

À l’écran : `/`, session vide, horloge simulée, bannière
`Événements synthétiques — traitement réel du CGE`.

Dire : « Chaque clic envoie une observation synthétique au vrai ShadowEngine,
dans un répertoire temporaire local. Ce laboratoire n’a aucune action de
sécurité. »

## 1. Première observation — 40 secondes

Choisir `Entrée normale du résident`, vérifier `18:15`, puis `Envoyer au CGE`.

Observer la trace : observation, contexte, plan d’association, chaîne candidate,
evidence insuffisante et déviation `insufficient_history` avant apprentissage.

Dire : « Au démarrage, le moteur ne prétend pas connaître la routine. Il crée
une mémoire candidate et expose le manque d’historique. »

## 2. Construire la routine — 45 secondes

Cliquer `Répéter cette routine · 7` ou `Charger une base réelle · 7 jours`.
Chaque événement est traité individuellement, avec un timestamp à 24 heures.

Observer les occurrences, jours distincts, révisions, routine et journal.
Dire : « La routine est produite par les mutations réelles, pas injectée comme
un snapshot final. »

## 3. Comparer un écart — 50 secondes

Cliquer `Créer une déviation temporelle`. Le bouton détecte une routine mature,
prépare le jour suivant à `02:15`, conserve le sujet et le motif, passe en mode
`night`, puis s’arrête sans envoyer l’événement. Vérifier le formulaire avant
de cliquer `Envoyer au CGE`.

Lire la séparation `ÉVALUATION AVANT APPRENTISSAGE` dans la trace et le panneau
de déviation : bande, score, couverture, structure, temporalité et intervalle.
La trace doit montrer `Baseline lue`, `Déviation évaluée`, puis `Occurrence de
routine ajoutée`. Une valeur zéro alignée est explicitement une évaluation
effectuée sans écart mesuré ; elle ne signifie pas « pas de calcul ».

Dire : « Une déviation est une mesure explicable de différence avec l’historique,
pas une alarme et pas une probabilité de danger. L’occurrence est ensuite
apprise selon le comportement réel du moteur. »

## 4. Ambiguïté — 50 secondes

Avec une base ou au moins une observation existante, cliquer `Observation
ambiguë`, puis `Envoyer au CGE`. Si nécessaire, conserver le même horodatage
simulé que la dernière occurrence.

Observer les candidats, la marge, `ambiguous`, puis l’hypothèse ouverte.

Dire : « Le CGE sait dire “je ne sais pas encore”. Aucune alternative n’est
sélectionnée automatiquement. » Si le contexte est `missing` ou `partial`,
le dire explicitement : la dégradation vient du contexte fourni au moteur.

## 5. Replay — 35 secondes

Cliquer `Redémarrer le moteur` dans le panneau WAL.

Observer le digest avant/après et `equal: true`, ainsi que la reconstruction
des chaînes, hypothèses et routines. Mentionner que le store de déviation est
éphémère et vidé au redémarrage.

Dire : « La mémoire durable est versionnée, vérifiable et rejouable. »

## Limite à toujours mentionner

`synthetic_episode_not_separated` reste visible. La mécanique cognitive est
fonctionnelle et qualifiée ; la calibration comportementale doit être validée
sur des domiciles et capteurs réels. Les décisions de sécurité restent sous le
contrôle du moteur historique.

## Secours

Si la session est dans un état inattendu : `Réinitialiser la session`, puis
`Charger une base réelle · 7 jours`. Si le batch est trop long, cliquer
`Annuler le batch`. Pour les détails d’ingénierie, basculer `Mode technique`.
