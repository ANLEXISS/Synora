# Règles de développement des contrats CGE

Toute nouvelle entrée, sortie, structure sérialisée, écriture durable, métrique,
RPC, route HTTP, message WebSocket ou erreur CGE doit être ajoutée au catalogue
avant le code qui l'utilise.

`surface-inventory.yaml` décrit ce que le scanner a découvert ; il ne constitue
pas une décision architecturale. Une décision doit apparaître dans
`field-mappings.yaml` ou dans une exemption explicite, limitée aux surfaces
non persistantes et non publiques. Chaque exemption approuvée porte
`review_status: approved`, une raison, un périmètre et une preuve bornée.
Les valeurs découvertes par défaut
(sensibilité, rétention, protection ou persistance) ne sont jamais une preuve
de revue.

## Procédure

1. Ajouter un ID versionné et son propriétaire dans le catalogue.
2. Déclarer l'implémentation Go exacte, les champs wire, la confiance, la
   sensibilité, la protection, la persistance, la rétention et l'autorité.
3. Relier le contrat aux frontières, stores, transports et registres
   identifiants/temps concernés.
4. Ajouter une fixture valide et des fixtures de rejet. Les mappings générés
   ne sont que des propositions : `scaffold-mappings` écrit
   `/tmp/cge-field-mapping-proposal.yaml` avec `review_status: pending` et ne
   modifie jamais le mapping approuvé.
5. Exécuter :

```bash
go run ./cmd/cge-contractgen generate
go run ./cmd/cge-contractgen check
go run ./cmd/cge-contractgen check-compat
go run ./cmd/cge-contractgen coverage
go run ./cmd/cge-contractgen freeze-baseline   # création initiale seulement
go run ./cmd/cge-contractgen freeze-baseline-v2 # migration approuvée seulement
go run ./cmd/cge-contractgen check-compat --baseline v2
```

6. Vérifier les tests de dérive et les tests de store avant revue.

Une modification compatible conserve l'ID et les invariants de sécurité. Une
modification breaking crée un nouvel ID de version, une migration documentée et
une fixture legacy. Les anciens fichiers ne sont jamais réécrits
automatiquement.

Le registre généré est le seul registre utilisé au runtime ; les YAML sont des
sources de génération et ne sont pas lus par le système installé. Une baseline
existante est immuable : `generate`, `freeze-baseline-v2` et `check-compat` ne
l'écrasent jamais. La couverture est une jointure indépendante entre la
découverte AST du code, les mappings approuvés, les exemptions prouvées et le
catalogue ; `surface-inventory.yaml` ne compte jamais comme approbation.

La preuve de surface 67.3 découvre séparément HTTP, RPC, Bus et WebSocket avec
une clé exacte (transport, méthode/type, chemin/canal, direction), suit
récursivement les types atteignables et recense les sorties transportées. Les
writers sont découverts dans tout `internal/cge` ; les helpers sont reliés aux
sites de mutation physique et la garde doit précéder la première mutation.
