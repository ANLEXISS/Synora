# SynoraNet

SynoraNet est le réseau local isolé destiné aux futures caméras Synora. La centrale utilise `10.77.0.1/24`, distribue `10.77.0.50` à `10.77.0.200` et publie `synora.local`, `hub.synora.local`, `api.synora.local` et `rtsp.synora.local` vers `10.77.0.1`.

La configuration installée est `/etc/synora/network.yaml` (modèle : `configs/network.yaml`). Sans fichier, ou sans interface renseignée, le défaut est `enabled: false` : Core, API et webapp restent utilisables.

La PSK est lue depuis `ap.passphrase_file`. Si le fichier manque, Discovery génère une valeur aléatoire, crée le répertoire en `0700` et le secret en `0600`. La valeur n'est jamais écrite dans les logs ni dans Git.

Le gestionnaire tente le canal 36 en 5 GHz (`hw_mode=a`, largeur 40 MHz). En cas d'échec hostapd, il active le canal 6 en 2,4 GHz avec le message `5 GHz failed, running 2.4 GHz fallback`. Si les deux profils échouent, le réseau est `unavailable` mais Discovery ne redémarre pas en boucle et ne publie pas d'événement de sécurité CGE.

L'état est écrit dans `/run/synora/network-status.json` et exposé dans les rapports runtime (`synoranet`, `ap_5ghz`, `ap_2ghz`, `dhcp`, `dns`). SynoraNet n'impose pas de NAT Internet et ne remplace pas la politique pare-feu existante.

Pour diagnostiquer un 5 GHz absent, vérifier `iw list`, `country_code`, les limitations `NO-IR`/DFS et le pilote. Pour hostapd, consulter les journaux de Discovery et `/run/synora/hostapd-*.conf`. Pour dnsmasq, rechercher un conflit de processus DHCP/DNS et inspecter `/run/synora/dnsmasq-synoranet.conf`.
