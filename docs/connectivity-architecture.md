# Synora connectivity foundation

Cette passe introduit uniquement l’agent local `synora-connect`. Il possède
une identité persistante et publie un état local ; il ne contacte aucun
serveur, ne fait aucun NAT traversal et ne crée aucune interface réseau.

```text
Application
    │
    ├── futur control plane Synora : coordination seulement
    │
    └════════ futur tunnel WireGuard direct ════════ Centrale
```

Le control plane futur attribuera une identité de centrale, des pairs, des
candidats et éventuellement un relais. Il ne doit pas devenir le chemin des
données. Le tunnel WireGuard futur sera séparé de l’identité Ed25519 utilisée
pour l’identité et l’authentification de la centrale.

## État actuel

- `synora-connect` s’enregistre sous `connectivity` sur le bus Unix local ;
- `connectivity.status` retourne uniquement `pkg/contracts.Status` ;
- `GET /api/system/connectivity` relaie ce statut avec authentification API
  existante ;
- la configuration est désactivée par défaut ;
- les clés sont générées localement sous
  `/var/lib/synora/connectivity/` ;
- le contrôleur tunnel est noop/mémoire et ne touche ni netlink, ni routes,
  ni forwarding, ni nftables.

## Données et OTA

`device-identity.key`, `wireguard.key` et `state.json` restent persistants
hors rootfs A/B. Une OTA ou un rollback ne doit jamais les remplacer ni
régénérer l’identité. Le mode `boot-readonly` et le healthcheck OTA ne doivent
pas initialiser, renouveler ou modifier ces fichiers.

Les clés privées sont en `0600`, le répertoire en `0750` et l’état public en
`0640`. Aucun de ces fichiers n’appartient au bundle OTA, à `/opt/synora`, aux
templates Git ou aux diagnostics.
