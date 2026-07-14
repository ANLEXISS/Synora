# Streaming caméra

Une caméra publie son flux vers MediaMTX, par exemple `rtsp://10.77.0.1:8554/cam_03`. La webapp ne lit jamais cette URL RTSP directement : les navigateurs n'ont pas de lecteur RTSP standard.

MediaMTX est configuré pour RTSP TCP (`8554`), HLS (`8888`) et WebRTC (`8889`). `GET /api/streams` et `GET /api/streams/{device_id}` retournent notamment :

```json
{"device_id":"cam_03","rtsp_publish_url":"rtsp://10.77.0.1:8554/cam_03","webrtc_url":"http://10.77.0.1:8889/cam_03/whep","hls_url":"http://10.77.0.1:8888/cam_03/index.m3u8","status":"unknown","live_available":true}
```

Les bases WebRTC/HLS sont configurables dans `network.yaml` ou par `SYNORA_WEBRTC_BASE_URL` et `SYNORA_HLS_BASE_URL`. Si elles sont vides, l'UI affiche « Live indisponible : WebRTC/HLS non configuré ».

L'authentification MediaMTX actuelle reste générique sur le réseau local. L'identité par caméra (username/password ou token) reste un TODO avant toute exposition hors SynoraNet. Une caméra connectée mais sans live indique souvent un mauvais chemin, un flux non publié, MediaMTX injoignable ou une URL live non configurée.
