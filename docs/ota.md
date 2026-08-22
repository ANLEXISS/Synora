# OTA et rollback V1

Synora délègue l’intégrité des bundles et la sélection des slots à RAUC. Le
runtime ne désactive jamais la vérification de signature et ne marque un slot
comme bon qu’après le healthcheck readonly post-boot.

La commande installée est `synora-ota` :

```text
synora-ota status
synora-ota install /var/lib/synora/update/synora.raucb
synora-ota mark-good
synora-ota mark-bad
```

`mark-good` exécute d’abord `synora-boot-healthcheck run --readonly`. En cas
d’échec, RAUC n’est pas appelé et le slot reste non validé. L’unité
`synora-ota-mark-good.service` automatise cette validation après un boot
lorsque `/usr/bin/rauc` existe. Sur une machine de développement sans RAUC,
`synora-ota status` retourne `backend: unmanaged` et aucune simulation de slot
n’est effectuée.

La qualification matérielle du bootloader, des slots et du rollback reste à
faire sur la carte cible ; la frontière locale et son comportement fail-closed
sont testés dans `internal/ota`.
