# Déploiement local Synora

Le déploiement SynoraNet est préparé mais n'est pas exécuté par les tests de ce dépôt. Après installation, vérifier `/etc/synora/network.yaml`, créer le certificat local et conserver la PSK hors Git.

Pour le certificat de la centrale, inclure l'adresse AP :

```bash
make generate-local-cert TLS_IP=10.77.0.1 TLS_DNS=synora.local
```

L'API conserve le HTTP debug sur `:8080` et sert HTTPS sur `:8443`. Si le certificat manque, HTTP reste disponible et HTTPS est signalé `degraded` au lieu de bloquer l'API. Pour le pairing caméra, utiliser `https://10.77.0.1:8443` et accepter ou embarquer le certificat local de la centrale dans la caméra.

Commandes prototype proposées, non exécutées ici :

```bash
ip addr show synorabr0
curl -k -H "Authorization: Bearer $SYNORA_API_TOKEN" https://10.77.0.1:8443/api/system/health
curl -H "Authorization: Bearer $SYNORA_API_TOKEN" http://10.77.0.1:8080/api/streams
iw list
```
