# Passe 36 — instrumentation d’essai Shadow local

La passe 36 ajoute `internal/cge/fieldtrial`, un recorder local optionnel et
expurgé. Il est placé après l’orchestration Shadow complète : l’assessment de
déviation est enregistré avant l’application du plan de routines, avec le
résultat d’apprentissage observé ensuite. Les assessments ne sont jamais
écrits dans le WAL cognitif et ne participent ni au replay, ni à une décision.

## Stockage et confidentialité

Chaque session possède un manifeste atomiquement remplacé, des segments
`events-XXXXXX.ndjson` et un fichier d’annotations séparé. Les segments ont
leur propre séquence et chaîne HMAC indépendante du journal CGE. Une ligne
terminale partielle est réparable uniquement avec l’option explicite de
récupération; toute divergence historique met la session en `degraded`.

Les références sont des HMAC-SHA-256 tronquées, propres à la session. La clé
reste en local avec les permissions `0600`; elle n’est jamais exportée. Les
événements ne contiennent ni payload brut, image, embedding, biométrie,
adresse, ni identifiant cognitif en clair. Les annotations sont append-only,
expérimentales et invisibles au CGE.

Le mode est désactivé par défaut. Le root, le quota, la rétention et la clé
sont configurables par `SYNORA_CGE_FIELD_TRIAL_*`. `SyncEachEvent` permet de
choisir entre durabilité par événement et durabilité aux rotations,
checkpoints et fermeture. Aucune goroutine ni ticker n’est créé.

## Récupération, rotation et export

La rotation termine et synchronise le segment courant avant d’en ouvrir un
nouveau. `Checkpoint` synchronise les fichiers et le manifeste sans produire
de snapshot cognitif. `Close` est idempotent. Une session interrompue peut
être reprise explicitement; son store d’assessments Shadow reste, lui,
éphémère et vide après redémarrage.

La rétention ne touche que les sessions fermées et reste dans le root
configuré. `verify` ne répare pas par défaut. `export` produit un répertoire
NDJSON vérifiable, un rapport et les SHA-256 des fichiers, sans clé ni chemin
local absolu.

## Topologie et protocole opératoire

`SYNORA_CGE_FIELD_TRIAL_TOPOLOGY_FILE` peut fournir au démarrage un snapshot
JSON détaché validé. Il n’y a pas de hot reload ni de topologie inventée; en
cas d’erreur le provider partiel reste utilisable.

Un protocole indicatif pour un essai de plusieurs semaines est :

1. semaine 0 : installation, vérification, session désactivée puis ouverture
   explicite et `verify`;
2. semaine 1 : fonctionnement ordinaire sans scénario provoqué;
3. semaines 2–3 : annotations des variations légitimes, sans les transmettre
   au moteur;
4. semaine 4 : scénarios contrôlés non dangereux, uniquement selon le cadre
   opératoire local;
5. fin : `checkpoint`, `close`, `verify`, `report`, puis `export` avant toute
   modification de politique.

Le recorder est secondaire : erreur, quota, permissions, disque plein ou
panic sont comptés et isolés. Ils ne dégradent ni le coordinateur, ni le
moteur historique et ne déclenchent aucun retry cognitif.

CLI de développement (les commandes de préparation hors service sont ajoutées
en passe 37) :

```text
synora-cge-trial start|status|checkpoint|close|sessions|verify|annotate|report|export|prune
synora-cge-trial topology validate|inspect
synora-cge-trial key generate
synora-cge-trial preflight|prepare|doctor|smoke-check|export-verify
```

La passe 37 ajoute également l’empreinte de configuration cognitive au
manifeste. Une reprise avec une empreinte différente est refusée : les
données existantes restent conservées et une nouvelle session doit être
préparée. Cette vérification ne compare pas les chemins de stockage aux
politiques cognitives.

Les annotations et les diagnostics de campagne sont des données de vérité
expérimentale externes. Ils ne modifient aucune politique, association,
evidence, hypothèse, routine, lifecycle, décision historique, action ou
automation.
