# Architecture des données du CGE

## Statut et portée

Ce document et les catalogues de `configs/cge/contracts/` sont la source de
vérité contractuelle de l’architecture CGE à la date de la passe 65. Le
catalogue décrit le code existant ; il ne change aucun comportement du Core,
du StateStore, de l’admission, du workflow ou de la persistance.

Le catalogue couvre le chemin historique et le chemin Shadow :

```text
source externe
  -> Bus contract.Message
  -> ingest / contract.Event
  -> Core historique et décision historique
  -> frontière CGE scalaire
  -> observation normalisée et protégée
  -> contexte / chaînes / épisodes
  -> faits / hypothèses / évaluations
  -> situation cognitive
  -> recommandations advisory
  -> comparaison historique
  -> workflow WAL/checkpoint, journal, générations, ledger
  -> recovery et diagnostics read-only
```

La frontière historique conserve les identifiants nécessaires au moteur
historique. Les pseudonymes `cgeid-v1:<kind>:<hex>` ne reviennent jamais dans
le moteur historique, le StateStore ou les automations.

## Registre exécutable et gel v1 (passes 66–67)

Les catalogues YAML sont chargés strictement : en-têtes, types, clés inconnues,
doublons et documents multiples sont refusés. Le validateur impose les 17
catégories v1 exactes, les vocabulaires de confiance/sensibilité/autorité/
stabilité, ainsi qu'une version et une politique pour chaque contrat durable.

Le registre compilé est produit par `go run ./cmd/cge-contractgen generate`,
puis vérifié par `go run ./cmd/cge-contractgen check`. `check-compat` compare
le jeu canonique avec `baselines/cge-contract-set-v1.json` et `coverage` refuse
une couverture obligatoire incomplète. Le runtime utilise
`generated_registry.go` et ne lit jamais les YAML installés. `gosurface` surveille
les packages déclarés dans `go-surfaces.yaml`, inventorie chaque type exporté de
ces packages et les fixtures rendent rouges les dérives de champs, tags et
types. Les 10 writers durables sont catalogués dans `writers.yaml` et chacun
doit appeler `ValidateStoreWrite` avant sa première sérialisation ou écriture.

Chaque writer CGE durable déclare un StoreID et un ContractID et appelle
`ValidateStoreWrite` avant marshal, append, rename ou fsync. Cette garde ne
transforme aucune donnée : la frontière protège les identifiants et la garde
vérifie ensuite le domaine, l'autorité, la sensibilité et le store. Les anciens
records restent lisibles sans métadonnées générées.

La garde vérifie également le type Go racine pour les contrats `go_struct` et
utilise un validateur nommé pour les unions fermées partageant une enveloppe
wire legacy. Elle ne transforme jamais un payload invalide et aucun writer ne
doit créer de fichier temporaire avant son retour positif.

## Contrats

Les contrats sont dans `catalog.yaml`. Chaque contrat possède un ID versionné,
un propriétaire, des producteurs et consommateurs, un niveau de confiance,
une sensibilité, une autorité, un statut de stabilité et des champs décrits
par source, protection, persistance, rétention et validation.

Les contrats historiques `synora.contract.historical-decision.v1` et
`synora.contract.action-request.v1` sont inventoriés pour rendre la frontière
d’autorité vérifiable. Le second appartient à l’automation et ne peut pas être
produit par le CGE.

Les sorties cognitives actuelles sont descriptives ou advisory. Elles portent
les marqueurs `NotADecision`, `NotAnAction`, `NoSecurityMeaning` et
`HistoricalDecisionRetainsAuthority` selon leur modèle Go. Aucune sortie CGE
ne produit un `ActionRequest`.

## Temps et provenance

Les temps ne sont pas interchangeables :

| Temps | Sémantique |
|---|---|
| `observed_at` | moment déclaré par la source ou l’observation ; sert au raisonnement temporel |
| `produced_at` | moment où le producteur a créé le message, lorsqu’il est fourni |
| `received_at` | moment où le composant destinataire reçoit le message |
| `ingested_at` | moment où l’ingest l’accepte dans le Core |
| `processed_at` | moment où le composant CGE termine sa transformation |
| `committed_at` | moment où le workflow ou le ledger accepte l’écriture logique |
| `persisted_at` | moment où le store durable confirme son écriture selon sa politique fsync |

Le modèle actuel ne porte pas systématiquement les sept temps. Cette absence
est un gap catalogué dans `GAPS.md`, pas une permission de réutiliser un temps
à la place d’un autre. Les données cognitives doivent conserver la référence
logique à leur schéma source, producteur, observation, type d’événement,
confiance, timestamp, version de transformation et version de policy. Les
références sensibles sont protégées, les fingerprints restent vérifiables.

## Identité, ordre et déduplication

Les identifiants ont des sémantiques distinctes :

| Identifiant | Générateur / portée | Stabilité et usage |
|---|---|---|
| `event_id` | source ou Core, événement historique | déduplication et corrélation historique |
| `observation_id` | frontière CGE, dérivé protégé de `event_id` | corrélation durable Shadow, `cgeid-v1:observation` |
| `entity_id` | identité source, protégé à la frontière | sujet connu ; stable par valeur brute et domaine |
| `device_id` / `camera_id` | source, protégé comme device | corrélation de la même source sans révélation |
| `node_id` / `zone_id` | topologie Core | conservés pour la résolution topologique ; pas pseudonymisés dans cette passe |
| `clip_id`, `track_id`, `activation_id` | source | références multimédia protégées avant stockage |
| `clip_index` | index dans un clip | entier borné, non identifiant |
| `sequence_key` | source ou pipeline | ordre/corrélation protégé comme sequence |
| workflow/journal/ledger sequence | store propriétaire | ordre monotone local, non interchangeable |
| `revision` | agrégat ou comparaison | révision logique monotone |
| `digest` / `fingerprint` | composant propriétaire, SHA-256 ou fingerprint versionné | intégrité, idempotence et recovery ; pas un identifiant métier |

Après restart, le journal, WAL, checkpoint et ledger restaurent leurs
séquences, révisions et digests selon leurs contrats. Les anciennes données
valides restent rejouables ; aucune migration automatique n’est définie ici.

## Données et sensibilité

Les classes et contrôles de `catalog.yaml` interdisent les secrets et données
biométriques en clair dans les stores durables. Les identités, candidats,
devices, événements, clips, tracks, activations et sequence keys utilisent la
protection durable CGE par domaine. IP, tokens, images, vidéos, embeddings et
visages ne sont pas des données persistables CGE. La présence résidentielle est
réduite à un état contextuel borné ; la localisation est limitée aux références
node/zone nécessaires à la topologie.

Les logs, journaux, WAL et ledger ne doivent pas contenir de valeur brute. Les
anciens fichiers peuvent toutefois être sensibles : ils ne sont ni réécrits ni
supprimés automatiquement par cette architecture.

## Règle de changement obligatoire

> Aucune nouvelle entrée, sortie, frontière, persistance, métrique, RPC ou donnée cognitive ne peut être ajoutée sans mise à jour du catalogue et de ses tests.

Toute incohérence actuelle est un gap explicite. Une passe ultérieure doit
proposer la correction et sa migration séparément.

Toute nouvelle entrée, sortie, structure sérialisée, persistance, métrique,
RPC ou donnée cognitive doit mettre à jour le catalogue, la surface Go, le
registre généré et les tests avant d'être acceptée.

Le jeu v1 gelé contient les contrats, champs wire, implémentations Go,
frontières, stores, identifiants, temps, transports, writers et erreurs. Une
suppression, un renommage, un changement de type wire, un durcissement de
nullabilité, une réduction de protection, une hausse d'autorité ou un changement
de sémantique d'identifiant/temps est breaking et exige un nouvel ID versionné,
une migration documentée et une fixture de compatibilité. Une modification
compatible conserve l'ID, les champs requis, la protection et l'autorité ; elle
doit tout de même régénérer le registre et la baseline appropriée.
