# HTTPS natif de synora-api

`synora-api` peut servir le même handler HTTP sur deux listeners :

- HTTP `:8080` pour le debug/local ;
- HTTPS `:8443` quand `server.https_enabled` vaut `true`.

La webapp statique, l'API et `/api/ws` utilisent le même handler. Le
WebSocket du frontend choisit automatiquement `ws://` en HTTP et `wss://` en
HTTPS. Aucun token n'est placé dans l'URL du WebSocket.

## Configuration

Dans `/etc/synora/security.yaml` :

```yaml
server:
  http_addr: ":8080"
  https_enabled: false
  https_addr: ":8443"
  tls_cert_file: "/etc/synora/tls/synora.crt"
  tls_key_file: "/etc/synora/tls/synora.key"
  redirect_http_to_https: false
```

Les variables `SYNORA_HTTP_ADDR`, `SYNORA_HTTPS_ENABLED`,
`SYNORA_HTTPS_ADDR`, `SYNORA_TLS_CERT_FILE` et `SYNORA_TLS_KEY_FILE`
permettent de surcharger la configuration au lancement.

## Certificat local

Depuis le repo :

```bash
make generate-local-cert TLS_IP=100.80.170.47 TLS_DNS=rock-5-itx
```

La cible génère un certificat auto-signé avec les SAN `127.0.0.1`, l'IP
fournie, `localhost`, le DNS fourni et `synora.local`. Elle refuse d'écraser
des fichiers TLS existants. La clé est `root:synora`, mode `0640`; le certificat
est `root:synora`, mode `0644`.

Un navigateur affichera un avertissement pour ce certificat auto-signé. Pour
un usage local régulier, importer le certificat ou sa CA dans le magasin de
confiance du navigateur. `curl` de validation peut utiliser `-k` uniquement
pour ce certificat de développement.

HTTPS rend aussi le contexte navigateur et les cookies `Secure` explicites.
Tailscale ou WireGuard protègent le transport réseau, mais ne remplacent pas
HTTPS pour le contexte navigateur ni l'authentification applicative.

La redirection HTTP vers HTTPS est réservée à une passe ultérieure ; le champ
`redirect_http_to_https` est conservé dans la configuration mais reste sans
effet tant qu'il n'est pas activé par une implémentation dédiée.
