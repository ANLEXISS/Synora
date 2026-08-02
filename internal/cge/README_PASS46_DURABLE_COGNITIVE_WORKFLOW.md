# Passe 46 — Durable Cognitive Workflow

Cette couche fournit une durabilité expérimentale et isolée pour les sept
couches cognitives. Elle est déterministe, mono-writer par processus, dérivée
et non intégrée au runtime de production.

```text
Experimental cognitive layers
          ↓
Cross-layer lineage validation
          ↓
WorkflowTransaction
  ├── source revision/digest
  ├── typed mutations
  ├── resulting revision/digest
  └── checksum
          ↓
Append WAL + fsync
          ↓
Publish immutable state
          ↓
Checkpoint atomique
          ↓
Replay déterministe
```

## Graphe et filiation

```text
episode → situation_facts → situation_hypotheses → evidence_discrimination
        → advisory_requests → capability_mapping → authorization_boundary
```

Le graphe est statique et fingerprinté. Chaque couche descendante conserve les
fingerprints de ses sources. Une mutation amont rend ses descendants `stale`,
sauf lorsqu’ils sont remplacés dans la même transaction avec une filiation
exacte. La fraîcheur du workflow ne modifie pas les statuts métier des objets.

## État et transactions

`EpisodeWorkflowState` contient uniquement les snapshots publics et les
résultats compacts nécessaires à la filiation. Il ne contient ni événements
bruts, ni média, ni secrets, ni tokens, ni commandes, ni inventaire, policy set
ou grant snapshot complets. Les frontières publiques renvoient des clones
défensifs.

`PlanTransaction` est pur : il ne lit ni horloge ni fichier. Les timestamps et
les identifiants de transaction sont fournis par l’appelant. Une transaction
complète remplace plusieurs couches dans un seul état résultant.

## WAL et replay

Le `FileStore` utilise un journal NDJSON versionné : chaque ligne contient une
version, une sequence, un kind, un payload JSON, son fingerprint et un checksum
SHA-256. Les kinds sont `genesis`, `transaction` et `checkpoint_marker`. Une
transaction complète est un seul record logique.

Le commit suit strictement : verrouillage, vérification de la revision et du
digest, recalcul temporaire, vérification du résultat, append WAL, `fsync`, puis
publication de l’état. Les séquences, fingerprints, checksums et TransactionID
sont vérifiés au replay. Une troncature finale peut être ignorée selon la
policy ; une corruption intermédiaire, un gap, une collision ou un checksum
incorrect sont fatals. Les doublons strictement identiques sont idempotents.

## Checkpoints et recovery

Un checkpoint est sérialisé dans un fichier temporaire du même répertoire,
chmodé, synchronisé, renommé atomiquement puis suivi d’un `fsync` du
répertoire. Le WAL n’est jamais supprimé ni compacté dans cette passe. Un
checkpoint corrompu peut être signalé et le replay retombe explicitement sur
le WAL complet lorsque celui-ci est disponible ; il ne démarre jamais avec un
état partiel.

Les permissions par défaut sont `0700` pour le répertoire et `0600` pour les
fichiers. Le store est mono-writer au niveau du processus ; aucune coordination
distribuée n’est fournie.

## Invariants et limites

Les états sont canoniques et fingerprintés. Aucun état n’est publié avant
l’append durable. Une source obsolète produit un conflit de revision ou de
digest. Les snapshots sont défensifs et l’état est en mémoire dans le
coordinateur.

`Durable` ne signifie pas autorisé. Une éligibilité rejouée ne devient pas une
permission. Une couche descendante `stale` ne doit jamais être traitée comme
actuelle. Aucun format de WAL de production n’est modifié.

Cette passe ne fournit ni intégration Shadow, ni persistence de production, ni
compaction/rotation/migration, ni invocation de capacité, ni autorité, ni
token, ni réservation, ni commande, ni action ou automation. La prochaine
étape possible est **Shadow Workflow Integration**.
