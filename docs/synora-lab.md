# Synora Lab

Synora Lab est un module fonctionnel du produit Synora, destiné aux
utilisateurs administrateurs. Ce n’est pas un mock ni un simple outil de
développement : il permet de vérifier qu’une installation réelle réagit comme
prévu.

## Accès et sécurité

Lab est disponible dans la webapp pour les utilisateurs autorisés et requiert
la permission `lab:use`, accordée au rôle `admin`. Les résidents et invités ne
peuvent ni injecter d’observation, ni consulter l’historique de validation.
Les routes Lab refusent aussi les requêtes lorsque
`features.synora_lab_enabled` est désactivé.

## Capacités

Le module permet notamment de :

- tester des caméras et des emplacements ;
- injecter des observations contrôlées dans le pipeline normal ;
- construire et exécuter des scénarios CGE ;
- vérifier les chaînes, le score de danger et le `DangerRuntime` ;
- valider les automations et l’Action Policy ;
- observer les notifications et résultats d’actions en mode sûr ;
- comparer résultat attendu et résultat observé ;
- consulter et effacer l’historique des validations ;
- ouvrir le détail CGE sans exposer de secret.

La page web utilise les routes `/api/cge/validation/*`. Les routes
`/api/lab/validation/*` sont leurs noms produit ; les anciens chemins restent
disponibles pour compatibilité.

## Marquage des événements

Une validation Lab est distincte d’une simulation développeur. Elle est
marquée par les métadonnées contrôlées :

```text
source_type=validation
lab_source_type=synora_lab
validation=true
generated_by=synora_lab
test_mode=controlled_real_test
```

`source_type=validation` est conservé comme valeur CGE historique pour ne pas
casser les consommateurs existants ; `lab_source_type` identifie explicitement
la validation produit Synora Lab.

Elle traverse Core, CGE, Event Chains, automations et actions comme une
observation contrôlée, sans être présentée comme un événement naturel de la
maison. `learn=false` reste le défaut ; l’apprentissage est un choix explicite
de l’administrateur.

## Différence avec la simulation développeur

Les routes `/api/simulation/*` et les scénarios de `tools/dev/` servent à la
simulation développeur et sont désactivés par défaut par
`features.dev_simulation_enabled=false`. Ils conservent leurs contrats pour
compatibilité et tests, mais ne sont pas l’interface produit Lab et ne sont
pas installés comme services.

Le compagnon CLI historique `tools/dev/synora-lab` reste conservé pour les
opérations et validations hors web ; il ne remplace pas les contrôles
d’autorisation de l’API et ne doit pas recevoir de secret en argument ou dans
les logs.

## Limites

Lab ne contourne pas l’authentification, ne modifie pas les drivers, ne
redémarre pas les services et ne prétend pas remplacer un test de production
longue durée. Les résultats sont persistés comme historique de validation
lorsque le flux le prévoit, tandis que les événements et chaînes conservent
leur historique normal.
