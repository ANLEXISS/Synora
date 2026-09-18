# Architecture Synora V1 — gel architectural

Statut : gel V1 avant toute évolution sémantique du MLP.

Ce document fixe les responsabilités, les frontières et le flux de référence
de Synora V1. Toute évolution qui modifie une frontière ci-dessous doit faire
l’objet d’une décision d’architecture explicite et d’un nouveau contrat
versionné.

## 1. Sources de vérité et autorité

Les composants suivants sont autoritaires pour leurs domaines respectifs :

- **Core** : état opérationnel, événements acceptés, priorités système et
  décision de flux ;
- **EventStore** : historique durable des événements et des transitions
  acceptées ;
- **Engine** : interprétation déterministe des événements et projection des
  faits vers l’état ;
- **Topology** : identité, relations et contraintes des nœuds, zones et
  chemins ;
- **Device Store** : disponibilité, capacités et état des dispositifs ;
- **Safety Gate**, **Action Ledger** et limites physiques : seules autorités
  habilitées à autoriser ou refuser une action exécutable.

La Vision fournit des observations et des résumés validés. Elle ne fournit
jamais une commande, ne modifie pas directement l’état autoritaire et ne peut
pas contourner le Core, le Device Store ou le Safety Gate.

Le MLP produit des propositions cognitives : classifications, tâches,
propositions d’action et signaux de divergence vis-à-vis du teacher. En V1,
il est exclusivement **advisory_shadow** et n’est pas autoritaire. Il ne peut
ni abaisser un plancher P0, ni créer une autorisation, ni appeler un exécuteur.
Le runtime MLP V1 est CPU uniquement ; le NPU RK3588 reste réservé à la
Vision.

Le Safety Gate, l’Action Ledger et les limites physiques restent déterministes
et sont les seuls mécanismes habilités à autoriser une action. Dans le mode
shadow, toute sortie cognitive reste une proposition observée en `dry_run` et
aucune action physique n’est exécutée.

## 2. Flux V1 figé

Le flux nominal est :

```text
capteur / clip
      ↓
Discovery
      ↓
segments Vision
      ↓
runtime d’épisode
      ↓
StateFrame déterministe
      ↓
MLP CPU advisory
      ↓
Safety Gate
      ↓
action éventuelle
```

La dernière étape désigne uniquement une possibilité du pipeline autorisé par
les composants déterministes. Elle est inactive dans le shadow E2E : aucune
commande physique, notification, verrou, éclairage, sirène ou appel externe
ne peut être déclenché par le MLP.

Les segments Vision et le runtime d’épisode transforment le média en
observations et résumés validés. Le StateFrame est construit de façon
déterministe à partir des sources autoritaires et des contrats V1. Le MLP
consomme ce StateFrame et renvoie des propositions traçables ; il ne réécrit
pas le StateFrame.

## 3. Frontières non négociables

### Bus et données

- Aucune frame, bbox, crop, embedding biométrique ou média brut ne circule
  sur le bus.
- Les échanges inter-composants utilisent exclusivement des contrats
  versionnés et validés.
- Les observations Vision sur le bus sont structurées, minimales et
  suffisamment sûres pour être rejouées sans transporter le média source.

### Bornes et pression

- Les épisodes, pistes, files et ledgers sont bornés.
- Une saturation, une expiration ou une indisponibilité se traite par une
  politique déterministe et fail-closed ; elle ne déclenche pas une croissance
  illimitée ni une inférence implicite.
- Les limites physiques et les budgets restent vérifiables indépendamment du
  MLP.

### Priorités et décisions

- **P0 est réservé au Core**. La Vision et le MLP ne peuvent ni l’inventer,
  ni le déclasser.
- **P1 Vision est prioritaire mais non décisionnel** : les observations P1
  peuvent accélérer le traitement et alimenter l’épisode, mais l’autorité
  reste au Core et à ses contrats déterministes.
- Le MLP ne peut pas autoriser une action, contourner le Safety Gate ou
  transformer une proposition en exécution.
- Le mode shadow interdit toute exécution physique et conserve la politique
  `dry_run` comme garde-fou obligatoire.

### Boucle cognitive

- Il n’existe pas de boucle de rétroaction **MLP → StateFrame → MLP**.
- Une proposition MLP est un résultat de trace et de comparaison ; elle ne
  devient pas une nouvelle vérité d’état dans le même cycle.
- Les divergences teacher/MLP sont des signaux d’évaluation et non des
  décisions opérationnelles.

## 4. Contrats V1 et invariants d’exécution

Les schémas, versions, dimensions, labels, seuils et politiques de sortie
sont des contrats. Ils doivent être validés à la frontière du composant et
rester compatibles avec les manifests et les hashes d’artefacts attendus.

Le StateFrame V1 est déterministe et constitue l’unique entrée cognitive
autorisée. Le MLP V1 utilise le runtime CPU et les têtes prévues par le
contrat ; toute absence de bundle, de parité, de manifeste ou d’artefact doit
produire un chemin fail-closed où le teacher reste disponible et où aucune
proposition d’action MLP n’est produite.

Les traces et rapports peuvent contenir des identifiants d’épisode, des
empreintes d’état, des résumés structurés, des labels et des raisons de
filtrage. Ils ne contiennent pas de média brut, bbox, crop, embedding ou donnée
biométrique.

## 5. Éléments explicitement différés

Les sujets suivants ne font pas partie du gel architectural V1 :

- State Encoder v5 et nouvelles features Vision ;
- SFace RKNN ;
- OCR/plaque ;
- détection d’objets sensibles ;
- code caméra réel ;
- activation du MLP ;
- RKNN pour le MLP.

Le réentraînement ou la modification sémantique des têtes MLP est également
hors de cette tâche. Toute reprise de ces sujets doit préserver les frontières
ci-dessus, introduire les contrats nécessaires et être validée dans un cycle
distinct.
