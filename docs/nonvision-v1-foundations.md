# Fondations non-Vision V1 — contrats et maturité

Ce document décrit les fondations logicielles testées sans périphérique réel.
Le harnais ne change aucun statut de qualification : les services et capacités
matériels restent `not_configured`, `unavailable` ou `unknown` tant qu'un
périphérique et un retour d'état réel et autorisé ne sont pas établis.

## Sources de vérité et frontières

| Domaine | Source de vérité | Projection/test | Autorité |
| --- | --- | --- | --- |
| Configuration topologique | `internal/topology` (configuration du domicile) | `foundationv1.Topology` redacted, directionnelle, avec intégrité, couverture et expiration | Validation de graphe; aucune inférence biométrique |
| Appareils configurés | `internal/device` | `foundationv1.PeripheralRegistry` dans Universal Store | Identifiant abstrait, permissions et capacités validées; aucun pilote |
| État/persistance métier | Universal Store Core (`internal/cognitivecore`) | adaptateur Foundation append-only et replay | Core est l'unique écrivain métier; ouverture fail-closed sur état corrompu |
| Proposition cognitive | MLP/Core | `ActionProposal` abstraite | Le MLP ne commande rien directement |
| Décision d'action | Safety Gate puis permissions, capacité et arbitrage | statut d'action redacted | Gate déterministe; priorité/cooldown ne peuvent pas l'outrepasser |
| Communication | intention symbolique et scheduler | rôle/destination de zone abstraits, clé de texte | suppression/cooldown/déduplication; aucun TTS/audio |
| Recherche/corrélation | observations agrégées et transitions déclarées | résultats à durée limitée | `unknown`, ambiguïté et expiration sont préservés; aucun suivi inter-caméras |
| État API | `StateSnapshot` allowlisté | `GET /api/system/state` dans le harnais éphémère authentifié | Autorisation côté serveur et validation redacted |

`foundationv1.Topology` est une représentation de contrat destinée au Store et
à l'API; elle ne remplace pas le modèle de configuration canonique
`internal/topology`. De même, la registry Foundation ne remplace pas la
configuration de `internal/device` ni ne prouve la disponibilité d'un appareil.

## États et arbitrage

Les fonctions peuvent porter `not_configured`, `unavailable`,
`simulated_test`, `dry_run`, `available` ou `failed`. `available` pour un
périphérique requiert un état observé, une confirmation récente et la provenance
`device_feedback`; le harnais ne fabrique jamais cette provenance.
`desired_state` ne remplace jamais `observed_state`; sans retour, l'observé,
l'état périphérique et la couverture restent `unknown`.

Les seules issues finales d'action sont `allowed_dry_run`, `blocked`,
`suppressed`, `unknown_result` et `failed`. Un appel n'arrive à l'exécuteur
dry-run qu'après Safety Gate, compatibilité action/capacité, permission,
disponibilité déclarée et arbitrage. Une décision Gate négative ne contacte
jamais l'exécuteur. Les clés d'idempotence sont liées au contenu exact de la
proposition : même demande rejouée retourne le résultat existant; réutiliser la
clé avec une autre proposition échoue de façon structurée. Timeout ou résultat
absent donne `unknown_result`, jamais un succès implicite.

Communication transporte seulement une intention, une zone, un rôle abstrait,
une priorité et une clé symbolique. Le scheduler persiste l'horodatage et l'état
de déduplication dans les reçus Store pour reconstruire le cooldown au replay.
Les champs de sortie garantissent `tts_status=not_configured` et
`audio_rendered=false`.

Recherche distingue `found`, `not_found`, `ambiguous`, `coverage_unknown` et
`expired`, avec confiance grossière, provenance et durée de validité. Les
corrélations topologiques restent `hypothesis`, `ambiguous` ou une observation
explicitement confirmée; elles expirent et ne fusionnent jamais d'identité.
PTZ et association biométrique inter-caméras sont interdits.

La projection publique est un type fermé, ne contient pas de chemin, URL,
média, image, bbox, crop, keypoint, embedding, identité, plaque, secret, adresse
ou coordonnée de piste, et refuse aussi ces familles de clés dans les payloads
persistés. Le handler API authentifie côté serveur avant sérialisation.

## Harnais Foundation V1

Le harnais officiel `make test-central-v1` appelle `foundationv1.RunE2E` et
inclut 29 scénarios déclaratifs. Chacun produit une journey redacted, un
`correlation_id` stable, un état API avec vérification 401/200 et une issue
terminale. Les issues sont comptées séparément : parcours complété,
`rejected_expected`, `blocked`, `suppressed`, `unknown_result` et
`failed_expected`; seuls les parcours sans résultat terminal sont incomplets.

Les cas couvrent notamment graphe/discontinuité/couverture/transitions,
périphérique et capacité absents, permission refusée, contradiction/priorité/
cooldown, timeout/retour absent/rejeté, répétition et reprise Store, cooldown de
communication, recherche ambiguë/expirée, donnée interdite, corruption Store
contrôlée et accès API non autorisé. La suite vérifie également que les
terminaisons Safety Gate ne touchent pas l'exécuteur, et que le compteur
`peripheral_available` reste nul sans matériel ni feedback réel.

Ces scénarios sont logiciels et synthétiques. Ils ne démontrent ni la
disponibilité d'un appareil, ni la robustesse d'une installation, ni une action
physique, ni une communication audible. Le MLP est simulé; aucun bundle n'est
chargé par la suite Foundation.

## Maturité et fonctions inactives

| Fonction | Code/contrat | Harnais | Matériel/runtime |
| --- | --- | --- | --- |
| Topologie et transitions | Validateur strict, états d'intégrité, provenance/expiration | Validations invalides, déconnectées, inconnues et transitions interdites | Déclaration fournie par l'utilisateur; aucune localisation réelle |
| Registre périphériques | Persisté dans Universal Store; permissions/capacités bornées | absence, inconnu, capacité et permission refusées | Aucun pilote; `available` impossible sans retour confirmé |
| Action | Gate → policy → arbitrage → exécuteur dry-run; idempotence/replay | conflit, priorité, cooldown, timeout et rejets attendus | Exécution physique `not_configured` |
| Communication | Intention abstraite, permission, cooldown et reprise | suppression Safety Gate/cooldown/replay | TTS `not_configured`; aucun son |
| Recherche/corrélation | Résultats temporisés et redacted | inconnu, ambiguïté, expiration, absence de couverture | Aucun suivi inter-caméras ni PTZ |
| Store | Journal, révision, replay, compaction et validation existants | reprise, idempotence et corruption fail-closed | Données de production non touchées |
| API/observabilité | Projection allowlistée et états de fonction | serveur éphémère authentifié; trace/counters par cas | Aucun serveur de production lancé |

Explicitement inactifs : pilotes réels, automatisation, serrures/garage,
PTZ, caméra physique, TTS/rendu audio, communication réseau externe,
identification biométrique, et exécuteur physique. Ces fonctionnalités exigent
un dossier d'intégration et de sûreté dédié, un périphérique réel avec retour
d'état, des permissions explicites et des tests indépendants avant tout
changement d'état.

## Ajout futur d'une intégration physique

1. Documenter modèle, capacités, zones, état de santé, permissions, propriétaire
   et protocole de retour; ne jamais enregistrer numéro de série, MAC ou secret
   dans Store/API/rapport.
2. Ajouter un adaptateur isolé à `internal/device`; démarrer en
   `not_configured`/`unavailable`, puis démontrer retours `unknown` et erreurs.
3. Ajouter compatibilité action/capacité et permission explicite, tests de
   doublons, timeout, reprise, retour rejeté et panne matérielle.
4. Garder l'action en `dry_run` jusqu'à revue de sûreté et validation explicite
   du matériel. Cette préparation ne fournit aucune autorisation d'activation.

TTS exige un port symbolique séparé, consentement, permissions et validation
audio dédiée; aucun rendu ne découle de l'intention. PTZ/caméra exige une
intégration matérielle, règles de zones et limites physiques séparées; ce
contrat ne permet aucune commande PTZ. Les tests/qualifications caméra restent
distincts et `J2/J3/J4` ne changent pas d'état ici.
