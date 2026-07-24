# Registre des identifiants CGE

`configs/cge/contracts/identifiers.yaml` est la source canonique des
sémantiques d'identité, d'ordre et de corrélation. Chaque entrée décrit son
générateur, sa portée, son unicité, sa stabilité après redémarrage, sa
persistance et son usage de déduplication.

Les identifiants sensibles entrant depuis Core sont protégés à la frontière
avec `synora.cge.durable-id.v1`. Les domaines `observation`, `entity`, `device`,
`clip`, `track`, `activation` et `sequence` sont distincts. La garde durable
vérifie le domaine et refuse un token brut ou mal typé ; elle ne le transforme
pas.

`node_id` et `zone_id` restent des références topologiques opérationnelles. Les
sequences, révisions, digests et fingerprints servent à l'ordre et à
l'intégrité, pas à remplacer une identité métier.
