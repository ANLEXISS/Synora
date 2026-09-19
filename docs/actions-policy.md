# Action Policy

Synora sépare désormais les faits canoniques du Core, la proposition du MLP,
le filtrage déterministe du Safety Gate et l’écriture de l’Universal Store.
Cette note décrit uniquement les contraintes de politique d’action ; elle ne
constitue pas une seconde source de danger ou de décision.

## Niveaux

Les niveaux ordonnés sont `none`, `low`, `medium`, `medium_high`, `high` et `critical`. Une policy est sélectionnée sur le niveau calculé ; un niveau plus élevé n’active pas implicitement les actions d’un autre niveau. Les actions d’une policy sont des propositions traçables et peuvent être bloquées par le palier, l’action ou une condition.

Le fichier local est `/etc/synora/action_policy.yaml`, surchargeable avec `SYNORA_ACTION_POLICY`. Il est créé uniquement lors d’une modification/réinitialisation, jamais lors de l’installation. Les écritures passent par un fichier temporaire, un backup dans `backups/`, puis un `rename` atomique. Un fichier absent utilise les defaults sûrs en mémoire.

La sirène est désactivée par défaut. Le provider WhatsApp est également désactivé tant que sa configuration n’est pas activée. Le profil `high` propose notamment `notify.whatsapp`, `record.clip` et `mark_intrusion_candidate`; `critical` ajoute la rétention et conserve la sirène explicitement inactive.

## API

- `GET /api/actions/policy` — policy effective et état non sensible du provider WhatsApp, admin-only.
- `PATCH /api/actions/policy` — patch validé strictement, admin-only.
- `POST /api/actions/policy/reset` — restauration des defaults sûrs, admin-only.
- `GET /api/actions/catalog` — catalogue des commandes, admin-only.
- `POST /api/actions/test` — préparation dry-run ou mise en file d’une action, admin-only.

Les conditions utilisent le même vocabulaire que les Automations (`security.armed`, `security.mode`, `danger.level`, etc.). `priority` est bornée à 0–100 et `cooldown_seconds` à 0–86400.

## WhatsApp Cloud API

`synora-actions` lit `whatsapp` dans `/etc/synora/actions.yaml` (surcharge possible par variables d’environnement) :

```yaml
whatsapp:
  enabled: false
  provider: cloud_api
  graph_version: v23.0
  phone_number_id: ""
  access_token_file: /etc/synora/secrets/whatsapp_token
  default_to: ""
  default_template: synora_security_alert
  language_code: fr
  dry_run: true
```

Le token est lu depuis `access_token_file` ou `SYNORA_WHATSAPP_ACCESS_TOKEN` pour le développement. Il n’est jamais écrit dans Git ni dans les logs. Le numéro est masqué dans les résultats. Le mode dry-run ne contacte pas Meta et retourne le provider, le destinataire masqué, le template et le message. Le mode actif utilise `POST /{graph_version}/{phone_number_id}/messages` avec un timeout court et renvoie une erreur neutre pour les problèmes réseau/HTTP.

Les commandes reconnues sont `notify.whatsapp` et `notify_owner_whatsapp`. Les templates sont privilégiés ; le texte direct reste un chemin de développement quand la fenêtre WhatsApp l’autorise.

## Décision visible

Les évaluations exposent la proposition MLP, le plan filtré, les actions
bloquées et la raison du filtrage. Une action bloquée conserve son
`blocked_reason` (`action_disabled`, `condition_not_met`, etc.). En V1,
`active_dry_run` reste le défaut et aucune action physique n’est exécutée.
