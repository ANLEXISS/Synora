# Synora V1 — threat model et identité

## Actifs

- identité cryptographique de la centrale et des caméras ;
- clés privées Ed25519 et WireGuard, stockées localement avec permissions
  `0600` et jamais incluses dans les contrats publics ;
- registre durable des clés publiques, générations, révocations et remplacements ;
- configuration, sessions API, clips, incidents et données résidentes.

## Frontières de confiance

1. La centrale Core est l’autorité de l’état métier et du registre des appareils.
2. La caméra est un périphérique non fiable jusqu’à bootstrap local et
   authentification réussis.
3. Discovery, Vision, API et bus sont des processus locaux séparés ; ils ne
   deviennent pas propriétaires de l’identité métier.
4. Un serveur de rendez-vous WireGuard éventuel ne reçoit ni clip, ni incident,
   ni biométrie ; il ne fait que signalisation/rendez-vous.

## Attaquants considérés

- caméra clonée, appareil supprimé ou clé compromise ;
- client LAN sans session, replay réseau ou modification d’un payload ;
- utilisateur local non privilégié lisant des fichiers de secrets ;
- attaquant contrôlant un serveur de rendez-vous ;
- panne ou corruption du stockage pendant une rotation.

## Garanties implémentées

- identité centrale Ed25519/WireGuard stable par clé publique ;
- registre public versionné, atomique et fail-closed sur corruption, symlink ou
  permissions trop larges ;
- génération, rotation, révocation et remplacement conservés dans l’historique ;
- une identité révoquée ou remplacée ne peut plus authentifier de message ;
- les signatures sont vérifiées avec la clé publique active et un identifiant
  de génération explicite côté registre ;
- aucune clé privée n’est persistée dans le registre ni sérialisée par les vues
  publiques existantes.

## Bootstrap et cycle de vie

Le bootstrap usine doit remettre la centrale dans un état non provisionné et
une caméra avec une identité neuve. Le pairing local limité dans le temps et
l’action physique obligatoire seront finalisés au jalon 12. Une rotation
remplace la clé d’un même appareil ; une perte ou compromission utilise
`replaced`, puis révoque l’ancienne identité. Une révocation est terminale
pour cette génération.

## Résidus explicitement suivis

La résistance à la compromission physique des clés, l’attestation matérielle,
la révocation distante en cours de session et la qualification réglementaire
restent à démontrer dans les jalons 12–15 et lors de la qualification hardware.
