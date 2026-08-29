# V1 — qualification sécurité hostile

M045 exécute la matrice hostile hors ligne, uniquement sur des fixtures et
serveurs locaux de test. Aucun scanner ni cible extérieure n’est utilisé.

La couverture comprend :

- usurpation et rejeu des caméras, services et claims de pairing ;
- bruteforce login/pairing, énumération, IDOR et séparation des permissions ;
- CSRF, Origin WebSocket, session fixation, révocation et rotation de secret ;
- JSON volumineux ou profondément imbriqué, identifiants encodés et traversal ;
- archives hostiles, symlinks, permissions, logs biométriques et dernier admin ;
- plages média abusives, client WebSocket lent, files et limites de ressources.

Deux défauts ont été corrigés pendant le jalon :

1. les identifiants de route acceptaient `..` après décodage URL ; ils sont
   maintenant rejetés au même titre que `.` et les séparateurs ;
2. une Range HTTP multi-segments invalide pouvait être servie comme réponse
   complète par `ServeContent` ; elle est refusée en `416 Requested Range Not
   Satisfiable` avant ouverture du fichier.

Chaque correction possède un test de non-régression. Les autres frontières
reposent sur les tests de sécurité V1 existants, référencés par l’oracle de
qualification `tools/tests/test_v1_hostile_security.py`.

Preuves :

```text
GOFLAGS=-buildvcs=false go test ./cmd/synora-api ./internal/api ./internal/security ./internal/backup ./internal/discovery/ingress ./internal/mediamtx -count=1
python3 -B -m unittest tools.tests.test_v1_hostile_security
```
