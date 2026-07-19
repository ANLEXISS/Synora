# Migrations configuration/data

Le framework `internal/migrations` est un squelette non branché au runtime.
Il expose `List`, `Plan` et `Apply(path, current, target, dryRun)`. Les trois
migrations versionnées sous `migrations/` décrivent les contrats futurs, sans
secret ni valeur de maison.

Une intégration OTA devra lire la version du schéma, construire un plan
monotone, valider la configuration complète, exécuter les transformations en
mémoire, puis écrire avec backup et remplacement atomique. Une transformation
doit être idempotente et ne doit jamais modifier `secrets/`, les hashes, les
PSK, les certificats ou les clés. Les erreurs destinées aux rapports sont
redactées.

La migration s’exécute avant le healthcheck post-boot et avant `mark-good`.
Pendant la fenêtre `pending`, l’ancien slot doit encore pouvoir lire les
données persistantes ou l’orchestrateur doit restaurer le backup avant de
laisser le bootloader revenir à l’ancien slot. Cette passe n’exécute aucune
migration sur `/etc/synora`.
