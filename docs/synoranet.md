# SynoraNet

SynoraNet est le réseau local isolé destiné aux futures caméras Synora. La centrale utilise `10.77.0.1/24`, distribue `10.77.0.50` à `10.77.0.200` et publie `synora.local`, `hub.synora.local`, `api.synora.local` et `rtsp.synora.local` vers `10.77.0.1`.

La configuration installée est `/etc/synora/network.yaml` (modèle : `configs/network.yaml`). Sans fichier, ou sans interface renseignée, le défaut est `enabled: false` : Core, API et webapp restent utilisables.

La PSK est lue depuis `ap.passphrase_file`. Si le fichier manque, Discovery génère une valeur aléatoire de longueur suffisante, crée le répertoire en `0700` et le secret en `0600`. Une valeur de moins de 16 caractères est refusée. La valeur n'est jamais écrite dans les logs ni dans Git.

Le modèle de sécurité est `security.mode: wpa3`, avec SAE uniquement, PMF obligatoire et isolation client. `wpa2-wpa3-transition` doit être configuré explicitement et est dégradé ; `wpa2` reste réservé au legacy/dev. L'ancien `ap.wpa` est accepté et mappé lors du chargement.

Le gestionnaire tente le canal 36 en 5 GHz (`hw_mode=a`, largeur 20 MHz). En cas d'échec hostapd, il active le canal 6 en 2,4 GHz avec le message `5 GHz failed, running 2.4 GHz fallback`. Si les deux profils échouent, le réseau est `unavailable` mais Discovery ne redémarre pas en boucle et ne publie pas d'événement de sécurité CGE.

L'état est écrit dans `/run/synora/network-status.json` et exposé dans les rapports runtime (`synoranet`, `ap_5ghz`, `ap_2ghz`, `dhcp`, `dns`, `wifi_security`, `network_isolation`, `firewall`). Le firewall utilise une table/chaînes dédiées, autorise seulement les ports configurés vers la centrale et bloque le forwarding de `synorabr0` vers le LAN, Tailscale, Internet et les autres interfaces. SynoraNet n'impose pas de NAT Internet et ne remplace pas les règles globales non-Synora.

Pour diagnostiquer un 5 GHz absent, vérifier `iw list`, `country_code`, les limitations `NO-IR`/DFS et le pilote. Pour hostapd, consulter les journaux de Discovery et `/run/synora/hostapd-*.conf`. Pour dnsmasq, rechercher un conflit de processus DHCP/DNS et inspecter `/run/synora/dnsmasq-synoranet.conf`.
