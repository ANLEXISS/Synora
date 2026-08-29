# M048 — validation caméras, pairing et live

Cette procédure est préparée pour trois caméras Synora réelles. La dépendance
M047 (Rock 5T) est différée ; M048 ne peut donc pas être déclaré réussi avant
la validation de la centrale et la disponibilité des trois unités. Aucun
fixture ou Rock 5 ITX distant ne constitue une preuve matérielle M048.

## Préconditions

- centrale validée et versionnée ;
- trois caméras identifiées par numéro de série, firmware et identité
  matérielle ;
- réseau local isolé, centrale et caméras appairées uniquement par le protocole
  V1 ;
- MediaMTX local et configuration sauvegardée ;
- sortie dédiée sous `docs/v1/validation/runs/M048-<date>/`, permissions 0700,
  sans clé imprimée, secret, image faciale ou contenu biométrique.

## Pairing et identité

Pour chaque caméra, puis pour deux demandes simultanées :

1. imprimer et scanner le code de pairing à usage unique ;
2. accepter la bonne clé et vérifier l’association à la bonne caméra ;
3. rejeter une clé fausse, expirée, réutilisée ou destinée à une autre caméra ;
4. interrompre le pairing, redémarrer la centrale, puis reprendre sans caméra
   semi-autorisée ;
5. rejeter une caméra déjà liée et vérifier qu’aucune seconde association n’est
   créée ;
6. renommer et déplacer chaque caméra, puis vérifier la continuité de son
   identité et de son historique ;
7. révoquer puis resetter une caméra et vérifier l’impossibilité d’accès aux
   flux, clips et commandes tant qu’un nouveau pairing n’est pas terminé.

Les preuves conservent uniquement des identifiants hachés, statuts, codes
d’erreur et timestamps RFC3339. Une clé ou un secret ne doit jamais apparaître
dans un log, un rapport ou une capture d’écran.

## Réseau, firmware et reprise

Pour les trois caméras, vérifier le firmware et le checksum du paquet avant
installation. Rejouer après :

- perte et retour du Wi-Fi ;
- changement d’adresse IP ;
- absence temporaire de centrale ;
- reboot de la caméra ;
- reboot de la centrale ;
- caméra déjà mise à jour ou paquet corrompu.

Une caméra hors ligne doit rester `offline/pending` et reprendre sans
duplication à son retour. Une perte réseau ne doit pas effacer le spool clips,
l’identité, l’appairage ou la séquence `clip_index`.

## Clips et live

Avec trois sources, produire un clip par scénario : succès, interruption,
retry, checksum invalide et ordre de chunks invalide. Vérifier côté centrale :

- un seul clip logique et un `clip_index` strictement ordonné ;
- absence de fichier temporaire après succès ou abandon ;
- checksum et taille finaux ;
- reprise idempotente après reboot ;
- ingestion des deux autres caméras pendant la panne d’une caméra.

Vérifier ensuite le live MediaMTX : chemin autorisé et non devinable, accès
autorisé seulement pour la caméra appairée, refus après révocation, retour
degraded sans flux et retour ready après réconciliation. Aucun secret d’URL
ne doit être journalisé.

## PIR et Doppler

Tester uniquement les déclencheurs V1 prévus, séparément puis en concurrence.
Vérifier anti-rejeu, déduplication, timestamp, caméra source et absence de
décision IA ou métier ajoutée par le capteur. Toute capacité non définie par
le contrat V1 reste `not_attached`.

## Contrôles logiciels préparatoires

Avant la campagne physique, exécuter dans le worktree de release :

```sh
GOFLAGS=-buildvcs=false go test ./internal/discovery/... ./internal/cameraota ./internal/mediamtx ./internal/ingest -count=1
GOFLAGS=-buildvcs=false go test -race ./internal/discovery/... ./internal/cameraota ./internal/mediamtx ./internal/ingest -count=1
python3 -B -m unittest tools.tests.test_v1_camera_network_qualification
```

Tout défaut reproductible observé sur le matériel doit d’abord devenir un
test logiciel avec double, puis être corrigé et rejoué.

## Rapport et décision

Le rapport local doit contenir au minimum :

```json
{
  "schema_version": 1,
  "milestone": "M048",
  "physical_qualification": "pass|fail|blocked_no_target_results",
  "camera_count": 3,
  "pairing": {},
  "network_recovery": {},
  "clips": {},
  "live": {},
  "sensors": {"pir": "not_attached", "doppler": "not_attached"},
  "secrets_stored_in_report": false
}
```

Sans les résultats réels des trois caméras, le seul statut valide est
`blocked_no_target_results`. La préparation de cette procédure ne constitue
pas une validation M048.
