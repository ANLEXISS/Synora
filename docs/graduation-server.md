# Serveur de graduation OTA Synora

Le serveur de graduation est l’autorité qui connaît l’état de chaque centrale,
attribue les versions autorisées et sert les bundles signés. Il ne doit pas
devenir un coffre de secrets en clair ni une source de configuration globale
partagée entre machines.

## Responsabilités

- enregistrer une machine et lui attribuer un `device_id` unique ;
- vérifier serial, hardware revision et identité cryptographique ;
- provisionner ou signer les éléments initiaux via un canal d’enrollment
  contrôlé ;
- publier un manifest et fournir le bundle RAUC signé ;
- gérer les canaux `dev`, `beta`, `founders` et `stable` ;
- recevoir le résultat du boot healthcheck et l’état de slot ;
- conserver l’historique des versions, migrations et rollbacks ;
- bloquer une version révoquée, une carte incompatible ou un bundle invalide.

Le serveur ne fait jamais de `make install`, ne redémarre pas directement les
services de la centrale et ne décide pas d’écraser `/etc/synora`.

## Identité et données par machine

Enregistrer au minimum :

- serial matériel ;
- `device_id` ;
- hardware revision et board family ;
- clé publique device ou certificat ;
- current slot et last known good slot ;
- current image version et bundle id ;
- kernel/RKNN runtime observés ;
- dernier healthcheck, warnings et raison du dernier rollback ;
- canal de mise à jour et fenêtre de maintenance ;
- timestamps d’enrollment, installation, boot et `mark-good`.

La PSK SynoraNet n’est jamais stockée en clair. Le serveur peut conserver un
hash pour comparaison, ou un secret chiffré par machine dans un KMS/HSM si une
réémission est réellement nécessaire. Les logs, métriques, tickets et traces
HTTP ne contiennent jamais PSK, bearer token, mot de passe ou clé privée.

Les secrets bootstrap sont générés localement par
`synora-bootstrap-config`. Le serveur fournit seulement ce qui est nécessaire
à l’enrollment authentifié et ne réutilise jamais l’identité d’une autre
machine.

## Manifest et graduation

Un manifest signé contient :

- `bundle_id`, `image_version`, `synora_version`, commit et build time ;
- board cible, OS base, kernel attendu et runtime RKNN attendu ;
- fichiers/artefacts inclus et hashes ;
- slots concernés et contraintes RAUC ;
- `config_schema_min` / `config_schema_max` ;
- migrations requises et version de données minimale ;
- modèles requis/optionnels et leur compatibilité ;
- policy healthcheck et délais ;
- policy rollback ;
- canal et niveau de graduation.

Le serveur distribue d’abord à une cohorte limitée, observe boot, health,
rollback et durée de stabilité, puis élargit. Un échec de healthcheck est une
raison de gel automatique du bundle et non une invitation à retenter sans
analyse.

## Séquence d’enrollment

1. La machine fournit serial, attestation locale et clé publique générée
   localement.
2. Le serveur vérifie la commande de fabrication ou le code de claim à usage
   unique.
3. Le serveur crée le `device_id` et associe la clé publique ; aucun secret
   d’une autre machine n’est copié.
4. La centrale génère ses secrets runtime et obtient uniquement les certificats
   ou informations nécessaires.
5. Le serveur marque la machine `enrolled`, puis l’autorise sur un canal.

Une réinscription doit être explicite et auditable. Une clé publique changée
ou un serial incohérent doit bloquer l’opération jusqu’à intervention
opérateur.

## Sécurité opérationnelle

- signature des bundles avec clé hors ligne ou HSM ;
- certificat public de vérification embarqué dans l’image de confiance ;
- rotation et révocation des certificats serveur/device ;
- séparation des rôles build, signature, graduation et support ;
- journal d’audit sans secrets ;
- rate limit et expiration des URLs de bundle ;
- conservation des manifests précédents pour rollback et forensic.
