# Parcours utilisateur V1

Le manifeste `V1_USER_FLOW_QUALIFICATION.json` vérifie les écrans, appels API
et décisions de périmètre qui composent le parcours utilisateur local :
onboarding, pairing, résidents/photos, incidents/clips, acquittement,
résolution, live à la demande, santé/version/erreurs et responsive.

```sh
python3 -B -m unittest tools.tests.test_v1_user_flow_qualification -v
python3 -B tools/v1_user_flow_qualification.py
cd synora-web && npm run build && npm run lint
```

Le contrôle est volontairement déclaratif et ne prétend pas être un test
navigateur. Le parcours local est couvert par le code existant et les tests
API/Go. L’accès distant est explicitement `blocked_external_adapter`: J14 a
livré le registre d’accès signé et l’architecture, mais la V1 locale n’a pas
d’implémentation WireGuard/netlink ni de client distant réel. La qualification
finale de ce parcours doit être rejouée avec une centrale, une caméra et un
client distant disponibles.
