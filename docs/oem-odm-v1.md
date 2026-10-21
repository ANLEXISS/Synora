# Fondation produit OEM/ODM Synora V1

## Architecture modulaire

Synora reste un produit unique. Core, MLP, Safety Gate, Evidence V1 et
Universal Store consomment des contrats fonctionnels stables et ne dépendent
pas d’une carte, d’un modèle ou d’un driver. Les modules (`vision-person`,
`vision-pose`, `vision-face`, `vision-vehicle`, `vision-plate`, `vision-animal`,
`camera-health`, `device-control`, `communication`) sont séparés des adaptateurs
de runtime. Les profils sélectionnent explicitement des adaptateurs compilés et
allowlistés ; il n’existe pas de découverte dynamique de plugin.

Les adaptateurs restent derrière Discovery. Ils fournissent capacités,
disponibilité, état de santé, version, compatibilité, résultats redacted,
métriques agrégées et catégorie d’erreur. Ils ne peuvent écrire directement
dans Core/Store, contourner Evidence V1 ou Safety Gate, publier de média ou
s’auto-déclarer qualifiés. Un adaptateur absent rend le module `unavailable` ;
une incompatibilité de modèle le rend `blocked`. Aucun fallback implicite.

## Profil et preflight

`synora.oem.profile/v1` décrit la variante et les capacités demandées sans
secret ni donnée personnelle. Le validateur YAML rejette les champs inconnus,
les valeurs non allowlistées, les politiques risquées, les chemins absolus et
les traversals. Les capacités demandées, observées et effectives sont rapportées
séparément. `qualified` est réservé à une validation indépendante et n’est
jamais accordé par un profil. Lancer `make preflight-oem-v1
PROFILE=configs/oem/synora-rk3588-central-v1.yaml` ou le profil CPU.

Le preflight inspecte l’architecture, l’espace de stockage local, l’auth API et
la présence des adaptateurs/runtime demandés. Il ne charge aucun modèle et ne
fait aucun accès réseau. Le statut `degraded` signifie que le profil est valide
mais une capacité optionnelle demandée, telle que RKNN, est indisponible.

## Ajouter matériel ou modèle

Un nouveau matériel ajoute un adaptateur au module fonctionnel concerné, son
manifest de compatibilité, son préflight, ses hashes quand il existe des
artefacts, et ses tests de parité. Il ne crée pas de fork Core. Les modèles
restent référencés par un manifest versionné ; vérifier SHA-256 et runtime
avant activation. Mock/replay permettent les tests de transport et de contrat,
mais restent `simulated_only` et ne deviennent jamais `real` ou `qualified`.

## Registre de packages modèles `synora.model.package/v1`

Les packages OEM sont des manifestes JSON stricts et déclaratifs sous
`configs/oem/model-packages/`. Ils identifient le module fonctionnel, le
format (`rknn`, `onnx`, `mock`, `replay`), le fichier et son SHA-256, les
adaptateurs de runtime et de post-traitement allowlistés, l’architecture et
version runtime, les contrats tensoriels d’entrée/sortie, la provenance, la
licence et l’état de qualification. Tout champ inconnu, format/adaptateur
inconnu, hash mal formé, chemin absolu/traversal, symlink, fichier absent ou
incompatible est refusé ou reste indisponible. Les packages ne transportent
ni URL, commande, code exécutable, secret, ni chemin hors de la racine locale
de confiance (`SYNORA_MODEL_ROOT`). Aucun package ni poids binaire n’est
committé.

Le profil OEM lie explicitement `module_id -> package_id -> runtime_adapter_id
-> postprocess_adapter_id -> requested_mode`. Discovery calcule les capacités
effectives et expose une vue redacted par `/api/v1/health`; les modules Vision
sans package/runtime effectif ne reçoivent pas de job et renvoient une
évidence structurée `unavailable`. Aucun fallback RKNN/ONNX/mock/replay n’est
automatique. Mock est uniquement un adaptateur synthétique de test; replay
reste indisponible tant qu’aucun lecteur replay audité n’est livré. Tous deux
restent non réels et non qualifiés.

`make preflight-oem-models PROFILE=...` valide les packages sélectionnés sans
charger de runtime, allouer le NPU ou exécuter d’inférence. Il rapporte
séparément demandé, observé et effectif; les erreurs et états d’adaptateur ne
révèlent ni hash intégral, ni chemin, ni tenseur. Le registre contient des
descripteurs synthétiques sans artefact: le preflight RK3588 doit donc rester
`unavailable` sur cette version, et le profil CPU ne rend utilisable que le
mock dans un contexte explicitement test. ONNX CPU et RKNN ont un contrat et
un slot allowlisté, mais aucun exécuteur réel n’est livré ici.

Une conversion ONNX → RKNN est une opération hors-runtime, réalisée sur un
hôte/outillage compatible selon la licence de l’OEM. La racine de production
ne télécharge, ne convertit ni ne charge un package à chaud. Qualification
requiert une preuve locale indépendante associée au SHA-256 exact, plus
fixtures golden et mesures de parité. Les sorties des modules restent des
agrégats sémantiques destinés à Evidence V1; les tenseurs et labels bruts ne
franchissent jamais Discovery vers Core/Store/API.

## Provisioning, sécurité et release

Le bootstrap est désactivé par défaut. Les profils ne contiennent ni secrets ni
identité ; configuration et secrets sont séparés. Les rôles/scopes
d’administration restent gérés par l’auth Discovery existante. L’API est
authentifiée et redacted. La rétention locale suit les politiques runtime ; les
mises à jour sont documentées mais aucune OTA automatique n’est activée.
Le manifest `configs/oem/release-manifest.v1.json` décrit le contenu redacted de
release ; la nomenclature `configs/oem/dependency-licenses.v1.json` couvre les
dépendances déclarées actuellement sans en télécharger de nouvelles.

Les rapports et API n’exposent aucun média, bbox, crop, embedding, chemin local,
identifiant matériel ou donnée brute. Safety Gate reste l’autorité déterministe.
Toutes actions demeurent `active_dry_run`; audio, réseau externe et action
physique sont désactivés.

## État V1

| Domaine | État | Portée démontrée |
|---|---|---|
| Contrat profil et validateur strict | `implemented` | Schéma versionné, unknown-field reject, validation fail-closed |
| Profil RK3588 central | `prepared_blocked` | Profil préparé ; disponibilité RKNN dépend du runtime local |
| Profil générique CPU | `simulated_only` | Adaptateurs mock/replay, pas une qualification CPU de Vision |
| Sélection des adaptateurs | `implemented` | Sélection explicite et allowlist statique |
| Vision face/véhicule/plaque/animal | `not_configured` | Aucun backend qualifié par ce jalon |
| Exécution physique/audio/OTA | `not_qualified` | Désactivées et non revendiquées |

Statuts autorisés pour ce tableau : `implemented`, `simulated_only`,
`real_replay_verified`, `prepared_blocked`, `not_configured`, `not_qualified`.
Ce jalon ajoute une fondation de configuration et de tests ; il ne constitue
pas une qualification terrain ou une preuve d’inférence Vision.
