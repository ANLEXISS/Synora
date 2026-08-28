# Synora V1 — état courant de la baseline

Ce document constitue l’inventaire M001 du dépôt basé sur
`integration/synora-v1-baseline-20260817`. Il décrit les implémentations déjà
présentes et leurs oracles de test ; il ne constitue pas une validation des
jalons de durcissement ultérieurs.

## Socle et propriété de l’état

| Capacité | Implémentation | Oracle principal | État M001 |
| --- | --- | --- | --- |
| Propriété de l’état métier | `cmd/synora-core`, `internal/state`, `internal/stateapply` | `cmd/synora-core/*_test.go`, `internal/state/*_test.go`, `internal/stateapply/*_test.go` | Présent ; à durcir sur les jalons dédiés |
| Événements, snapshots et récupération | `internal/event`, `internal/snapshot`, `internal/recovery` | tests des mêmes packages | Présent |
| Bus local | `internal/bus`, `cmd/synora-bus` | `internal/bus/server_test.go` | Présent |
| Configuration et validation | `internal/configfile`, `configs/` | `internal/configfile/*_test.go`, tests des consommateurs | Présent ; centralisation à vérifier en M003 |

## Flux V1 produit

| Capacité | Implémentation | Oracle principal | État M001 |
| --- | --- | --- | --- |
| Découverte et ingestion caméra | `internal/discovery`, `internal/discovery/ingress`, `internal/ingest` | tests `internal/discovery/**/*_test.go`, `internal/ingest/*_test.go` | Présent |
| Runtime Vision et tracking | `internal/discovery/vision`, `services/vision-worker/` | `internal/discovery/vision/*_test.go`, `services/vision-worker/tests/` | Présent ; validation matériel hors CI |
| Appairage caméra | `internal/security/pairing.go`, `internal/discovery/network/pairing.go`, `cmd/synora-api/synora_camera_pairing.go` | tests pairing API/security/network | Présent |
| Tracking et présence | `cmd/synora-core/tracking.go`, `internal/state/tracking.go` | `cmd/synora-core/tracking_test.go`, `internal/state/tracking_test.go`, `internal/state/presence_decay_test.go` | Présent |
| Mode de sécurité et calcul métier | `cmd/synora-core`, `internal/engine/danger`, `cmd/synora-api/security_mode.go` | tests core, `internal/engine/danger/*_test.go`, API/security-mode tests | Présent |
| Incidents | `cmd/synora-core/incidents.go`, `internal/state/incidents.go`, `cmd/synora-api/incidents.go` | tests incidents core/state/API, `cmd/synora-core/v1_integration_test.go` | Présent |
| Clips et corrélation incident | `cmd/synora-core/clips.go`, `internal/state/clips.go`, `cmd/synora-api/clips.go` | tests clips core/state/API | Présent |
| WebSocket temps réel | `cmd/synora-api/ws.go` | `cmd/synora-api/ws_realtime_test.go`, `ws_simulation_test.go` | Présent |
| API et serveur WebApp | `cmd/synora-api`, `internal/api`, `synora-web/` | tests Go API et tests `synora-web/src/lib/*.test.ts` | Présent ; parcours complet à qualifier ultérieurement |
| Résidents et dataset facial local | `internal/facedataset`, `internal/facestore`, handlers face/photo API | tests facedataset/facestore et tests API face/photo | Présent ; runtime réel à qualifier ultérieurement |

## Durabilité, sécurité et opérations

| Capacité | Implémentation | Oracle principal | État M001 |
| --- | --- | --- | --- |
| Outbox, livraison et idempotence des actions | `internal/outbox`, `internal/delivery`, `internal/actions` | tests des trois packages | Présent |
| Authentification, autorisation et redaction | `internal/security`, `cmd/synora-api`, `internal/rpc` | tests security/API/RPC | Présent |
| Backup, rétention et restauration | `internal/backup`, `internal/retention`, `internal/state` | tests backup/retention/state | Présent |
| OTA centrale et caméra | `internal/ota`, `internal/cameraota`, `cmd/synora-ota`, `cmd/synora-camera-ota` | tests OTA et camera OTA | Présent |
| Santé runtime et diagnostics | `internal/manager`, `internal/boothealth`, `cmd/synora-api/runtime_diagnostics.go` | tests manager/boothealth/API diagnostics | Présent |

## Contrôles de baseline

La baseline est propre côté Git dans le worktree dédié M001. Les fichiers Go
non formatés détectés par `gofmt -l` ont été normalisés dans le commit
`chore(v1): normalize Go formatting`; aucun changement fonctionnel volontaire
n’y est associé. Les deux TODO de production recensés sont documentés dans le
code : migration de l’ancien type `automation.action` et extension future de
l’engine cognitif. Ils ne bloquent pas M001.

`WebHealth` (`internal/api/web.go`) est un état local du serveur Web ; il ne
duplique pas le contrat `RuntimeHealth`, exposé par `pkg/contract` et consommé
par l’API. Les recherches de contrats concurrents et d’imports obsolètes n’ont
pas révélé d’incohérence structurelle bloquant la compilation ou les tests.

Les chemins de tests observés sont temporaires ou injectés. Le répertoire
réel de l’interface est `synora-web/` (et non `webapp/`) ; cette différence de
nom doit être conservée dans les commandes d’exécution locales.

## Limites explicites

Cet inventaire ne revendique pas la qualification d’une caméra physique, d’un
modèle RKNN, de MediaMTX installé, de systemd, du réseau extérieur ni d’un
parcours matériel. Ces sujets restent soumis aux jalons et gates prévus par le
plan maître.
