# Feedback administrateur CGE

Le feedback administrateur est une annotation séparée des événements bruts et des évaluations historiques. Il ne réécrit pas une chaîne déjà produite : il exprime une intention que le moteur peut utiliser pour les futures évaluations.

## Modèle d’intention

Les POST `/api/cge/feedback/evaluation` et `/api/cge/feedback/chain` acceptent :

```json
{
  "chain_id": "chain-1",
  "event_id": "event-1",
  "evaluation_index": 2,
  "correction_type": "reaction_too_strong",
  "scope": "apply_to_similar_future_chains",
  "preferred_actions": ["observe", "request_user_validation"],
  "admin_note": "Demander confirmation avant toute action automatique."
}
```

`event_id` et `evaluation_index` concernent le feedback d’évaluation. Le feedback de chaîne ne contient que `chain_id` et l’intention générale.

`correction_type` peut être `false_positive`, `false_negative`, `reaction_too_strong`, `reaction_too_weak` ou `correct_but_tune_actions`. La portée est `case_only` ou `apply_to_similar_future_chains`.

Les actions préférées sont ordonnées et limitées à `observe`, `notify_owner`, `notify_emergency_contact`, `record_clip`, `lock_evidence`, `create_alert`, `request_user_validation`, `ignore_pattern` et `activate_related_automation`.

Les anciens champs (`corrected_state`, `corrected_danger_level`, `final_outcome`, `note`) restent acceptés pour compatibilité, mais ne constituent plus l’interface principale.

## Simulations

Les simulations restent visibles dans Live CGE avec leur badge `Simulation`. Les mémoires critiques exposent `source` (`real`, `simulation` ou `mixed`) ainsi que les compteurs réel/simulation. L’onglet Chaînes connues masque les mémoires uniquement simulées par défaut ; le filtre « Inclure les simulations » permet de les afficher.
