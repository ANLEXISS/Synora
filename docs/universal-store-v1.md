# Universal Store V1

Le Universal Store est la source de vérité runtime par site.

Il conserve un journal append-only borné, un snapshot matérialisé, une
révision monotone, les identifiants déjà traités, les décisions MLP, les
résultats d’action, les traces de capture et l’outbox d’actions. Core est son
unique écrivain métier. Discovery peut lire un snapshot pour répondre au Web.

Chaque commit atomique comprend :

```text
fait traité + snapshot suivant + danger + décision cognitive
+ trace d’entraînement + demande d’action éventuelle
```

La validation d’idempotence intervient avant mutation. Le replay réapplique le
journal dans l’ordre ; les événements déjà traités sont ignorés. Les limites
de journal et d’outbox empêchent une croissance non bornée.

## Durabilité et rollback

L’implémentation V1 fournit le contrat atomique et le replay hermétique. La
persistance fichier est append-only et doit être placée sous le répertoire de
données configuré avant déploiement. Un rollback Git ne réécrit jamais le
journal existant ; une reprise se fait par replay du dernier format compatible.
