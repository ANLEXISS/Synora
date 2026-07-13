# Runtime health et mode dégradé

Synora considère Discovery, le Vision Worker, le bus, le Core et Actions comme
des composants indépendants. Une capacité optionnelle indisponible ne doit pas
faire mourir le service parent.

`GET /api/system/health` retourne un rapport exploitable même si un composant
ne répond pas : HTTP 200 avec `status: degraded` et des statuts par service.
Les sondes RPC runtime utilisent un délai borné d'environ 400 ms ; une sonde
lente est signalée, pas propagée comme un blocage de cinq secondes.

Le rapport contient notamment `services`, `components`, `network`, `mediamtx`,
`disk`, `status` et `generated_at`. Les statuts `active`/`ok` signifient que la
capacité répond ; `degraded`, `inactive` ou `unavailable` doivent être lus avec
le champ `error` ou le message du composant.

`GET /api/runtime/diagnostics` expose le read-model runtime : état et danger
courants, chaînes ouvertes réelles/simulées, dernière activité, actions
bloquées et état Discovery/Actions. Les résultats inconnus ne sont pas
convertis en zéro.

## Réseau

Un échec hostapd est enregistré comme `network: degraded`. Bridge, hostapd,
dnsmasq et pare-feu sont initialisés indépendamment afin que Discovery puisse
continuer à recevoir les autres événements disponibles.

## Modèles

Les modèles RKNN attendus sont `arcface_w600k_r50.rknn`, `det_10g.rknn`,
`yolov8.rknn` et `weapon.rknn` sous `/var/lib/synora/models`. Un fichier absent,
invalide ou un runtime RKNN indisponible rend uniquement la capability
concernée indisponible. Le worker expose alors `/healthz` et `/capabilities` et
reste en `degraded`/`no_models` au lieu de redémarrer en boucle.

Les événements de diagnostic (`discovery.worker.crashed`, modèle manquant,
flapping, réseau dégradé) alimentent ce rapport mais ne créent pas de chaîne
de sécurité. Les répétitions sont limitées/coalescées.
