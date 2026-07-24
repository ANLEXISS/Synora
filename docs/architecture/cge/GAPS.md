# Gaps connus du catalogue CGE

Cette liste est volontairement descriptive. La passe 65 ne corrige pas ces
écarts et ne modifie aucun runtime.

## Critical

Aucun gap critique contractuel restant démontré par
`go run ./cmd/cge-contractgen coverage` :

```text
critical_gaps=0
opaque_durable_envelopes=0
uncatalogued_durable_maps=0
durable_writer_coverage=100%
```

Les anciennes enveloppes sont lues par les décodeurs legacy existants ; les
nouvelles écritures passent par les contrats v1 et les validateurs nommés.

## High

Les gaps High relatifs aux contrats de données sont fermés par les registres
exécutables `identifiers.yaml`, `timestamps.yaml`, `transports.yaml`,
`writers.yaml`, le jeu v1 gelé et la commande `coverage` :

```text
high_contract_gaps=0
field_mapping_coverage=100%
wire_field_coverage=100%
transport_surface_coverage=100%
identifier_semantics_coverage=100%
timestamp_semantics_coverage=100%
```

Les temps absents d'une structure sont explicitement documentés dans le
registre comme absents par design ou soumis à migration ; ils ne sont pas
interprétés comme un autre temps.

## Medium

- Les permissions et garanties fsync de certains stores restent dépendantes de
  la configuration du répertoire ou du système de fichiers.
- Les logs structurés et métriques ne possèdent pas tous un schéma de
  sensibilité vérifié automatiquement champ par champ.
- Les modèles cognitifs expérimentaux ont une surface de sortie plus large que
  le minimum documenté par les projections publiques.
- La conservation de certaines données de field trial dépend de limites
  configurées plutôt que d’une politique centrale de gouvernance.

## Low

- Les fenêtres de dépréciation ne sont pas encore enregistrées pour chaque
  contrat.
- Les contrats publics UI et les projections internes pourraient être séparés
  par des IDs dédiés dans une passe ultérieure.
- Un outil de génération de diagrammes depuis les frontières réduirait le
  risque de divergence documentaire.

## Traitement futur

Chaque correction devra mettre à jour le catalogue, les tests d’architecture,
la documentation de migration et les preuves de non-régression. Un gap ne doit
pas être masqué par une valeur `stable` ou par une validation permissive.
