# Passe 47 — Shadow Cognitive Workflow Integration

Cette passe ajoute une intégration expérimentale et optionnelle entre les
événements déjà normalisés du Shadow CGE et les couches cognitives durables.
Elle est désactivée par défaut, asynchrone, bornée et sans autorité.

```text
Historical Shadow CGE
        │
        ├── historical decisions unchanged
        │
        └── non-blocking redacted copy
                       ↓
                bounded queue
                       ↓
                single worker
                       ↓
        experimental cognitive pipeline
                       ↓
             durable transaction
                       ↓
              WAL + checkpoint
                       ↓
          status / metrics / recovery
```

## Frontière historique

Le moteur historique adapte et traite son événement comme avant. Après cette
adaptation, une copie limitée est soumise à `shadowworkflow.Runtime` par
`TrySubmit`. Son résultat est volontairement ignoré par la décision historique.
Le package `shadowworkflow` n’importe pas le package parent `cge`, ce qui évite
un cycle ; le câblage reste dans l’adaptateur Shadow.

La copie contient seulement un `EventID`, des timestamps, un
`episodes.ObservationRef` et des références de révision/fingerprint Shadow.
Elle ne contient ni payload original, média, embedding, biométrie brute,
secret, token ou décision de sécurité.

## Activation et queue

La configuration `Workflow` est ajoutée à `ShadowConfig` avec le flag
`SYNORA_CGE_SHADOW_WORKFLOW_ENABLED`. La valeur par défaut est `false`. Quand
le flag est désactivé, le runtime ne crée ni store, ni queue, ni worker.

`TrySubmit` ne fait jamais d’attente de traitement : la queue est bornée et la
saturation applique `drop_newest` via le statut `queue_full`. Les entrées
acceptées ne sont pas durables avant le commit. La queue n’est pas persistée au
redémarrage.

Cette passe utilise un worker et un coordinateur durable mono-writer. Le
shutdown annule les traitements en cours et peut abandonner les entrées encore
en queue ; cette limite est explicite.

## Pipeline et atomicité

La profondeur par défaut est `advisory_requests` :

```text
episode → facts → hypotheses → discrimination → advisory requests
```

Les profondeurs `capability_mapping` et `authorization_boundary` sont
disponibles mais nécessitent des providers explicitement injectés. Aucun
inventaire, grant ou policy n’est fabriqué si le provider est absent.

Pour chaque entrée acceptée, le runtime construit une vue candidate des épisodes
avec `episodes.PlanIngest`, extrait les facts, évalue les hypothèses, analyse la
discrimination puis planifie les requêtes consultatives. Toutes les couches
activées sont assemblées dans un seul `WorkflowMutation`. Aucun registre
intermédiaire n’est publié.

Le commit suit le planner pur de `durableworkflow`, puis le WAL et enfin la
publication de l’état. Les doublons `EventID` sont comptabilisés sans nouvelle
révision logique.

## Providers

`CapabilityInputProvider` et `AuthorizationInputProvider` sont des interfaces
abstraites. Ils exposent uniquement des snapshots explicites et peuvent
retourner `available=false`. Aucun provider matériel n’est fourni. Il n’existe
aucune association caméra, microphone, réseau ou autre dispositif concret.

## Quotas, timeout et circuit breaker

Les quotas bornent la queue, l’âge des entrées, le nombre d’épisodes, les
requêtes, mappings, assessments, la durée d’un cycle et la taille du WAL.
Chaque cycle est exécuté sous `context.WithTimeout`. Une erreur avant commit
conserve l’état durable précédent.

Le circuit possède les états `closed`, `open` et `half_open`. Les échecs
consécutifs ouvrent le circuit ; une seule entrée de test est admise en
`half_open`. Une erreur de recovery ne devient pas un retry silencieux : le
runtime passe en `recovery_failed` et le Shadow historique reste actif.

Les panics sont capturés dans le worker, convertis en erreur diagnostique et
comptabilisés. Ils ne sortent pas vers le moteur historique.

## Store et recovery

Le mode `memory` est utilisé par défaut. Le mode `file` utilise exclusivement
`durableworkflow.FileStore` dans un répertoire explicitement fourni, avec les
permissions de la policy durable. Le WAL historique du Shadow n’est pas
réutilisé ni modifié.

Au démarrage fichier, genesis, checkpoint et WAL sont rejoués avant le worker.
Une recovery invalide produit `recovery_failed` ; aucun état partiel n’est
accepté. Le runtime programme les checkpoints selon le nombre de transactions
ou l’intervalle fourni. Une erreur de checkpoint dégrade le status mais ne
retire pas les transactions déjà durables.

Lorsque la taille du WAL atteint `MaxWALBytes`, le runtime refuse de nouvelles
entrées et expose `storage_limit_reached`. Il ne tronque ni ne compacte le WAL.

## Status, métriques et logs

`StatusSnapshot` expose uniquement les compteurs, révision, sequence, digest,
état du circuit, profondeurs de couches et erreurs codées. Les métriques par
stage couvrent épisode, facts, hypothèses, discrimination, advisory, mapping,
autorisation, queue, transactions, checkpoints et panics.

Les logs ne contiennent que des codes, révisions, sequences, compteurs et
fingerprints abrégés si nécessaire. Aucun payload, grant, identité ou scope
sensible n’est logué.

## Limites et garanties

Le workflow ne fournit pas une livraison exactement-once depuis le bus source :
une entrée en queue au moment d’un crash peut être perdue. Il fournit
l’idempotence des événements retraités et l’atomicité des transactions
commitées.

Le pipeline reste mono-worker. Il n’y a ni endpoint public, ni intégration
Shadow décisionnelle, ni invocation, réservation, observation active, action,
automation ou autorité de sécurité.

```text
Shadow workflow failure must never block historical Shadow CGE.
Accepted queue entries are not durable until committed.
No downstream cognitive result has production authority.
No capability is invoked.
No authorization eligibility becomes an execution grant.
```

La prochaine étape possible est **Physical Shadow Qualification**.
