# Pass 30 — contexte spatial, temporel et domestique

Pass 30 capture un contexte descriptif détaché au moment où un événement
franchit la frontière Shadow/CGE. Le contexte est versionné (`context-v1`),
transporté par `chains.ObservationRef` et persiste dans les records existants
(`chain.added`, `chain.observation_added`, hypothèses, snapshots et
générations). Aucun record WAL supplémentaire n’est introduit.

## Topologie, frame et signature

`internal/cge/context` ne dépend ni du Core, ni du journal, ni du replay. Une
`TopologySnapshot` contient seulement une révision, un instant de capture, des
nœuds détachés et des arêtes. Les nœuds sont bornés, uniques et canoniquement
triés ; les arêtes doivent viser des nœuds existants, ne peuvent pas être des
self-edges et la hiérarchie parentale ne peut pas cycler. Les cycles de
déplacement et les composantes déconnectées restent autorisés.

`ResolveFrame` reçoit l’identifiant d’observation, son timestamp, le nœud, une
timezone explicite, l’occupation, le mode observé et une topologie. Il ne lit
jamais l’horloge système et n’écrit rien. La temporalité locale comprend le
weekday, le weekend, la minute du jour/semaine, l’offset UTC et un day-part
stable : night [00:00,06:00), morning [06:00,12:00), day [12:00,18:00),
evening [18:00,24:00). Le bucket de signature est de 15 minutes.

Les états `unknown`, `unoccupied`, `occupied` et les modes `unknown`, `home`,
`away`, `night`, `sleep`, `armed` décrivent uniquement une valeur reçue. Une
identité observée ne déduit pas l’occupation. Une topologie absente ou un nœud
inconnu peut produire un frame `partial` si `AllowPartial` est actif ; aucune
pièce ou arête n’est inventée. Le fingerprint SHA-256 couvre tous les champs
contextuels canoniques, jamais l’acteur, la corrélation ou une donnée sensible.

`EvaluateTransition` produit seulement des faits déterministes : même nœud ou
zone, adjacency, distance BFS, entrée/sortie, extérieur, changement de mode ou
d’occupation, changement de bucket, changement de révision et partialité.
`ShortestPath` distingue `reachable`, `unreachable` et `unknown`. Aucun fait ne
signifie normal, anormal, sûr ou dangereux.

## Shadow et compatibilité

Les flags sont désactivés par défaut :

```text
SYNORA_CGE_SHADOW_CONTEXT_ENABLED
SYNORA_CGE_SHADOW_CONTEXT_TIMEZONE
SYNORA_CGE_SHADOW_CONTEXT_ALLOW_PARTIAL
```

La timezone par défaut est `UTC`, jamais la timezone implicite du système, et
`AllowPartial` vaut `true`. Lorsque le contexte est désactivé, le provider
n’est pas appelé et le chemin de la passe 29 reste identique. Le provider
runtime actuel est un adaptateur statique partiel : il conserve le NodeID de
l’événement et les états explicitement disponibles ; aucune structure mutable
du Core n’est importée. Une source topologique en lecture seule pourra être
branchée ultérieurement.

Une observation legacy avec `Context=nil` reste valide et lisible. Les clones
de chaînes, les effets de résolution et les snapshots copient le frame sans
partage mutable. Le contexte reste inchangé après association, contribution,
rebase, supersession, replay ou génération.

## Limites et garanties

Le contexte n’est pas encore utilisé par les politiques d’association ou
d’evidence. Il ne change aucun score, seuil, valeur de contribution ou
fingerprint de décision ; il ne crée pas d’hypothèse depuis le contexte seul,
ne résout aucune hypothèse, ne change aucun lifecycle, ne produit aucune
décision de sécurité et ne déclenche aucune action/automation. Le traitement
historique reste prioritaire et isolé.

La capture est effectuée avant association, puis l’ordre durable existant
reste inchangé. Une interruption peut donc laisser l’association durable sans
effet cognitif ultérieur ; l’état demeure valide et un événement futur peut
compléter l’apprentissage. Les métriques sont des compteurs agrégés sans
identifiant ni cardinalité dynamique. La topologie complète n’est pas exposée
par Snapshot/Explain ni par HTTP.
