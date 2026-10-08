# Fondations logicielles V1 — état et limites

`internal/foundationv1` définit les contrats et ports logiciels des fonctions
V1 non raccordées. Les états autorisés sont `not_configured`, `unavailable`,
`simulated_test`, `dry_run`, `available` et `failed`. L’état initial de chaque
fonction est `not_configured` ; exécuter le harnais ne change pas cet état en
qualification.

Le harnais central ajoute dix parcours : caméra indisponible, périphérique
inconnu, action bloquée, action dry-run, doublon idempotent, communication
supprimée, recherche sans couverture, corrélation ambiguë, reprise après
redémarrage, et rejet de données interdites. Les neuf scénarios admis traversent
le validateur Discovery, le Universal Store, le Core et son Safety Gate, les
ports MLP/exécuteur de test, puis la projection API éphémère. Le dixième est
rejeté par Discovery avant Store/Core, comme l’exige la frontière de sécurité.

Le MLP utilisé par ces cas est simulé et n’ouvre aucun bundle ; l’exécuteur
construit uniquement un résultat abstrait et n’envoie aucune commande. La
communication utilise des clés de texte et rôles abstraits ; TTS et rendu audio
sont toujours désactivés. Recherche et corrélation ne font ni identité
inter-caméras ni PTZ ; une corrélation ne devient `observed` qu’après une
confirmation explicite de preuve validée. L’absence ou la perte de flux caméra
laisse toujours la sûreté de scène à `unknown`.

La projection `/api/system/state` est allowlistée et le handler exige une
autorisation serveur. Dans le harnais, une session éphémère exerce les cas 401
et 200. Ce port n’est pas branché à un service de production ou à un registre
de périphériques réel. Ces tests prouvent uniquement le parcours logiciel
simulé ; ils ne qualifient ni modèle, caméra, communication audio ou périphérique.

Le contrat unique `PipelineInput` contient un identifiant abstrait de scénario,
une enveloppe `Record` à statut explicite, une raison Safety Gate fermée
(`safety_gate_denied` ou absente) et un horodatage. Les propositions MLP et
les décisions simulées restent dans leurs ports dédiés, séparés des données
d’évidence. La fixture golden est
`testdata/foundation-v1/pipeline-input-valid.json`; les champs interdits du
payload sont rejetés à Discovery.

La comptabilité distingue `journeys_terminal_completed`,
`journeys_terminal_rejected_expected` et `journeys_incomplete`. Le cas de
données interdites a un reçu terminal redacted vérifié par l’API éphémère,
mais son payload ne va ni dans le Store ni vers Core/MLP/Safety Gate ou
l’exécuteur. Un rejet attendu n’est donc pas une journey incomplète.

Le contrat unique `PipelineInput` contient un identifiant abstrait de scénario,
une enveloppe `Record` à statut explicite, une raison Safety Gate fermée
(`safety_gate_denied` ou absente) et un horodatage. Les propositions MLP et
les décisions simulées restent dans leurs ports dédiés, séparés des données
d’évidence. La fixture golden est
`testdata/foundation-v1/pipeline-input-valid.json`; les champs interdits du
payload sont rejetés à Discovery.

La comptabilité distingue `journeys_terminal_completed`,
`journeys_terminal_rejected_expected` et `journeys_incomplete`. Le cas de
données interdites a un reçu terminal redacted vérifié par l’API éphémère,
mais son payload ne va ni dans le Store ni vers Core/MLP/Safety Gate ou
l’exécuteur. Un rejet attendu n’est donc pas une journey incomplète.
