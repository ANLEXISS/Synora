# Jalon 25 — preuve de Release Candidate

L’audit J25 distingue la qualité logicielle démontrée localement des preuves
qui exigent une unité, un réseau, un pilote ou un tiers externe. Le rapport
reproductible est généré par :

```sh
python3 -B -m unittest tools.tests.test_v1_rc_audit -v
python3 -B tools/v1_rc_audit.py
```

## Décision

Le dépôt peut fournir une candidate locale identifiée, mais pas déclarer
`v1-rc1` tant que les gates obligatoires externes restent ouvertes. Le statut
est `software_rc_audited_external_gates_open`; il n’y a aucun P0 ouvert dans
l’audit du dépôt.

## Vision et terrain

Les tests de contrats et de résilience Vision sont disponibles. Aucune mesure
réelle de calibration, faux positif, faux négatif, pilote ou soak terrain n’est
présente dans ce worktree; ces valeurs restent `not_available`. Les fixtures
J21/J22 ne sont pas promues en résultats physiques.

## Sécurité et récupération

Les gates locales de sessions, autorisations, REST/WebSocket, CORS, CSRF,
rate-limiting et limites d’upload sont couvertes par les suites existantes et
les statuts précédents. Backup/restore, OTA centrale, OTA caméra et rollback
sont verts en simulation ciblée; redémarrage physique et rollback sur unité
restent externes.

## P0/P1

- P0 ouvert : aucun identifié dans l’audit local.
- P1/gates externes : matériel cible, accès distant réel, calibration Vision et
  preuves réglementaires; ils sont listés, non masqués et ne sont pas reclassés
  silencieusement.
