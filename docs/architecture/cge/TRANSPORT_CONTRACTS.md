# Contrats de transport CGE

Les surfaces actuelles sont cataloguées dans `transports.yaml` : Bus,
frontières internes Core→CGE, RPC, HTTP et WebSocket. Chaque surface possède
une direction, une version, des contrats de requête/réponse/erreur, une règle
de redaction, une borne, une pagination et une autorité.

Les sorties CGE sont read-only et au plus advisory. Elles ne sont pas des
`ActionRequest`, ne publient pas de message `command` et ne sont pas utilisables
directement par une automation. Toute nouvelle surface de transport doit
ajouter son ID, ses contrats et ses tests avant implémentation.

Les erreurs publiques utilisent seulement les codes catalogués ; les détails
internes peuvent rester dans `Unwrap` mais ne doivent pas transporter un
identifiant brut, un secret ou un payload sensible.
