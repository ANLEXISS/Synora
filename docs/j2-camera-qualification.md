# J2 — Rapport de qualification caméra

Contrôle read-only exécuté le 2026-10-06 depuis le commit `0b6aae0` :

```text
make qualify-camera OUT=/tmp/synora-camera-qualification-current.json
decision=not_qualified cameras=2 active_paths=0
```

## Résultat

| Élément | Résultat |
| --- | --- |
| Caméras déclarées | `cam_03`, `cam_04` |
| Caméras actives et appairées | 0 |
| Identités caméra actives | 0 |
| Endpoints RTSP/ingress configurés | 0 |
| Chemins MediaMTX actifs | 0 |
| Santé Discovery | `503 degraded` |
| Qualification | `not_qualified` |

Motifs communs aux deux caméras : confiance réseau différente de `paired`,
identité active absente, endpoint absent et aucun chemin MediaMTX actif.

Le rapport est volontairement non bloquant pour le système : il ne tente ni
pairing, ni modification de `devices.yaml`, ni ajout de chemin MediaMTX. Une
caméra ne pourra passer à `transport_qualified` que lorsque l’identité active,
la confiance réseau, l’endpoint et le chemin MediaMTX seront tous observés.
Cette qualification transport ne suffira pas encore à clôturer J2 : il faudra
ensuite la preuve du parcours réel Discovery → Vision → Core, la reprise après
panne et la rétention sur le RK3588.

Le harnais central apporte désormais une **preuve E2E de transport et
résilience simulés** avec ingress Discovery HTTP local et worker de test sans
modèle. Ce jalon n’est pas une preuve d’inférence Vision et ne modifie pas la
décision ci-dessus : J2, J3 et J4 restent non validés.
