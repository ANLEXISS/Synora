# Configuration runtime V1

`internal/runtimeconfig` est la source commune des chemins, endpoints et
timeouts utilisés par les processus Synora. `Load` reçoit un lecteur
d’environnement injecté, applique les valeurs par défaut de production, puis
valide l’ensemble avant le démarrage.

Les chemins principaux sont `SYNORA_BUS`, `SYNORA_VISION_WORKER_SOCKET`,
`SYNORA_STATE_PATH`, `SYNORA_CLIP_ROOT` (ou l’ancien alias
`SYNORA_CLIP_DIR`), `SYNORA_FACE_DATA_ROOT`, `SYNORA_MODEL_ROOT`,
`SYNORA_CONFIG_DIR`, `SYNORA_BACKUP_ROOT`, `SYNORA_WEB_ROOT`,
`SYNORA_SESSION_STORE`, `SYNORA_IDENTITY_REGISTRY`, `SYNORA_OTA_JOURNAL` et
`SYNORA_CAMERA_OTA_ROOT`. Les fichiers de configuration Core/API sont dérivés
de `SYNORA_CONFIG_DIR` sauf surcharge explicite par leur variable dédiée.

Les endpoints sont `SYNORA_HTTP_ADDR`, `SYNORA_HTTPS_ADDR`,
`SYNORA_VISION_HEALTH_ADDR`, `SYNORA_VISION_HTTPS_ADDR` et
`SYNORA_MEDIAMTX_RTSP_URL`. Les ports peuvent être éphémères (`:0` ou
`127.0.0.1:0`) dans les tests ; l’URL MediaMTX doit rester une URL `rtsp` avec
un hôte.

Les délais acceptent la syntaxe Go de `time.ParseDuration` :
`SYNORA_BUS_CONNECT_TIMEOUT`, `SYNORA_BUS_RPC_TIMEOUT`,
`SYNORA_HTTP_READ_TIMEOUT`, `SYNORA_HTTP_WRITE_TIMEOUT`,
`SYNORA_HTTP_IDLE_TIMEOUT`, `SYNORA_HTTP_HEADER_TIMEOUT`,
`SYNORA_SHUTDOWN_TIMEOUT` et `SYNORA_VISION_TIMEOUT`.

Une valeur vide utilise le défaut ; une valeur relative, un endpoint invalide,
une URL non-RTSP ou un délai non positif provoque une erreur exploitable avant
la création des clients et serveurs. Les tests de `internal/runtimeconfig`
utilisent uniquement `t.TempDir` et des ports éphémères.
