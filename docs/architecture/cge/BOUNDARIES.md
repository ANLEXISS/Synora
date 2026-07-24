# Frontières canoniques du CGE

Les détails machine-readable sont dans `configs/cge/contracts/boundaries.yaml`.
Les noms de contrats sont résolus par `catalog.yaml`, les stores par
`stores.yaml`, les erreurs par `errors.yaml` et les writers par `writers.yaml`.

| ID | Producteur → consommateur | Transformation / validation | Persistance | Autorité |
|---|---|---|---|---|
| B01 | External Source → Bus | adaptation, taille et contrat | aucune | descriptive |
| B02 | Bus → Ingested Event | décodage et normalisation | Core State/EventStore | historique |
| B03 | Ingested Event → Historical Core | règles et état historiques | Core State/EventStore | authorized_decision historique |
| B04 | Historical Core → CGE Boundary Event | copie scalaire sans payload | aucune | descriptive |
| B05 | Boundary Event → Observation | allowlist, validation, `ProtectRaw` | stores CGE en aval | descriptive |
| B06 | Observation → Context Frame | topologie, temps local, mode et occupation | journal/checkpoint selon modèle | descriptive |
| B07 | Observation + Context → Cognitive Model | agrégation bornée en faits | WAL/checkpoint | descriptive |
| B08 | Cognitive Model → Hypothesis Set | alternatives et provenance | journal/WAL | advisory |
| B09 | Hypothesis + Evidence → Evaluation | support/contradiction/neutre borné | journal/WAL | advisory |
| B10 | Evaluation → Cognitive Situation | consolidation et marqueurs | WAL/checkpoint | advisory |
| B11 | Situation → Recommendation Set | candidats advisory bornés | WAL/checkpoint | advisory |
| B12 | Historical Decision + Cognitive Output → Comparison | comparaison sans override | WAL/ledger | advisory |
| B13 | Workflow Commit → Durable Stores | WAL, checkpoint, journal, ledger | stores CGE | advisory |
| B14 | Durable Stores → Recovery | validation puis replay | aucune nouvelle écriture logique | diagnostic |
| B15 | Runtime → Diagnostics | détachement de compteurs/status | mémoire | diagnostic |
| B16 | Diagnostics → RPC/API | projection read-only et redaction | aucune | diagnostic |
| B17 | Recommendation → Authority Boundary | rejet d’action/commande, conservation des marqueurs | aucune | advisory |
| B18 | Feedback/Validation → Calibration | record de calibration et analytics descriptives | ledger | advisory |

Pour chaque frontière, le YAML conserve aussi le type exact d’entrée et de
sortie, les erreurs, les effets de bord et les validations. Une frontière
historique peut avoir une autorité historique ; cette autorité ne traverse pas
B04/B05 sous forme d’autorité CGE. B17 est une barrière explicite : le CGE ne
peut produire ni `ActionRequest`, ni commande, ni automation.

## Enforcement

B04 conserve le contrat historique et ne mute pas `contract.Event`. B05
construit l'observation détachée, applique `durableids.ProtectRaw`, puis passe
par `contractcatalog.ValidateOutput` avant l'admission. B12 protège les
références historiques et vérifie le contrat de comparaison. Les writers de
B13 appellent `ValidateStoreWrite` avant toute sérialisation durable. Un refus
ne crée donc ni record partiel ni append parasite.

Le journal possède également une union v1 fermée dans `journal-kinds.yaml` :
chaque `RecordKind` accepté est relié à un type payload Go et à un validateur.
Les records legacy restent lisibles, mais aucun kind nouveau ne peut être
écrit sans mise à jour du registre.
