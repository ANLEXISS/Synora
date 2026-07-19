# Simulateurs développeur historiques

Les simulateurs CLI développeur sont hors produit et hors installation par
défaut. Ils servent à reproduire rapidement des payloads ou des séquences en
environnement de développement. Ils ne doivent pas être utilisés pour
représenter Synora Lab dans la documentation produit.

Une migration de chaque binaire ici doit conserver un wrapper ou une cible
Make de compatibilité, puis vérifier `go test ./...` et les imports runtime.
