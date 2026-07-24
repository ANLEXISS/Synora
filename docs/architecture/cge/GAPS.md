# Gaps connus du catalogue CGE

Cette liste est volontairement descriptive. La passe 65 ne corrige pas ces
écarts et ne modifie aucun runtime.

## Critical

- La couverture champ-par-champ de toutes les structures complexes n'est pas
  encore exprimée dans le YAML : `gosurface.CompareFields` et ses fixtures
  détectent la dérive, mais les mappings `go_field` doivent encore être
  complétés contrat par contrat. Le générateur refuse toutefois toute
  implémentation Go absente de la surface surveillée.
- Tous les writers CGE actuellement identifiés sont derrière
  `ValidateStoreWrite`. Les enveloppes legacy opaques utilisent une garde
  générique (secrets/biométrie et contrat/store) et ne permettent pas toujours
  une inspection sémantique de chaque identifiant imbriqué. La fermeture
  complète exige de remplacer ces enveloppes par des contrats structurés ou
  des validateurs dédiés, sans réécrire les historiques.

## High

- Les sept temps (`observed_at` à `persisted_at`) ne sont pas présents avec une
  sémantique complète sur chaque structure actuelle.
- Les générateurs, portées et politiques de déduplication de tous les
  identifiants historiques ne sont pas centralisés dans un registre exécutable.
- Les politiques de rétention et de compaction de plusieurs stores historiques
  et du feedback store ne sont pas explicitement configurées dans un contrat
  unique.
- Les routes RPC/HTTP/WebSocket ne sont pas toutes reliées automatiquement à
  des IDs de contrats et à des versions de réponse.
- La taxonomie des erreurs est répartie entre packages et n’est pas encore
  imposée par génération de code.

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
