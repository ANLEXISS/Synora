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
