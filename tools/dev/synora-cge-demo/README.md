# Synora CGE investor demonstrator

Outil de développement local uniquement. Il exécute le vrai `ShadowEngine`
dans un répertoire temporaire et expose une UI autonome sur `127.0.0.1`.
Il ne lit pas `/var/lib/synora`, n’appelle pas `systemctl`, ne modifie aucune
politique cognitive et ne déclenche aucune action.

## Lancer

```bash
go run ./tools/dev/synora-cge-demo --scenario investor-core --locale fr
```

La bibliothèque Live Lab contient `routine-formation`,
`temporal-divergence`, `spatial-divergence`, `house-mode-divergence`,
`interval-divergence`, `combined-divergence`, `partial-context`,
`routine-shift`, `association-ambiguity`, `unknown-new-subject`,
`idempotent-retry`, `restart-replay` et `memory-field-isolation`.

Le parcours guidé historique reste disponible avec `investor-core` pour la
présentation locale ; il ne constitue pas une hypothèse de situation et ne
déclenche aucune action.

Options : `--seed`, `--port` (8091), `--headless`, `--export DIR`, `--reset`,
`--technical`. Le serveur se lie exclusivement à `127.0.0.1`.

## Live Lab

La racine `/` est le laboratoire interactif. Elle démarre sans autoplay une
session `LiveSession` temporaire, isolée du runtime Synora. L’utilisateur
choisit une observation, son contexte et son horodatage simulé, puis l’envoie
à `ShadowEngine`. La réponse expose la trace réelle : contexte, plan
d’association, evidence, hypothèse éventuelle, déviation pré-apprentissage,
apprentissage et records WAL.

La présentation guidée existante est conservée sur `/presentation`. Le bouton
de bascule technique change seulement la densité d’affichage et ne relance pas
le moteur. La bannière `Événements synthétiques — traitement réel du CGE` est
toujours visible.

Fonctions du laboratoire : horloge simulée, actions rapides, exécution
immédiate ou révélation pas à pas de la trace, répétition unitaire ou batch,
bases synthétiques générées par exécution réelle sur 7 ou 30 jours, reset de
session et redémarrage/replay durable. Les endpoints `/api/live/*` restent
locaux et le flux SSE est `/api/live/events`.

Le pas à pas capture le traitement réel en une exécution puis révèle ses étapes
dans l’ordre. Il ne prétend pas interrompre les sous-opérations internes du
moteur.

## Export hors ligne

```bash
go run ./tools/dev/synora-cge-demo --scenario investor-core \
  --headless --export /tmp/synora-cge-investor-demo
```

L’export contient `index.html`, `app.js`, `live.js`, `live.css`, `live-extra.css`, `styles.css`,
`scenario.json`, `claims.json` et `manifest.json`. Il ouvre la présentation
préenregistrée hors ligne ; le Live Lab reste intentionnellement lié à ses
API locales et ne peut pas être simulé par l’export statique. Le scénario et le manifest sont produits
par l’exécution courante ; les JSON sont aussi embarqués dans la page afin
que l’ouverture `file://` ne dépende pas d’une requête réseau.

## Présentation

Espace : pause/reprise · flèches : chapitre précédent/suivant · `1`–`9` :
chapitres · `F` : plein écran · `T` : mode technique · `R` : reset.
`/presentation` active la route optimisée pour une capture 16:9.

Le parcours cible neuf minutes. Les chapitres sont navigables manuellement,
ce qui permet de ralentir une question investisseur sans relancer le moteur.

## Provenance et restrictions

Les chaînes, hypothèses, routines, scores de déviation, facteurs, révisions,
séquence WAL et digest sont lus depuis `ShadowEngine` et ses registres
défensifs. Le frontend ne contient aucun score cognitif de démonstration.
Les chiffres de qualification sont absents si aucun rapport versionné n’est
chargé ; l’interface affiche alors explicitement cette absence.

Le scénario est synthétique. `synthetic_episode_not_separated` reste visible :
la mécanique cognitive est fonctionnelle, mais sa calibration comportementale
doit être validée sur des domiciles réels. Le CGE reste en Shadow Mode et n’a
actuellement aucune autorité sur la sécurité.

Pour ajouter un scénario, étendre `internal/cge/demo/scenario.go`, conserver
des événements synthétiques, puis ajouter un test de déterminisme et une
entrée documentaire dans `docs/investor/`.
