# Confidentialité, redaction et rétention

## Protection

La frontière Core → CGE traite les identifiants entrants comme bruts. Les
références durables utilisent la primitive unique `internal/cge/durableids` et
le namespace versionné `synora.cge.durable-id.v1`, avec séparation de domaine.
Le format est `cgeid-v1:<kind>:<hex>`. Il s’agit d’une pseudonymisation
déterministe, pas d’un chiffrement : aucune clé ou secret n’est introduit dans
cette passe.

La relation stable est conservée pour une même valeur et un même domaine ; les
domaines observation/entity/device/clip/track/activation/sequence restent
distincts. Une valeur vide reste vide. Les références déjà protégées sont
contrôlées par domaine avant d’être réutilisées.

## Données interdites

Les tokens, secrets, IP brutes, images, vidéos, embeddings et visages ne sont
pas autorisés dans le journal, le WAL, le checkpoint ou le ledger. Les
identités et identifiants de corrélation bruts ne sont pas autorisés dans les
fichiers durables ni dans les logs CGE. Les node/zone IDs restent pour la
résolution topologique, car leur suppression casserait le contexte ; ils ne
doivent pas être utilisés comme substitut d’identité.

Les anciens journaux peuvent contenir des valeurs historiques sensibles. Cette
passe n’effectue aucune réécriture, suppression ou migration automatique. Toute
migration future devra avoir son propre catalogue, outil, backup, validation et
fenêtre de rétention.

## Rétention des stores

Le journal, les générations, le WAL, le checkpoint et le ledger sont durables
et append-oriented ou atomiquement remplacés selon `stores.yaml`. Le WAL et le
ledger n’ont pas de suppression implicite. Les métriques, diagnostics et
déviations en mémoire sont bornés par leur nature et sont réinitialisés ou
reconstruits au redémarrage. Le field trial a des limites de jours/octets
configurables.

Lorsque le code ne définit pas une politique explicite de rétention,
compaction, permissions ou migration, `GAPS.md` le dit au lieu d’inventer une
règle opérationnelle.

## Garde exécutable

Le registre généré associe les champs protégés à leur domaine `durableids` et
associe chaque contrat aux stores autorisés. La garde refuse un token brut, un
token d'un autre domaine, un secret ou une biométrie en clair ; elle ne
pseudonymise jamais au moment d'écrire. Les contrôles s'exécutent avant
`json.Marshal`, l'append du journal/WAL, le remplacement atomique d'un snapshot
ou checkpoint et l'append du ledger.
