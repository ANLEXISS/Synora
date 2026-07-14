# Synora Camera Pairing

Cette passe implémente uniquement l’appairage des caméras Synora. Matter et
Thread restent affichés comme fonctionnalités à venir côté webapp et ne sont
pas activés par l’API.

## Flux

1. Un administrateur scanne le QR code ou colle son JSON.
2. L’API valide le payload et ouvre une session mémoire de 10 minutes.
3. L’administrateur choisit le nom, la pièce de la topologie et l’état activé.
4. La confirmation crée le device dans `devices.yaml` via Core.
5. L’interface relit `/api/devices` et vérifie que le nouvel `device_id` est
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
  "setup_token": "one_time_secret"
}
```

`device_id` doit être en minuscules et ne contenir que des lettres, chiffres,
`_` ou `-`. `setup_token` est obligatoire et sa longueur est limitée. Un
device existant est refusé ; le remplacement n’est pas implémenté.

## API

Toutes les routes ci-dessous sont admin-only et répondent avec
`Cache-Control: no-store` via le middleware API.

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

Le device créé est de type `camera`, avec `vendor: synora`, le modèle et le
serial du QR, `pairing_method: synora_qr`, `trusted: true` et un statut initial
`pending`. Le fichier est écrit atomiquement avec sauvegarde par le registre
Core. La session est consommée après succès.

### Claim caméra (préparation)

`POST /api/devices/pairing/synora-camera/claim` existe comme point d’entrée
préparatoire. Il vérifie le hash du `setup_token` en mémoire et marque la
session `device_seen`, puis répond `accepted`. Il reste admin-only dans cette
passe, car l’authentification dédiée de la future caméra légère n’est pas
encore définie.

## Sécurité et redaction

Le backend conserve uniquement un SHA-256 du `setup_token` dans le store
volatile. Les sessions expirées sont supprimées opportunément et une session
confirmée est consommée. Les routes Devices redigent `setup_token`, secrets,
credentials, passwords et tokens, y compris dans les objets imbriqués.

## Limites restantes

- Le scan utilise `BarcodeDetector` natif ; les navigateurs qui ne le
  fournissent pas utilisent la saisie manuelle.
- La caméra légère et son authentification de claim ne sont pas livrées.
- Matter/Thread n’est pas implémenté.
- Aucun mode replace n’est disponible.
