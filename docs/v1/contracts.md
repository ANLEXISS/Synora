# Contrats V1 canoniques

## Décision M004 validée

Le package canonique des contrats partagés est `synora/pkg/contract` (`pkg/contract`).
Le package `synora/pkg/contracts` reste réservé aux contrats de connectivité déjà
isolés ; il n’est pas la destination générale des contrats V1.

Les timestamps V1 restent encodés en RFC3339/RFC3339Nano par la sérialisation
JSON standard de `time.Time`. Cette décision conserve les consommateurs existants.
Un éventuel timestamp Unix millisecondes devra être ajouté ultérieurement sous un
champ distinct, tel que `timestamp_ms`, sans changer le champ RFC3339.

## Types et frontières

`Message`, `Event`, `Decision`, `Clip`, `Resident`, `Action`, `ActionRequest`,
`ActionResult` et `Incident` sont les types partagés existants. Leurs champs JSON
et leurs adaptateurs legacy sont conservés ; les champs inconnus sont tolérés par
les décodeurs Go et Python concernés.

`FaceDatasetVersion` est le seul type promu dans M004. Il décrit les métadonnées
immutables échangées entre Discovery et Vision : version, révision, date de build,
checksums, empreinte du modèle et dimension d’embedding. Les embeddings et les
chemins de stockage restent internes. `internal/facedataset.Manifest.ContractVersion`
est l’adaptateur explicite vers cette vue.

`CameraObservation` est promu additivement pour la frontière Discovery/Core de
M025. Il transporte uniquement l’identité technique, l’endpoint, le firmware,
les capacités, l’état de santé et `last_seen`. Son identifiant stable sert à
dédupliquer les observations ; Core en conserve la projection dans `CameraState`.
Les événements historiques `discovery.camera.online` et
`discovery.camera.offline` restent compatibles.

`Track`, `Presence` et `ObservationRef` restent internes : aucun échange
interservice V1 ne justifie leur promotion. Les futures promotions devront fournir
un adaptateur et des tests de compatibilité avant migration des consommateurs.

## Compatibilité et validation

`V1SchemaVersion` et `ValidateSchemaVersion` refusent les versions futures tout en
acceptant l’absence de version pour les enregistrements legacy. Les validations
V1 vérifient les identifiants et les champs requis sans imposer un format nouveau
aux identifiants déjà utilisés.

Les fixtures déterministes sous `pkg/contract/testdata/v1` couvrent les formes
courantes et legacy de Message, Event et FaceDatasetVersion, ainsi que la
sérialisation des autres types partagés. Toute rupture de fixture doit être
traitée comme une évolution explicite du contrat.
