# Action Policy

Synora sépare désormais quatre responsabilités : le CGE évalue la situation, l’Action Policy propose un socle de réactions par niveau, les Automations ajoutent le contexte utilisateur, puis `synora-actions` exécute les `ActionRequest`.

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

Les évaluations exposent `recommended_actions_from_cge`, `recommended_actions_from_policy`, `policy_actions`, `final_action_plan`, `blocked_actions` et `action_decision_reason`. Une action bloquée conserve son `blocked_reason` (`action_disabled`, `condition_not_met`, etc.). L’Action Policy n’exécute pas automatiquement les actions physiques dans cette passe ; les Automations continuent d’être le chemin d’exécution contextualisé.
