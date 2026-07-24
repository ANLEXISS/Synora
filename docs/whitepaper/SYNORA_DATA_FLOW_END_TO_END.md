# Synora — Cartographie complète du cycle de vie d’une donnée

## Périmètre et preuve

Cette passe est exclusivement documentaire. Aucun fichier de production, score,
règle, décision, API, format WAL ou configuration n’est modifié. Les références
ci-dessous associent les affirmations au code réel. Les points non démontrés
sont placés dans la section dédiée.

Trois chemins sont distincts :

| Chemin | Produit | Autorité |
| --- | --- | --- |
| Historique | état, décision, automation | décisionnelle |
| Shadow CGE historique | contexte, chaînes, routines, déviation | diagnostique, sans autorité |
| Shadow Cognitive Workflow | épisodes, facts, hypothèses, advisory, mapping, éligibilité | expérimental, sans décision ni action |

## Le parcours d’une observation Synora

Une personne apparaît devant une entrée. La caméra envoie un clip à Discovery
par POST /vision. La requête multipart contient clip et X-Synora-Device. Le
serveur borne la taille, calcule le SHA-256 du fichier, appelle
Authenticator.VerifyCameraRequest, écrit le clip dans le répertoire configuré
et crée un ClipJob. Preuve : internal/discovery/ingress/server.go, StartServer ;
internal/discovery/vision/job.go, ClipJob.

Le worker vision reçoit un chemin de clip et un identifiant de caméra sur un
socket Unix. Il renvoie des événements structurés. RunClipWorker enrichit le
payload avec caméra, clip et track puis publie un contract.Message JSON vers
le Core. Le Core reçoit donc les métadonnées de l’événement, et non le fichier
vidéo par ce chemin. Preuve : internal/discovery/vision/runtime.go, Request et
WorkerResponse ; internal/discovery/vision/worker.go, RunClipWorker.

Le bus local est une connexion Unix avec une ligne JSON par message. Client
décode les messages et les place dans un canal borné. Un JSON invalide est
journalisé et ignoré ; un canal plein peut perdre le message. runBusLoop
souscrit au canal core, distingue événement, RPC et commande, puis transmet
l’événement à l’ingestion. Preuve : internal/bus/client.go, readLoop et Send ;
cmd/synora-core/main.go, runBusLoop.

Parser.Parse produit un contract.Event. Il exige une source, normalise type,
ID, timestamp, priorité, périphérique, nœud, identité, confiance et clés de
continuité. Le payload historique reste une map générique. Preuve :
internal/ingest/ingest.go, Parser.Parse et Queue.Ingest.

processEvent touche l’état du périphérique, conserve l’événement et appelle le
moteur historique. engine.Analyze normalise, apprend dans la mémoire de graphe,
évalue les comportements, calcule le danger et construit un résultat.
ToSynoraDecision et BuildResult produisent contract.Decision et projections.
Preuve : cmd/synora-core/main.go, processEvent ; internal/engine/engine.go,
Analyze ; internal/engine/adapter/adapter.go, ToSynoraDecision et BuildResult.

La décision historique contient état, score et niveau de danger, raison,
références d’événement/nœud/track/clip, validation et champs d’action. Les
seuils génériques de levelForScore sont 0,18, 0,35, 0,55, 0,75 et 0,90 ; des
branches spécifiques existent pour vision.unknown, vision.weapon et
vision.fall. Preuve : pkg/contract/decision.go, Decision ; internal/engine/danger/danger.go,
ComputeDangerScore et levelForScore.

stateapply.Apply écrit les projections dans StateStore. automation.EvaluateRequests
applique règles, score effectif, conditions, schedule et cooldown ; une règle
applicable forme un ActionRequest. DispatchRequest l’envoie comme commande à
synora-actions. Le service Actions déduplique, route et exécute l’adaptateur ;
action.result revient vers le Core. Preuve : internal/stateapply/stateapply.go,
Apply ; internal/automation/engine.go, EvaluateRequests ; internal/automation/dispatch.go,
DispatchRequest ; internal/actions/service.go, HandleMessage.

En parallèle, observeCGE transmet une copie scalarisée à ShadowEngine.Observe.
EventFromContract ne copie pas le payload générique. Le Shadow peut construire
contexte, associations, chaînes, routines et déviation, mais son résultat ne
retourne pas dans contract.Decision. Preuve : cmd/synora-core/main.go, observeCGE ;
internal/cge/event_adapter.go, EventFromContract ; internal/cge/shadow_runtime.go,
observeRuntime.

Quand il est activé, submitWorkflow crée ObservationRef puis
ShadowWorkflowInput et appelle TrySubmit sans attendre. Le worker unique
construit épisode, facts, hypothèses, discrimination et advisory. Mapping et
authorization exigent des providers explicites. Coordinator.Commit planifie,
append le WAL, fsync selon policy, puis publie l’état. preferred est un
classement ; eligible est une évaluation de frontière ; aucun des deux n’est
une commande. Preuve : internal/cge/shadow_workflow_adapter.go, submitWorkflow ;
internal/cge/shadowworkflow/pipeline.go, process ; internal/cge/durableworkflow/coordinator.go,
Commit.

PublicSnapshotFromCoreState construit ensuite une projection défensive.
Les routes /api/state et /api/snapshot la renvoient ; /api/ws envoie snapshot
initial et mises à jour. Preuve : pkg/contract/public_snapshot.go,
PublicSnapshotFromCoreState ; cmd/synora-api/main.go, route registration ;
cmd/synora-api/ws.go, websocketHub.

## Taxonomie et classification

| Donnée | Classe | Origine | Consommateurs | Persistence | Autorité |
| --- | --- | --- | --- | --- | --- |
| frame, flux, clip | raw | caméra/ingress | vision/stream | clip configuré | aucune |
| détection vision | technical/contextual | worker | Core/engine/chaînes | event/state selon projection | contribue à l’historique |
| message bus | technical | producteurs | Core/Actions | non | transport |
| observation | contextual | Core/Shadow | Shadow/workflow | journal/WAL sous forme réduite | aucune |
| device, présence, résident, topologie | technical/contextual | registres/StateStore/config | engine/API/automation | StateStore ou config | contexte |
| chaîne, routine, déviation | cognitive | historique/Shadow | engine/diagnostic | StateStore/journal | critical seed : influence historique |
| episode, fact, hypothèse | cognitive | couches 38–40 | couches aval | workflow | aucune |
| advisory, mapping, eligibility | cognitive | couches 41–45 | workflow | WAL workflow | recommandation/éligibilité |
| décision | decision | Core historique | StateStore/automation/API | StateStore | oui |
| automation, action result | operational | Core/Actions | Actions/Core/API | StateStore/event | action éventuelle |
| PublicSnapshot | public | StateStore | API/WebSocket/webapp | mémoire | exposition |
| WAL, checkpoint, logs, rapports | diagnostic | runtimes | replay/audit | fichiers locaux | aucune |

## Transformations et minimisation

| Étape | Type | Producteur → consommateur | Ajout | Retrait |
| --- | --- | --- | --- | --- |
| clip → job | vision.ClipJob | ingress → vision | ID, caméra, chemin, temps | corps HTTP |
| worker → bus | contract.Message | vision → bus | type, source, références | flux traité |
| message → event | contract.Event | ingest → Core | ID/temps/priorité normalisés | enveloppe |
| event → decision | contract.Decision | engine → Core | état, danger, raisons | éléments non projetés |
| event → Shadow | cge.Event | Core → Shadow | champs scalaires | payload générique |
| Shadow → workflow | ObservationRef/Input | adapter → queue | révision/fingerprint | image, audio, embedding, biométrie brute, secret |
| episode → facts → hypotheses | DTO expérimentaux | couches cognitives | provenance, support, contradiction, coverage | événements bruts |
| state → PublicSnapshot | DTO public | Store → API/WS | agrégats | maps internes mutables |

Les images, clips, embeddings biométriques, credentials et grants complets ne
sont pas copiés dans le workflow cognitif durable. ClipState peut conserver une
référence de chemin historique ; ce n’est pas le média dans le WAL.

## Origine physique et ingress

| Élément | Statut |
| --- | --- |
| caméra et upload de clip | implémenté : POST /vision |
| TLS | implémenté si certificat et clé existent ; insecure seulement explicitement permis |
| limite/auth | MaxBytesReader, hash SHA-256, authenticator |
| clip | temporaire puis renommé en ID.mp4 sous le répertoire du device |
| worker | socket Unix vision worker |
| RTSP/WebRTC/HLS | descripteurs API/config ; pas une preuve de capture Core |
| audio, capteur générique, frame isolée | non déterminé dans ce parcours |

## Persistence, replay et fin de vie

| Donnée | Format/emplacement logique | Replay/checkpoint | Fin de vie observée |
| --- | --- | --- | --- |
| StateStore | JSON atomique, chemin configuré | load/migration | collections bornées ; suppressions ciblées |
| clip | fichier mp4, référence ClipState | pas de replay média | ExpiresAt existe ; suppression automatique non établie |
| chaînes | StateStore/manager | restauration | fermeture par inactivité/lifecycle |
| critical chains | YAML et backup | reload startup | remplacement/backup, pas de TTL démontré |
| Shadow journal | journal/générations | replay Shadow | policy du module |
| workflow | WAL NDJSON + checkpoint | replay checksum/lineage | compaction absente |
| qualification | samples/summary/manifest | reporter hors ligne | limite de sortie |
| PublicSnapshot | mémoire/API | reconstruit du Store | remplacé au changement |

FilePersistence.Save écrit un temporaire, le synchronise, le renomme puis
synchronise le répertoire. Une corruption JSON est renommée avec suffixe
corrupt timestamp. Cette politique diffère de la corruption fatale du WAL
durableworkflow. Les queues bus/Core/workflow ne sont pas durables : un élément
non committé peut être perdu lors d’un arrêt. Preuve :
internal/state/persistence.go, FilePersistence.Load et Save ;
internal/cge/durableworkflow/replay.go.

## Exposition et autorité

| Surface | Source | Données | Rôle |
| --- | --- | --- | --- |
| GET /api/state, /api/snapshot | Core State/Snapshot | PublicSnapshot | lecture |
| GET /api/events | StateStore/Core | événements projetés | lecture |
| GET /api/devices, residents, topology | registres/config | état et configuration | lecture/mutations protégées selon route |
| GET /api/streams | devices/network config | URL de flux | descripteur |
| GET /api/cge/* | diagnostics CGE | chaînes, danger, seeds, feedback | diagnostic |
| GET /api/system/health, version | runtime | santé/version | diagnostic |
| GET /api/ws | PublicSnapshot | initial/updates | lecture WebSocket |

| Composant | Produit | Lit | Modifie | Persiste | Décide | Autorise | Exécute | Expose |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vision/Discovery | oui | clip/config | queue/clip | clip | non | non | non | statut |
| Bus | non | messages | queues | non | non | non | non | non |
| Core historique | oui | event/state/config | state/decision | oui | oui | non | non direct | oui |
| StateStore | projections | oui | oui, owner | oui | non | non | non | snapshot |
| Shadow CGE | cognition | copie/état | Shadow state | journal | non | non | non | diagnostic |
| Facts/Hypotheses/Advisory | cognition | DTO | assessments | workflow | non | non | non | indirect |
| Mapping/Authorization | mapping/eligibility | inputs explicites | assessment | workflow | non | non | non | indirect |
| Durable Workflow | état/filiation | snapshots | état/WAL | WAL/checkpoint | non | non | non | status |
| Automation | requests | decision/config | cooldowns | selon Core | non | policy | non | indirect |
| Actions | results | commandes | adapters | event result | non | non | oui | result |
| API/Webapp | réponses | snapshot | non métier | non | non | non | non | oui |
| Qualification Recorder | samples | status/process | fichiers diagnostic | rapport | non | non | non | local |

## Chaînes critiques, scores et décision

SYNORA_CGE_CRITICAL_CHAINS est chargé par
cmd/synora-core/main.go, loadCGECriticalChains. LoadCriticalSeeds décode YAML
avec champs connus, valide les IDs, le risque, la séquence et le score minimal,
puis GraphMemory.ReplaceCriticalSeeds le conserve. Engine.applyCriticalSeedMatch
modifie réellement assessment historique, raisons, preuves, niveau et score.
Les seeds ne font pas un appel direct à actions ; une automation historique
peut agir ensuite. Le code ne démontre pas que leur contenu participe au
fingerprint global de configuration.

| Score | Signification | Probabilité | Décisionnel |
| --- | --- | ---: | ---: |
| vision Confidence | confiance déclarée du worker | non établi | oui pour certaines projections, seuil 0,50 |
| danger/effective score | évaluation historique et résultat après runtime | non | oui |
| chain score | évaluation de chaîne | non | oui si consommé par historique |
| deviation | écart à une routine Shadow | non | non dans Shadow |
| support/contradiction/coverage | soutien, conflit, couverture | non | non |
| plausibility | classement d’hypothèse | non | non |
| discrimination/utility | utilité potentielle d’une preuve | non | non |
| compatibility/quality | adéquation/qualité déclarées | non | non |
| eligibility | complétude de conditions externes | non | non |

## Durée de vie, erreurs et concurrence

Une donnée peut être non reçue, reçue puis rejetée, acceptée en mémoire puis
perdue dans une queue, traitée mais non durable, durable, ou durable mais non
exposée pendant une panne API. Queue pleine, timeout, provider absent, circuit
ouvert et erreur WAL du workflow n’affectent pas la décision historique.
Une corruption intermédiaire du WAL échoue la recovery expérimentale ; le Core
historique reste actif.

Le bus possède une goroutine de lecture et un canal borné ; le Core ses files et
son process loop ; le Shadow ses mutex ; le workflow une queue, un worker unique
et un coordinateur mono-writer. Les snapshots sont défensifs. Le commit durable
publie seulement après append/fsync. Le recorder de qualification démarre ses
goroutines uniquement activé. Deux writers de processus ne sont pas supportés.

## Ce qui est implémenté, expérimental ou inconnu

Implémenté : upload clip, worker vision, bus JSON local, Core historique,
StateStore JSON atomique, engine/chaînes/automation/Actions, Shadow historique,
workflow durable expérimental, API et WebSocket.

Expérimental ou synthétique : providers de mapping et authorization, pipeline
durable avec providers, qualification locale.

Non déterminé dans le code actuel : rétention effective et suppression des
clips, capture audio dans ce parcours, exactly-once depuis un périphérique,
compaction du WAL, présence d’un dispositif concret, stabilité Rock 5
multi-jour, effacement utilisateur complet.

## Glossaire

- Observation : représentation structurée d’un fait perçu, souvent sans média.
- Event : enveloppe normalisée transportée et traitée par le Core.
- Detection : résultat structuré de l’inférence vision.
- Context : informations temporelles, spatiales, topologiques ou de mode.
- Chain : regroupement temporel d’événements liés.
- Critical Chain : seed déclarée pouvant influencer l’évaluation historique.
- Routine : motif temporel/comportemental appris dans le Shadow.
- Deviation : écart descriptif à une routine.
- Episode : mémoire de travail bornée d’observations liées.
- Fact : énoncé neutre observé, dérivé ou carried.
- Situation Hypothesis : explication concurrente soutenue ou contredite.
- Evidence : information qui pourrait départager des hypothèses.
- Advisory Request : recommandation non exécutable.
- Capability : capacité abstraite déclarée par un domaine.
- Eligibility : résultat de frontière, pas une permission.
- Decision : état historique produit par le Core.
- Automation : règles pouvant former une demande d’action.
- Action : demande ou effet traité par Actions.
- PublicSnapshot : projection destinée à l’API/WebSocket.
- WAL : journal append-only rejouable.
- Checkpoint : image atomique d’un état workflow.
- Replay : reconstruction vérifiée depuis checkpoint et WAL.
- Shadow Mode : chemin séparé sans autorité historique.

## Encadrés livre blanc

**Local-first.** Le bus et le worker utilisent des sockets Unix. Les médias
peuvent rester dans le stockage configuré ; le workflow ne copie pas le média.

**Data minimization.** Le Shadow passe d’un événement à des champs scalaires puis
à une observation bornée. Le durable conserve statuts, références et
fingerprints, pas les payloads bruts.

**Decision authority.** La décision opérationnelle vient du Core historique.
Le Shadow évalue et recommande sans gouverner.

**Shadow cognition.** Le Shadow apprend des motifs, conserve des hypothèses et
peut recommander une preuve, sans altérer la décision historique.

**Durable memory.** Le workflow écrit la transaction avant de publier l’état ;
checkpoint et WAL sont rejoués et vérifiés au redémarrage.

**Separation of concerns.** Observation, décision, automation et effet physique
sont des frontières distinctes dans les types et les appels.

## Références techniques principales

cmd/synora-core/main.go — runBusLoop, processEvent, observeCGE  
internal/bus/client.go — readLoop, Send  
internal/ingest/ingest.go — Parser.Parse, Queue.Ingest  
internal/discovery/ingress/server.go — StartServer  
internal/discovery/vision/runtime.go — Request, WorkerResponse  
internal/discovery/vision/worker.go — RunClipWorker  
internal/engine/engine.go — Analyze, applyCriticalSeedMatch  
internal/engine/danger/danger.go — AssessEvent, ComputeDangerScore  
internal/state/persistence.go — FilePersistence.Load, Save  
pkg/contract/public_snapshot.go — PublicSnapshotFromCoreState  
internal/cge/shadow_runtime.go — observeRuntime  
internal/cge/shadow_workflow_adapter.go — submitWorkflow  
internal/cge/shadowworkflow/pipeline.go — process  
internal/cge/durableworkflow/coordinator.go — Commit  
internal/automation/engine.go — EvaluateRequests  
internal/automation/dispatch.go — DispatchRequest  
internal/actions/service.go — HandleMessage  
cmd/synora-api/main.go — route registration  
cmd/synora-api/ws.go — websocketHub.serve, broadcastSnapshot

## Conclusion en dix points

1. Une caméra peut envoyer un clip à l’ingress Discovery.
2. Discovery authentifie, borne et stocke le clip avant le job vision.
3. Le worker vision transforme le clip en événements structurés.
4. Le bus Unix transporte les événements JSON au Core.
5. Le Core valide, normalise, met à jour l’état et calcule la décision historique.
6. L’automation peut transformer une décision en demande d’action.
7. Actions exécute séparément l’adaptateur et renvoie un résultat.
8. Le Shadow reçoit une copie scalarisée sans modifier la décision historique.
9. Le workflow réduit cette copie en couches cognitives durables et bornées.
10. API et WebSocket exposent une projection publique ; rétention et effacement restent à qualifier quand le code ne les définit pas.
