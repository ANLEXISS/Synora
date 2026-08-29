# Synora Camera Pairing

Cette passe implémente uniquement l’appairage des caméras Synora. Matter et
Thread restent affichés comme fonctionnalités à venir côté webapp et ne sont
pas activés par l’API.

## Flux

1. Un administrateur scanne le QR code ou colle son JSON.
2. L’API valide le payload et ouvre une session locale persistée de 10 minutes.
3. La caméra prouve la possession de la clé imprimée et de sa clé privée par
   une signature Ed25519 liée à son identité, son MAC et un timestamp borné.
4. L’administrateur choisit le nom, la pièce de la topologie et l’état activé.
5. La confirmation crée le device dans `devices.yaml` via Core et persiste
   uniquement l’identité publique et le dérivé de transport protégé.
6. L’interface relit `/api/devices` et vérifie que le nouvel `device_id` est
   présent avant d’afficher le succès.

Sur SynoraNet, l'API de pairing est joignable via
`https://10.77.0.1:8443`. L'upload de clips utilise encore l'ingress Discovery
HTTPS existant `https://10.77.0.1:7070/vision` ; la caméra doit accepter le
certificat local de la centrale après pairing.

## Payload QR

```json
{
  "type": "synora.camera",
  "version": 1,
  "device_id": "cam_01",
  "serial": "SYN-CAM-000001",
  "model": "synora-cam-fe",
  "setup_token": "one_time_secret",
  "public_key": "<base64-ed25519-public-key>"
}
```

`device_id` doit être en minuscules et ne contenir que des lettres, chiffres,
`_` ou `-`. `setup_token` et `public_key` sont obligatoires pour le protocole
sécurisé et leur longueur est limitée. Le token n’est jamais conservé en
clair. Un device actif est refusé ; un device explicitement révoqué ne peut
être réinitialisé qu’avec `reset: true`.

## API

Les routes d’administration ci-dessous sont admin-only et répondent avec
`Cache-Control: no-store` via le middleware API. Le claim caméra est une
exception étroite, uniquement ouvert pendant la fenêtre réseau de pairing et
protégé par la preuve de possession.

### Capacités

`GET /api/devices/pairing/capabilities`

```json
{
  "synora_camera": {
    "available": true,
    "qr_scan": true,
    "manual_code": true
  }
}
```

### Start

`POST /api/devices/pairing/synora-camera/start`

Le body accepte `qr_payload` comme objet JSON, ou `raw_code` comme chaîne JSON.
La réponse contient `session_id`, `device_id`, `serial`, `model`,
`status: "ready_to_confirm"` et `expires_at`. Le `setup_token` n’est jamais
renvoyé et n’est jamais écrit dans les logs.

### Confirm

`POST /api/devices/pairing/synora-camera/confirm`

```json
{
  "session_id": "…",
  "name": "Caméra entrée",
  "node_id": "zoneA.L0.entree",
  "enabled": true
}
```

La confirmation est acceptée après la preuve de possession. Le device créé est
de type `camera`, avec `vendor: synora`, le modèle et le serial du QR,
`pairing_method: synora_qr`, `trusted: true` et un réseau `paired`. Le fichier
est écrit atomiquement avec sauvegarde par le registre Core. La session est
consommée après succès.

### Claim caméra (préparation)

`POST /api/devices/pairing/synora-camera/claim` est la surface locale de claim
de la caméra. Elle exige `device_id`, `setup_token`, `mac`, `public_key`,
`timestamp` et `signature`. La signature est à usage unique et le timestamp
est borné. Le dérivé de transport est calculé sans exposer le token.

### Révocation et reset

`POST /api/devices/pairing/synora-camera/revoke` désactive la caméra, ferme son
accès réseau et révoque son identité persistante. `POST
/api/devices/pairing/synora-camera/reset` applique le même verrouillage ; un
nouveau pairing avec `reset: true` et une nouvelle clé publique augmente la
génération d’identité et remplace le dérivé de transport.

## Sécurité et redaction

Le backend conserve uniquement un hash du `setup_token` et, après preuve, un
dérivé de transport dans des fichiers protégés en `0600`. Les sessions
expirées sont supprimées opportunément et une session confirmée est consommée.
Les claims invalides sont limités par caméra et adresse d’origine. Une caméra
non appairée, désactivée ou révoquée ne peut ni publier de clip ni obtenir une
URL live autorisée.

## Limites restantes

- Le scan utilise `BarcodeDetector` natif ; les navigateurs qui ne le
  fournissent pas utilisent la saisie manuelle.
- Matter/Thread n’est pas implémenté.
