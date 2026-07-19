# Questions investisseurs

**Pourquoi est-ce différent d’un système à règles ?** Les règles associent
une condition à une réaction. Le CGE construit une mémoire contextualisée du
foyer, conserve l’incertitude et mesure les écarts par rapport à l’historique.

**Pourquoi ne pas utiliser uniquement un LLM ?** Le CGE fournit un état
durable, borné, versionné et rejouable. Un modèle génératif peut compléter une
interface d’explication, mais ne remplace pas ces invariants transactionnels.

**Quel est le moat technologique ?** La combinaison du graphe contextuel, des
hypothèses concurrentes, des routines locales, de la déviation pré-apprentissage,
de l’explicabilité et du WAL rejouable.

**Comment gérez-vous les faux écarts ?** Une déviation est un index explicable,
pas une alarme. Le moteur peut continuer à apprendre un changement légitime ;
la calibration réelle reste à mesurer.

**Que se passe-t-il lorsque les habitudes changent ?** Les nouvelles
occurrences enrichissent progressivement les distributions et les routines.
La vitesse présentée est volontairement rapide et expérimentale.

**Pourquoi conserver plusieurs hypothèses ?** Une décision forcée détruit une
information utile. Les alternatives restent disponibles jusqu’à l’arrivée de
preuves supplémentaires.

**Comment garantissez-vous la confidentialité ?** Le chemin démontré est local,
le journal est local et la télémétrie terrain est pseudonymisée. Aucun secret,
nom, adresse, image ou embedding ne figure dans le scénario.

**Que se passe-t-il en cas de redémarrage ?** Le journal durable est rejoué et
le digest est comparé avant/après. Le store récent de déviation est éphémère.

**Le système peut-il fonctionner sans cloud ?** Oui pour le fonctionnement CGE
démontré ; cela ne signifie pas qu’aucune communication n’existera jamais dans
tout l’écosystème.

**Le CGE décide-t-il déjà d’une intrusion ?** Non. Il est en Shadow Mode,
sans action, automation ni autorité sur le moteur historique de sécurité.

**Comment allez-vous valider le terrain ?** Par un déploiement Shadow isolé,
avec données pseudonymisées, mesures de contexte, suivi des changements
légitimes et séparation explicite des épisodes synthétiques.

**Comment le moteur peut-il évoluer vers plusieurs produits ?** Les registres,
le contexte, les routines et la durabilité forment une capacité transversale ;
les politiques et interfaces de chaque produit restent des frontières séparées.

**Qu’est-ce qui est brevetable ?** Cette question nécessite une analyse de
propriété intellectuelle dédiée ; le démonstrateur ne formule aucune promesse
juridique.

**Quelle partie est déjà fonctionnelle ?** Le cycle cognitif, les chaînes,
hypothèses, routines, déviation, replay, isolation et instrumentation sont
exécutables et testés dans le dépôt.

**Quelle partie reste expérimentale ?** Calibration sur domiciles réels,
capteurs physiques, adaptation optimale, croissance sur plusieurs mois et
valeur opérationnelle pour la sécurité.
