# Discovery V1

Discovery est la frontière unique entre Synora et le monde extérieur.

## Responsabilités

- recevoir les capteurs, clips Vision, périphériques, Web et services externes ;
- normaliser et valider minimalement les messages versionnés ;
- superviser les healthchecks et publier les indisponibilités ;
- servir les lectures Web depuis un lecteur de Store ;
- transformer toute commande Web en événement normal vers Core ;
- recevoir une demande d’action abstraite et publier son résultat comme nouvel
  événement ;
- communiquer avec les périphériques et les services sortants.

Discovery ne contient aucun State Encoder, MLP, score de danger, règle
décisionnelle ou écriture métier dans le Store. En V1, son adaptateur matériel
reste en `dry_run` ; `physical_action_executed` est toujours `false`.

La surface Web V1 Discovery fournit les lectures `state`, `history` et
`health`, la souscription SSE `subscribe`, ainsi que `commands` et `messages`.
Une commande ou un message entrant est toujours validé par `Boundary`, devient
un événement versionné publié vers Core, et ne possède aucun chemin d’écriture
direct vers Store ou périphérique. Les réponses d'historique sont des copies
en lecture seule du Store.

## Protection de frontière

Les payloads sont bornés et refusent les clés représentant un média brut,
frame, bbox, crop, embedding, identité biométrique ou identifiant matériel.
Les événements normalisés conservent uniquement des faits agrégés et leurs
identifiants de corrélation.

## Rollback

Le composant est isolé par le commit `feat: add Discovery as Synora external
boundary`. Revenir au commit parent rétablit seulement le code de branche ;
aucune configuration de `/home/rock/Synora` n’est modifiée.
