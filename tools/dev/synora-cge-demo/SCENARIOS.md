# Bibliothèque de scénarios du Live Lab

Les fichiers `scenarios/*.json` sont déclaratifs et versionnés (`cge-demo-scenario-v1`). Ils décrivent un état de départ, des événements, des avances d’horloge, des répétitions et des propriétés expérimentales. Les propriétés attendues sont comparées après l’exécution réelle du CGE ; elles ne sont jamais transmises au moteur.

Chaque exécution est locale, limitée à 100 étapes, 1 000 événements injectés et 365 jours simulés. `repeat_event` ne fabrique pas de routine : chaque événement passe par `LiveSession.Submit` et le pipeline Shadow réel. `same_id` sert à montrer l’idempotence.

Scénarios inclus :

| ID | Démonstration |
| --- | --- |
| `routine-formation` | épisodes, routine en formation, warm-up et readiness |
| `temporal-divergence` | divergence temporelle à sujet et lieu constants |
| `spatial-divergence` | divergence spatiale à horaire constant |
| `house-mode-divergence` | divergence contextuelle du mode du domicile |
| `interval-divergence` | divergence d’intervalle hors enveloppe historique |
| `combined-divergence` | décomposition de plusieurs facteurs réels |
| `partial-context` | couverture réduite et inconnues non traitées comme mismatches |
| `routine-shift` | adaptation progressive de la mémoire |
| `association-ambiguity` | hypothèses d’association et décision `ambiguous` |
| `unknown-new-subject` | chaîne séparée et historique insuffisant |
| `idempotent-retry` | retraitement sans doublon durable |
| `restart-replay` | checkpoint et replay exact |
| `memory-field-isolation` | matrice isolée des champs de mémoire |

`Memory field isolation` exécute contrôle, heure, espace, mode, occupation,
intervalle et contexte partiel dans des sessions séparées, toutes issues de la
même baseline déterministe. La matrice affiche les facteurs structurel,
temporel et d’intervalle, la couverture et le total issus de l’assessment réel.

Le résultat distingue toujours : observation injectée, mémoire comparable et
divergences mesurées. Une divergence positive n’est pas un niveau de menace.
Le panneau affiche en permanence : « Le CGE mesure une divergence avec sa
mémoire. Il n’en interprète pas encore la cause. »

Les hypothèses affichées sont des `Hypothèses d’association` : « À quelle
chaîne cette observation appartient-elle ? ». Une hypothèse de situation (« Que
signifie cette séquence d’événements ? ») n’est pas produite.

Le registre `Capacités actuelles` est accessible dans chaque scénario. Il
sépare association, routines, divergence explicable, apprentissage et replay
disponibles des hypothèse de situation, interprétation d’intention, causalité
complète et qualification automatique d’une menace non disponibles.

Le niveau futur pourra consommer plusieurs chaînes et routines, des
divergences successives, les contextes spatial et temporel, l’état du domicile,
l’identité, des relations causales et la persistance de la divergence. Cette
préparation est documentaire uniquement : aucun type, score, planner, stockage
durable ou mutation WAL de situation n’est ajouté.

Pour modifier un événement pendant une démonstration, utiliser le panneau d’édition de l’étape active. Le scénario passe alors en mode indicatif et les comparaisons concernées deviennent `inconclusive`. `previous-view` ne remonte jamais le moteur dans le temps ; un rejeu repart d’une session propre.

Les imports sont strictement déclaratifs : pas de script, chemin, HTML ou commande système. L’interface expose le catalogue et l’export uniquement sur le serveur local du démonstrateur.

Propriétés attendues autorisées : `association.decision`,
`association.ambiguous`, `hypothesis.association_opened`,
`deviation.status`, `deviation.score_positive`,
`deviation.structural_available`, `deviation.structural_positive`,
`deviation.temporal_available`, `deviation.temporal_positive`,
`deviation.interval_available`, `deviation.interval_positive`,
`deviation.coverage`, `routine.created`, `routine.occurrence_added`,
`routine.readiness`, `replay.digest_equal` et `wal.sequence_delta`.
Les propriétés de situation, d’intention, de menace ou de sécurité ne font pas
partie du schéma.
