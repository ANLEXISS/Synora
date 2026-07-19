# Agent `synora-connect`

## Commandes locales

```text
synora-connect run [--config PATH] [--data-dir DIR] [--bus SOCKET]
synora-connect status [--config PATH] [--data-dir DIR]
synora-connect explain [--config PATH] [--data-dir DIR]
```

Les chemins temporaires permettent les tests sans `/etc`, `/var/lib`, root ou
réseau. `run` utilise uniquement le socket Unix du bus et répond à
`connectivity.status`. Les commandes `status` et `explain` ne nécessitent pas
que l’agent soit lancé.

## Identités

L’identité Ed25519 est générée avec `crypto/rand`. Le `device_id` est dérivé
de la clé publique, jamais d’une MAC, d’un hostname, d’une adresse IP ou d’un
UUID de filesystem. La paire WireGuard est une clé X25519 au format base64
versionné, dérivée avec l’implémentation `golang.org/x/crypto/curve25519`.

Les fichiers existants sont rechargés strictement ; un lien symbolique, un
fichier non régulier, une permission trop large ou un format invalide est
refusé. Une clé valide n’est jamais silencieusement remplacée.

## Machine d’états

La configuration initiale produit `disabled/none`. Une configuration activée
sans enrôlement produit `unprovisioned/none`. Les états de contrôle, de
connexion directe et de relais sont des contrats futurs : aucune transition
réseau réelle n’est implémentée dans cette passe.

## Sécurité et limites

L’unité systemd n’a aucune capacité réseau et ne reçoit pas `CAP_NET_ADMIN`.
Elle utilise `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`,
`PrivateDevices` et une écriture limitée à la donnée persistante de
connectivité. Aucun client HTTP/WebSocket, relais, STUN, ICE, PCP, NAT-PMP,
UPnP ou WireGuard noyau n’est présent.

## Passes suivantes

1. définir l’enrôlement signé et la révocation ;
2. définir le control plane et le stockage des descripteurs de pairs ;
3. évaluer le transport UDP, NAT traversal et les relais ;
4. implémenter le contrôleur WireGuard avec une politique de privilèges
   dédiée ;
5. intégrer les migrations et les contrats OTA sans toucher à l’identité.
