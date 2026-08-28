# Sécurité du Bus local V1

Le Bus accepte uniquement les services allowlistés. Sur un socket Unix, il
associe l’enregistrement à l’identité noyau du pair (`SO_PEERCRED`) et vérifie
que l’exécutable correspond au service demandé. Le champ `Source` est ensuite
comparé au service enregistré ; il ne constitue jamais une preuve d’identité.
Les transports hermétiques `net.Pipe` restent réservés aux tests en processus.

L’ACL est fail-closed. Les familles nécessaires sont les suivantes :

| Producteur | Messages autorisés | Cibles |
| --- | --- | --- |
| `core` | événements Core, `action.request`, réponses RPC | `actions`, services appelants, diffusion contrôlée |
| `api` | RPC vers Core/Connectivity, événements réseau et simulation | `core`, `connectivity`, diffusion |
| `discovery` | événements Discovery/clip/device, RPC dataset/clips | `core`, diffusion |
| `actions` | `action.service.started`, `action.result` | `core`, diffusion |
| `vision` / `lab` | événements `vision.*` | `core` |

Quand `SYNORA_BUS_KEY_ID` et `SYNORA_BUS_SECRET_FILE` sont provisionnés,
chaque frame est également signée HMAC-SHA256 sur sa représentation JSON
canonique. La signature contient un nonce et le timestamp RFC3339 du message.
Le serveur n’accepte que l’identifiant de clé courant et refuse nonce réutilisé,
timestamp hors fenêtre ou payload modifié. Une rotation consiste à remplacer
la clé côté serveur et services de manière coordonnée ; l’ancienne clé est
alors refusée, sans fallback silencieux.

Sans clé provisionnée, les messages privilégiés conservent une protection
d’anti-rejeu par identifiant, empreinte de payload et fenêtre temporelle. Les
événements ordinaires ne sont pas dédupliqués au niveau transport afin de
préserver les retries idempotents des consommateurs V1.
