# V1 source de vérité

Le chemin runtime V1 utilise `internal/cognitivecore.UniversalStore` comme
source autoritaire pour les snapshots, décisions, journal WAL, outbox et
événements rejouables.

`internal/state` est conservé comme projection/domaine legacy isolé. Il n’est
pas importé par `synora-core`, Discovery ou l’API runtime. Une migration ne
pourra changer cette décision qu’avec un schéma versionné, une procédure de
reprise, une vérification de suppression et une preuve de rejeu idempotent.

Le garde-fou est vérifié par
`cmd/synora-core/store_authority_test.go`, en complément des tests de
persistance et de reprise de `UniversalStore`.
