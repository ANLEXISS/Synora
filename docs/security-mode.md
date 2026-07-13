# Security Mode et Manual Risk

Synora sépare deux contrôles :

- **Manual Risk** est un risque temporaire injecté par un administrateur. Il
  accepte `low`, `medium`, `medium_high`, `high` ou `critical`, expire automatiquement et reste
  compatible avec `POST /api/cge/manual-risk`.
- **Security Mode** est le contexte durable du système : `home`, `night`,
  `away` ou `high_security`. Il est persisté avec le `StateStore` dans l’état
  runtime et restauré par `synora-core` après redémarrage.

## API

```json
{
  "mode": "high_security",
  "reason": "Maison vide",
  "duration_seconds": 3600
}
```

`GET /api/security/mode` renvoie le mode courant. Les écritures sont réservées
à `admin` : `POST`/`PATCH /api/security/mode`, `POST /api/security/arm` et
`POST /api/security/disarm`. Une durée absente signifie sans expiration ; une
expiration remet le système à `home` et publie le même événement de changement.

Chaque changement publie `security.mode.changed` avec `old_mode`, `new_mode`,
`armed`, `expected_occupancy`, `source` et `reason`. Le mode n’est pas un danger
en soi et n’active pas automatiquement une intrusion.

## Contexte d’automation

Les évaluations exposent les champs suivants :

`security.mode`/`security_mode`/`mode`,
`security.armed`/`armed`/`is_armed`,
`occupancy.expected`/`expected_occupancy`,
`manual_risk.active`/`manual_risk_active`,
`danger.level`, `danger.source`, `current_state` et `event.type`.

Les champs booléens acceptent les valeurs JSON `true`/`false` et leurs formes
texte usuelles. Le catalogue `/api/automations/catalog` et le builder web
exposent les quatre conditions sécurité/occupation/risque manuel.

Les niveaux de danger suivent l’ordre `none < low < medium < medium_high < high
< critical`. Les automations utilisent cet ordre pour les opérateurs `>`, `>=`,
`<` et `<=` ; `medium_high` est affiché « Moyen élevé » dans la webapp.

## Runtime et frontend

`/api/state.system.security` et `/api/cge/runtime-status.security` contiennent
le mode, l’armement, l’occupation attendue, l’auteur, la raison, les dates et
la source. Le Dashboard affiche ce contexte séparément du danger CGE. Les
boutons de commande sont masqués pour les non-administrateurs ; la carte reste
visible en lecture seule.
