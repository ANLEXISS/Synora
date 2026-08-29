# HTTPS natif de synora-api

`synora-api` peut servir le même handler applicatif sur deux listeners :

- HTTP `:8080` comme listener de redirection uniquement lorsque
  `server.https_enabled` vaut `true` ;
- HTTPS `:8443` comme listener applicatif lorsque TLS est configuré.

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

Pour SynoraNet, l'URL caméra est `https://10.77.0.1:8443`. Le certificat doit
donc contenir `10.77.0.1` dans ses SAN ; pairing, claim, événements et les
handlers API existants passent par cette adresse. L'ingress clip Discovery
reste le endpoint HTTPS historique `https://10.77.0.1:7070/vision` tant qu'un
reverse-proxy dédié n'est pas ajouté.

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

Lorsque HTTPS est activé et que les deux fichiers TLS sont valides, aucune
requête applicative ne reste servie en clair : le listener HTTP renvoie une
redirection permanente vers l’autorité HTTPS configurée, tandis que le
listener HTTPS sert directement l’application. Si HTTPS est activé sans
certificat ou clé régulière, le démarrage est refusé.

Les en-têtes `X-Forwarded-*` ne sont pas interprétés. Une terminaison TLS par
proxy doit donc transmettre le trafic vers un endpoint Synora explicitement
configuré dans une frontière de confiance ; Synora ne déduit jamais la
sécurité du transport d’un en-tête client non authentifié.
