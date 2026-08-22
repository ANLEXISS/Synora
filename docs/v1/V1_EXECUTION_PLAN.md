# Synora V1 — plan d’exécution

## Règles d’exécution

Les 25 jalons sont traités dans l’ordre, par groupes de cinq. Chaque jalon
utilise une branche et un worktree dédiés, des commits atomiques, des tests
verts et un push sans force-push. Après les jalons 05, 10, 15, 20 et 25, la
branche s’arrête pour bilan et confirmation explicite.

Les invariants métier sont ceux du plan V1 utilisateur : Core reste l’unique
propriétaire de l’état dynamique, Engine décide sans posséder la persistance,
CGE reste advisory/shadow, les seuils de présence et decay restent inchangés,
la persistance précède toute publication confirmée, et aucun contrat public ne
révèle de chemin interne ou de donnée biométrique.

## Périmètre

V1 couvre la centrale Rock 5 ITX, trois caméras Zero 3W, le fonctionnement
local-first, les clips HTTPS et le live à la demande, Vision résident/incertain/
inconnu, présence, tracking, incidents, clips, REST/WebSocket, webapp,
WireGuard, pairing, rétention, OTA/rollback et qualification logicielle et
matérielle. Matter, Thread, ZigBee, marketplace, application mobile native,
multi-centrales, nouvelles fonctions CGE, décision cloud et traitement vidéo
continu restent hors périmètre.

## Jalons

1. Baseline d’exécution et branche durable.
2. Contrat de livraison durable.
3. Outbox persistante.
4. Dispatcher, ACK et reconnexion.
5. Replay, ordre et intégration outbox.
6. États de récupération Core.
7. StateStore et pannes de stockage.
8. Discovery et uploads résilients.
9. Résilience du worker Vision.
10. Matrice de pannes logicielles.
11. Threat model et identité des appareils.
12. Pairing et authentification caméra.
13. Sécurité API et webapp.
14. Accès distant et transfert de propriété.
15. Validation de sécurité.
16. Politiques de rétention.
17. Données sensibles et droits utilisateur.
18. Sauvegarde et restauration.
19. OTA de la centrale.
20. OTA caméra et récupération.
21. Harness de qualification hardware.
22. Qualification caméra et réseau.
23. Parcours utilisateur V1.
24. Release engineering et industrialisation.
25. Release Candidate.

## Gates obligatoires

Chaque jalon exige `git diff --check`, tests ciblés, vet ciblé, race ciblé si
Go modifié, tests Python concernés, worktree propre et branche poussée. Aux
jalons 05, 10, 15, 20 et 25 : `go list ./...`, `go test ./... -count=1`,
`go test ./... -shuffle=on -count=3`, `go vet ./...`, race globale bornée à
300 secondes et tous les tests Python Vision.

Une preuve matérielle, réglementaire ou externe absente doit rester marquée
comme bloquée ; elle ne doit jamais être inventée.
