# Architecture de connectivité V1

## Frontière

Le plan WireGuard est une liaison d’administration et de maintenance entre
la centrale et des appareils explicitement autorisés. Le contrôleur de tunnel
reste abstrait dans cette V1 : l’implémentation `NoopTunnelController` ne
crée ni interface, ni route, ni règle netlink. La mise en œuvre matérielle
WireGuard est donc un adaptateur ultérieur, derrière la même interface testée.

Les chemins de connexion sont limités à :

- une connexion directe quand elle est possible ;
- un relais de rendez-vous uniquement pour l’établissement du tunnel ;
- aucun transport de vidéo, biométrie, résident ou payload applicatif par le
  service de rendez-vous.

Les candidats et endpoints de rendez-vous sont éphémères et ne contiennent
que les identifiants publics, clés publiques WireGuard, endpoint, génération
et expiration. Les clés privées restent dans le répertoire local de l’agent,
en `0600`, et ne sont jamais sérialisées dans l’état ou les réponses publiques.

## Autorisation et révocation

`internal/connectivity.AccessRegistry` est la source locale de vérité pour
l’accès :

- un propriétaire central est enrôlé une seule fois ;
- chaque opération sensible est signée par la clé Ed25519 active du
  propriétaire et liée à la génération courante ;
- un pair actif contient ses deux clés publiques et sa génération ;
- rotation et révocation changent la génération, et une révocation interdit
  immédiatement tout nouveau rendez-vous ;
- un transfert de propriété révoque les pairs existants avant de remplacer le
  propriétaire ;
- le factory reset d’accès efface propriétaire et pairs, marque l’instant du
  reset et impose un nouvel enrôlement.

Le fichier est chargé en refusant symlink, fichier non régulier, permissions
trop larges, JSON inconnu ou corruption. Les écritures sont atomiques,
`0600`, avec sauvegarde contrôlée.

## Limites de qualification

Les tests V1 couvrent le modèle de sécurité, les générations, la perte ou
compromission d’un pair, la rotation, le transfert, le reset et les
permissions du registre. Ils ne constituent pas une qualification radio,
une mesure de débit WireGuard ou une preuve de fonctionnement netlink sur
Rock 5/Zero 3W.
