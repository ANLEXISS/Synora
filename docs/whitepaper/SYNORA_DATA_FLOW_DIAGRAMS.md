# Synora — Diagrammes du flux de données

Ces diagrammes sont une vue de livre blanc fondée sur le code audité. Ils ne
constituent pas une nouvelle architecture. Les flèches pleines représentent un
appel ou un transport observé ; les flèches pointillées représentent une
frontière d’isolation, une dépendance optionnelle ou une consommation
diagnostique. Les cylindres représentent une persistence. Les blocs violets
sont Shadow/expérimentaux ; les blocs orange sont historiques/décisionnels.

## Diagramme 1 — Vue grand public

~~~mermaid
flowchart LR
  A[Caméra / périphérique] --> B[Clip ou signal]
  B --> C[Traitement vision local]
  C --> D[Événement structuré]
  D --> E[Core Synora]
  E --> F[Contexte et état]
  F --> G[Décision historique]
  G --> H[Interface utilisateur]
  G --> I[Automation]
  I --> J[Action éventuelle]
~~~

Lecture : le média est transformé en événement avant le traitement Core dans le
chemin vision audité. L’action est séparée de la décision et dépend de règles
d’automation.

## Diagramme 2 — Vue architecture Synora

~~~mermaid
flowchart LR
  CAM[Camera / Peripheral] --> ING[Discovery ingress]
  ING --> CLIP[(Clip storage)]
  ING --> VW[Vision worker]
  VW --> BUS[Unix JSON bus]
  BUS --> CORE[Core]
  CORE --> STATE[(StateStore)]
  CORE --> HIST[Historical Engine]
  CORE -. scalar copy .-> SHADOW[Shadow CGE]
  SHADOW -. non-blocking .-> WF[Shadow Workflow]
  CORE --> AUTO[Automation]
  AUTO --> ACTBUS[Action command bus]
  ACTBUS --> ACTIONS[Actions service]
  ACTIONS --> EFFECT[Physical adapter]
  STATE --> API[API / WebSocket]
~~~

Le workflow Shadow et le moteur historique sont deux consommateurs séparés.
Aucune flèche de WF vers HIST, AUTO ou ACTIONS n’existe dans le code.

## Diagramme 3 — Cycle de vie d’une donnée

~~~mermaid
flowchart TD
  RAW[Raw clip / signal] --> DET[Vision detection]
  DET --> EVT[contract.Event]
  EVT --> DEC[contract.Decision]
  EVT --> OBS[cge.Event]
  OBS --> EP[Episode observation]
  EP --> FACT[FactSet]
  FACT --> HYP[Competing hypotheses]
  HYP --> ADV[Advisory request]
  ADV --> MAP[Capability mapping optional]
  MAP --> ELIG[Authorization eligibility optional]
  DEC --> PUB[PublicSnapshot]
  ELIG --> DUR[(Durable workflow WAL)]
  PUB --> UI[API / Webapp]
~~~

Les branches cognition et décision historique ne se rejoignent pas. Les couches
optionnelles ne sont activées qu’avec des providers explicites.

## Diagramme 4 — Données et persistence

~~~mermaid
flowchart LR
  MEDIA[Clip file] --> CLIPSTORE[(Configured clip storage)]
  CORE[Core projections] --> STATE[(StateStore JSON)]
  CHAINS[Historical chains] --> STATE
  SHADOW[Shadow CGE] --> JOURNAL[(Shadow journal)]
  WORKFLOW[Durable workflow state] --> WAL[(WAL NDJSON)]
  WAL --> CP[(Atomic checkpoint)]
  QUAL[Qualification recorder] --> SAMPLES[(Local samples / report)]
  STATE --> SNAP[PublicSnapshot]
  SNAP -. rebuilt .-> API[API / WebSocket]
~~~

Le StateStore, le journal Shadow, le WAL durable et les samples de qualification
sont des familles distinctes. Le WAL durable n’est pas le journal historique et
n’est pas le WAL de production.

## Diagramme 5 — Frontières d’autorité

~~~mermaid
flowchart LR
  OBSERVE[Observe] --> LEARN[Learn / retain]
  LEARN --> RECOMMEND[Recommend]
  RECOMMEND -. Shadow boundary .-> DECIDE[Historical Core decides]
  DECIDE --> AUTHOR[Automation policy evaluates]
  AUTHOR --> EXEC[Actions service executes]
  EXEC --> EFFECT[Physical effect]
  MAP[Capability mapping] -. descriptive .-> RECOMMEND
  ELIG[Authorization eligibility] -. not a grant .-> AUTHOR
~~~

La cognition Shadow observe, apprend et recommande. Le Core historique décide.
L’automation forme une demande selon ses propres règles. Actions effectue
l’exécution séparée. Mapping compatible, candidat préféré ou éligibilité ne
franchissent pas directement cette frontière.

## Diagramme 6 — Séquence complète

~~~mermaid
sequenceDiagram
  participant Device as Camera
  participant Discovery as Discovery
  participant Vision as Vision worker
  participant Bus as Unix bus
  participant Core as synora-core
  participant Hist as Historical Engine
  participant Shadow as Shadow CGE
  participant Queue as Workflow queue
  participant Layers as Cognitive layers
  participant Durable as Durable Coordinator
  participant Store as StateStore
  participant Auto as Automation
  participant Actions as Actions service
  participant API as API/WebSocket

  Device->>Discovery: multipart clip + device header
  Discovery->>Discovery: size limit + hash + auth + file rename
  Discovery->>Vision: ClipJob(path, camera)
  Vision->>Bus: structured event JSON
  Bus->>Core: contract.Message
  Core->>Core: Parse + validate + normalize
  Core->>Hist: Analyze(event, state)
  Hist-->>Core: Result / Decision
  Core->>Store: apply projections
  Core->>Auto: EvaluateRequests(decision)
  Auto->>Actions: command message, if rule matches
  Actions-->>Bus: action.result
  Core-->>API: snapshot/state publication
  Core->>Shadow: scalar observation copy
  Shadow->>Queue: TrySubmit, non-blocking
  Queue->>Layers: single worker
  Layers->>Durable: mutation / transaction
  Durable->>Durable: append WAL + fsync
  Durable-->>Layers: publish after durable append
  API->>Store: read PublicSnapshot
~~~

Les appels Core historiques jusqu’à StateStore sont synchrones dans
processEvent. TrySubmit est non bloquant ; le pipeline et le commit sont
asynchrones. Une erreur Shadow est isolée et ne devient pas une erreur de
traitement historique.

## Recommandations de mise en page

- Diagramme 1 : pleine largeur, six à dix blocs, pour l’introduction.
- Diagramme 2 : architecture en colonnes Acquisition, Core, Shadow, Action,
  Exposition ; utiliser un zoom secondaire pour les sous-packages.
- Diagramme 3 : lecture verticale dans la section minimisation et cognition.
- Diagramme 4 : placer les stockages en gris sous les traitements qui les
  alimentent ; rappeler que le clip n’est pas le WAL cognitif.
- Diagramme 5 : utiliser une ligne verticale épaisse pour la frontière
  Historical Core ; mettre Shadow en violet et Actions en rouge.
- Diagramme 6 : réserver au chapitre technique ; annoter les flèches
  non-bloquantes et les points de persistence.
- Bleu : acquisition et transport ; vert : état et contexte ; violet :
  cognition Shadow ; orange : décision historique ; rouge : action physique ;
  gris : persistence et diagnostics.
- Flèche pleine : appel/transport prouvé. Flèche pointillée : isolation,
  option ou dépendance non décisionnelle. Cylindre : fichier ou état durable.
- Les providers capability/authorization sont à dessiner comme des blocs
  optionnels uniquement dans un zoom expérimental ; aucun dispositif concret
  n’est déduit par ces diagrammes.

## Références

cmd/synora-core/main.go — runBusLoop, processEvent, observeCGE  
internal/discovery/ingress/server.go — StartServer  
internal/discovery/vision/worker.go — RunClipWorker  
internal/bus/client.go — readLoop, Send  
internal/engine/engine.go — Analyze  
internal/cge/shadow_runtime.go — observeRuntime  
internal/cge/shadow_workflow_adapter.go — submitWorkflow  
internal/cge/shadowworkflow/pipeline.go — process  
internal/cge/durableworkflow/coordinator.go — Commit  
pkg/contract/public_snapshot.go — PublicSnapshotFromCoreState  
internal/automation/dispatch.go — DispatchRequest  
internal/actions/service.go — HandleMessage
