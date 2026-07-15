# Sécurité réseau SynoraNet

SynoraNet transporte des flux caméra, clips, secrets de pairing et des données
du domicile. Le modèle produit de `configs/network.yaml` est donc
« closed by default » : une caméra inconnue ne reçoit pas d’accès applicatif,
et les connexions sensibles sont initiées par la centrale.

## Mode normal

- SSID caché (`ignore_broadcast_ssid=1`). Cela réduit la visibilité dans les
  listes Wi-Fi, mais ne constitue pas une sécurité cryptographique et ne
  remplace ni WPA3, ni PMF, ni l’authentification applicative.
- WPA3-SAE uniquement, PMF obligatoire et isolation AP (`ap_isolate=1`). Aucun
  fallback WPA2 implicite n’est autorisé.
- Allowlist hostapd des MAC des caméras connues et activées. Une caméra
  inconnue reste bloquée ; aucune station autorisée signifie un réseau verrouillé.
- DHCP lié aux stations connues, avec baux statiques lorsqu’une adresse est
  déclarée. Si dnsmasq ne peut pas distinguer un client dans un environnement
  particulier, le firewall reste la barrière d’application et le health le
  signale.
- Politique `connection_policy.mode=central_initiated` : depuis
  `synorabr0`, seuls DHCP/DNS et les retours de connexions établies sont
  permis. Les ports applicatifs clients et médias sont refusés.
- Forwarding vers le LAN, Tailscale, Internet, les autres interfaces et les
  autres clients bloqué. La centrale peut initier les ports caméra déclarés.

## Fenêtre de pairing

Un administrateur ouvre explicitement une fenêtre courte avec :

```text
POST /api/devices/pairing/window/start
GET  /api/devices/pairing/status
POST /api/devices/pairing/window/stop
```

Pendant cette fenêtre seulement, le SSID peut redevenir visible si
`visibility.visible_during_pairing=true`, les stations en association peuvent
obtenir un accès réseau temporaire limité, et le claim caméra est disponible.
Le claim exige le `setup_token` présenté par le QR/code, consomme la session et
associe si possible `device_id`, série, MAC et adresse observée. La fenêtre
expire automatiquement ; les MAC pending sont supprimées, le SSID est caché,
le firewall revient à la politique normale et hostapd/dnsmasq sont rechargés.

La visibilité temporaire ne permet pas l’accès au dashboard complet ni aux
ports RTSP/WebRTC/HLS. Le firewall de pairing n’ouvre que les ports explicitement
déclarés pour le claim et les services minimaux.

## Identité et allowlist

Une MAC est une friction opérationnelle, pas une identité forte : elle peut
être usurpée. Une caméra confirmée est associée à son `device_id`, sa série,
sa MAC observée, son état `paired|pending|blocked` et, lorsqu’il sera disponible,
à une empreinte de clé publique ou un certificat device. Une MAC différente
doit être traitée comme un avertissement de sécurité et ne doit pas être
auto-approuvée.

Les métadonnées réseau sont persistées côté registre device, mais ne sont pas
renvoyées dans les vues publiques. Le health expose uniquement des états,
compteurs et politiques ; il ne contient jamais PSK, token, hash de token,
clé privée ou certificat privé.

## Limites restantes

Cette passe ne permet pas d’affirmer que les flux vidéo sont totalement
sécurisés tant que :

- HTTPS sur 8443 n’est pas réellement actif et validé au runtime ;
- l’ingress caméra n’exige pas encore systématiquement un secret device ou
  un certificat/mTLS par caméra ;
- RTSP clair reste accessible et n’est pas remplacé ou encapsulé ;
- la résistance à l’usurpation MAC n’est pas complétée par une identité
  cryptographique ;
- la persistance du driver Wi-Fi après reboot n’est pas validée séparément.

Les changements de cette passe n’effectuent ni reboot, ni changement de
driver/DKMS, ni exposition Internet, ni installation runtime.
