# Synora V1 — matrice de pannes logicielles

Le harness `internal/qualification/failurematrix` injecte une coupure unique
à un point déterministe sur une charge connue, redémarre le hand-off logique,
et mesure le temps de reprise du harness. Il ne démarre aucun service réel et
ne remplace donc pas la qualification matérielle ou réseau.

| Couche | Coupure couverte | Garantie logicielle | Perte maximale démontrée |
| --- | --- | --- | --- |
| Core | avant/après persistance, publication et ACK | identité durable, replay stable et ACK idempotent | 1 élément seulement avant persistance |
| Bus | coupure avant émission ou avant ACK | transport at-least-once, doublon possible mais identité stable | 0 après persistance |
| Discovery | upload interrompu avant finalisation | aucun `.part` ou clip fantôme accepté | 1 upload non persisté |
| Vision | crash entre émission et ACK | reprise du job, événements rejouables sans nouvelle identité | 0 après persistance |
| StateStore | coupure pendant le hand-off | snapshot durable ou échec explicite, jamais d’état corrompu appliqué | 0 après snapshot valide |

La campagne de stabilité exécute 5 scénarios × 100 itérations avec une charge
de 128 éléments par scénario. Elle vérifie aussi la fermeture bornée sous
charge et la stabilité des identifiants rejoués. Les temps mesurés sont ceux
du harness logiciel local, pas une promesse de temps de boot matériel.

Limites restantes : aucune coupure électrique réelle, panne disque réelle,
perte radio/WireGuard ou qualification Rock 5T/Zero 3W n’est démontrée par
cette matrice.
