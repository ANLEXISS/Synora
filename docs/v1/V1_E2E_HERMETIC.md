# V1 — scénario E2E logiciel hermétique

Le scénario M044 est local, factice et réexécutable. Il ne démarre aucun
service système, ne demande ni matériel, ni MediaMTX réel, ni modèle Vision.
Les services sont représentés par des frontières injectées : bus mémoire,
Core réel avec ses boucles bornées, faux Discovery via l’ingress clips, faux
Vision via le worker de clips, Actions réel avec un exécuteur contrôlé, et
MediaMTX factice derrière le client HTTP réel.

Le scénario vérifie successivement :

- réconciliation MediaMTX, suppression d’un chemin obsolète et idempotence ;
- mise en ligne de trois caméras et présence du résident de fixture ;
- clips connu, incertain et inconnu avec corrélations caméra/clip/track ;
- intrusion immédiate et incident unique pour l’inconnu ;
- demande Actions, résultat corrélé et persistant ;
- rejeu des messages sans nouvelle mutation d’incident ;
- échec Vision retryable, saturation de file et états terminaux ;
- arrêt propre, redémarrage Core et restauration de l’incident et des clips ;
- lecture et acquittement via les routes HTTP API sur un store persistant.

Commandes de preuve :

```text
GOFLAGS=-buildvcs=false go test ./cmd/synora-core ./cmd/synora-api -run 'TestV1Hermetic|TestV1HermeticIncident' -count=1
python3 -B -m unittest tools.tests.test_v1_e2e_hermetic
```

Les timestamps internes du produit restent ceux du contrat V1 ; les assertions
portent sur les états, corrélations, déduplications et transitions, pas sur une
valeur d’horloge de la machine. Cette preuve logicielle ne remplace pas la
qualification physique des caméras et reste distincte des tests de sécurité
hostile du jalon M045.
