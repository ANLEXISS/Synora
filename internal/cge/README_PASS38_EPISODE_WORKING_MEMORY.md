# Passe 38 — Episode Working Memory

## Objectif et frontière

Cette passe ajoute `internal/cge/episodes`, un domaine expérimental et
déterministe de mémoire de travail en mémoire. Il regroupe des références
d’observations liées dans une séquence spatio-temporelle bornée; il ne leur
attribue pas de signification.

La frontière est la suivante :

```text
Event
  ↓
Context
  ↓
Chain association
  ↓
Routine/deviation
  ↓
Episode Working Memory
  ↓
[future] Situation Facts
  ↓
[future] Situation Hypotheses
```

Une chaîne est une mémoire cognitive d’association, une routine est un motif
contextuel appris, un épisode est une séquence bornée d’observations, et une
situation sera une interprétation ultérieure. L’épisode ne crée ni hypothèse
de situation, ni score de menace, ni intention, ni décision de sécurité.

## Modèle

`Episode` contient un `EpisodeID` opaque, un statut, les ancres temporelles,
des `ObservationRef`, des sujets, nœuds, chaînes, routines et agrégats
déterministes. Les références sont expurgées : aucun payload vidéo, image,
embedding, biométrie, événement brut, adresse réelle ou donnée de sécurité.

`ObservationRef` contient l’EventID, les temps observé/reçu, le type, un sujet
typé, le nœud/zone, le contexte domestique, les continuités activation/clip/
track/séquence, la chaîne, les routines et une référence descriptive minimale
de déviation. Le résultat de déviation est conservé comme référence; il n’est
pas recalculé.

Les sujets `known`, `unknown`, `uncertain` et `none` sont distincts. Les
identités connues différentes sont une contradiction forte. Un sujet inconnu
ne correspond pas arbitrairement à un résident; une incertitude peut produire
plusieurs candidats. Les listes de candidats sont triées et dédupliquées.

## Statuts et politique

Le cycle est `open → quiescent → closed` avec `invalidated` terminal. Les
réouvertures `quiescent → open` sont autorisées quand une observation compatible
est effectivement appliquée. Les transitions identiques, les sorties de
`closed` et `invalidated`, et les transitions non listées sont refusées.

Les valeurs par défaut sont conservatrices et expérimentales, non calibrées
sur le terrain :

| Valeur | Défaut |
| --- | ---: |
| même track | 45 s |
| même activation | 3 min |
| même sujet connu | 2 min |
| sujet inconnu | 30 s |
| durée maximale | 10 min |
| quiescence | 30 s |
| fermeture après quiescence | 2 min |
| grâce d’observation tardive | 30 s |
| score minimal | 650 / 1000 |
| marge minimale | 100 / 1000 |
| candidats évalués | 20 |
| observations par épisode | 128 |

`Policy.Fingerprint` est canonique et préfixé par
`episode-policy-v1:`. Il inclut toutes les durées en nanosecondes et toutes
les limites; il ne rejoint pas le fingerprint cognitif de production.

## Planner, scoring et topologie

`PlanIngest` est pur : il ne prend pas l’horloge, ne modifie pas de registre,
n’écrit pas de journal, ne lance aucune action et ne dépend pas du hasard.
Pour des entrées identiques, il produit le même plan. Les décisions sont
`attach_existing`, `create_episode`, `ambiguous`, `duplicate` et `rejected`.
Une ambiguïté est un résultat descriptif et ne crée pas d’hypothèse.

Le score est borné à `[0,1000]`. La base est 300; les deltas explicables sont :

| Facteur | Delta |
| --- | ---: |
| `track.same` | +400 |
| `activation.same` | +300 |
| `sequence.same` | +120 |
| `clip.related` | +50 |
| fenêtre track | +100 |
| fenêtre activation | +70 |
| fenêtre sujet | +40 |
| sujet connu identique | +250 |
| sujet inconnu compatible | +80 |
| sujets incertains recouvrants | +180 |
| même nœud / zone | +90 / +50 |
| topologie atteignable | +60 |
| chaîne identique / routine partagée | +40 / +20 |
| contexte domestique identique | +10 par champ |

Les identités connues différentes, les fenêtres dépassées, une durée maximale
dépassée et une topologie explicitement inaccessible sont des hard rejects. Un
contexte absent ou partiel est indisponible, jamais un mismatch. La déviation
ne participe pas au rattachement : une déviation élevée ne rompt donc pas à
elle seule une continuité technique forte. Une routine partagée n’est pas
suffisante à elle seule.

`TopologyView` est optionnelle. `ContextTopology` adapte le modèle détaché de
`internal/cge/context`; l’absence de topologie produit `space.unknown`. Une
topologie explicitement inaccessible peut rendre le candidat inéligible.

## Identifiants et registre

`DeriveEpisodeID` calcule `episode-` suivi d’un SHA-256 de la policy, du
premier EventID, de l’ObservedAt canonique et d’un fingerprint de sujet. Aucun
secret ni élément personnel lisible n’est inclus; une collision est signalée.

`Registry` est le propriétaire unique des épisodes. Il sérialise les accès,
retourne des deep clones, trie `List`, maintient l’index EventID et applique
les plans sous révision optimiste. Une mutation est préparée sur un clone,
validée, puis publiée sous verrou; les erreurs ne laissent pas de mutation
partielle. Deux applications concurrentes d’un même plan ne peuvent pas toutes
deux réussir.

Les doublons sont idempotents : aucune révision, observation, métrique d’état
ou digest ne change. Une ambiguïté ou un rejet ne crée ni épisode ni hypothèse.

Les observations sont triées par `ObservedAt`, puis `EventID`. Les observations
hors ordre acceptées conservent `StartedAt = min` et
`LastObservedAt = max`. Une observation tardive dans la grâce est insérée puis
retriée; une observation antérieure hors grâce est refusée. Un épisode fermé
ne se rouvre pas dans le flux normal.

## Snapshots, lifecycle et fingerprints

`Snapshot` est immutable par convention et défensif aux frontières : les
épisodes, observations et maps sont copiés. Le planner n’a donc pas besoin du
verrou du registre. `EpisodeFingerprint` et `RegistryDigest` utilisent des
représentations canoniques ordonnées; l’ordre d’insertion des maps n’influence
pas le résultat.

`EvaluateLifecycle` est pur et daté explicitement. Il propose `open →
quiescent` après 30 secondes d’inactivité, puis `quiescent → closed` après
2 minutes. `ApplyLifecycleBatch` vérifie la révision globale et les révisions
locales, rejette les lots incohérents et publie le lot sous verrou.

Les agrégats conservés sont le nombre d’observations, les ancres et la durée,
les sujets/nœuds/zones distincts, chaînes, routines, types d’événements et
qualités de contexte observées. Aucune probabilité, causalité, intention,
menace, intrusion ou confiance de sécurité n’est calculée.

## Bornes, erreurs et adaptation

Les limites de politique couvrent les durées, candidats et observations; la
taille des références textuelles est bornée à 256 runes. Les erreurs du
domaine sont vérifiables avec `errors.Is`, notamment les erreurs de policy,
observation, doublon, collision, transition, révision, topologie, ambiguïté,
rejet, limite et retard.

`BuildObservationRef(ExistingCGEOutput)` est un adaptateur pur et expurgé. Il
ne relance aucune association, ne recalcule pas la déviation, ne modifie pas
les routines et n’écrit dans aucun registre.

## Qualification et limites

Les tests couvrent les scénarios A à O : entrée/couloir, lendemain, résidents
simultanés, track inconnu continu, inconnus sans continuité, identité
incertaine, doublon, hors ordre, contexte partiel, topologie inaccessible,
durée maximale, lifecycle, retard dans/hors grâce et déviation élevée avec
continuité forte. Ils couvrent aussi déterminisme, propriété, clones,
révisions optimistes et concurrence; les benchmarks mesurent planning pour 10
et 100 épisodes, application, snapshot, digest et lifecycle.

Le domaine est **in-memory et expérimental**. Il n’ajoute aucun WAL,
checkpoint, replay, persistence ou dépendance externe. Il n’est pas intégré à
`ShadowEngine`, `ShadowOrchestrator`, `synora-core`, au runtime installé, au
Field Trial Recorder ou aux endpoints API. `RuntimeIntegrated`, `Durable`,
`SituationInferenceImplemented` et `SecurityAuthority` restent faux dans la
readiness de cette passe.

La prochaine étape est la dérivation de `Situation Facts`, après stabilisation
et calibration séparées de cette mémoire de travail.
