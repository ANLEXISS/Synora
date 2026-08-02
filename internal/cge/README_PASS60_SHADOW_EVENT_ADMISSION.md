# Passe 60 — admission des événements Shadow

La frontière Core → Shadow est une policy fermée et immuable. Sa valeur par
défaut contient exactement :

- `vision.identity`
- `vision.unknown`
- `vision.uncertain`

La policy est canonisée par ordre lexicographique et son fingerprint est
`shadow-event-admission-policy-v1:11fccc139860e454fd3587ce2aa08a690ddac39521e509af98ab05420dcdcc96`.
Une policy vide, dupliquée ou contenant un type absent du contrat est rejetée.

## Dispositions

Les trois types ci-dessus sont `shadow_admitted`. Les événements contractuels
restants sont `historical_only` par défaut, sauf les événements d’action et
d’automation (`ignored_by_design`) et `system.unknown` (`unknown`). Cette
matrice décrit le contrat présent ; elle n’élargit pas l’allowlist exécutable.

`ignored_by_policy` signifie que l’événement est connu et reste sur le chemin
historique, mais n’est pas candidat au Shadow. `invalid` signifie qu’un type
admis a échoué une validation de forme (identifiant, timestamp ou validation
scalaire). An ignored event is not necessarily invalid.

Les codes fermés de soumission sont : `accepted`, `ignored_by_policy`,
`invalid`, `queue_full`, `stopping`, `stopped`, `workflow_disabled` et
`workflow_unavailable`. Les résultats `TrySubmit` sont mappés explicitement :
une queue pleine devient `queue_full`, un runtime arrêté devient `stopped` (ou
`stopping` pendant la fermeture), et les états non disponibles deviennent
`workflow_unavailable`.

Le status Shadow conserve uniquement le dernier type normalisé et des compteurs
agrégés. Les métriques correspondantes sont nommées
`cge_shadow_admission_<code>_total`; elles ne portent aucun identifiant,
payload, identité, clip ou fingerprint d’événement.

L’admission est asynchrone et non bloquante. Une queue pleine affecte
uniquement le traitement Shadow ; elle ne bloque ni ne modifie l’analyse
historique et ne produit aucun record fantôme. Les événements ignorés et les
erreurs Shadow ne déclenchent pas de retry.

Shadow admission never changes the historical decision.

A queue-full result affects only Shadow processing.

No retry, action, command, authorization, or production override is introduced.

The default event admission set remains unchanged in this pass.

Les marqueurs du résultat d’admission indiquent que l’autorité historique reste
inchangée et qu’aucune action n’a été produite. Le Core ne consomme aucune
recommandation cognitive comme feedback et aucun endpoint, commande, action ou
automation n’est ajouté.

Les tests couvrent la policy, la distinction accepted/ignored/invalid, le
mapping des états du workflow, la queue pleine au niveau de `TrySubmit`, les
états de cycle de vie, la concurrence et le parcours bus → Core → ledger de la
passe 59. La saturation via le raccordement Core n’a pas de seam de worker
bloqué exposée sans refonte du runtime ; elle reste donc couverte au niveau
réel de `TrySubmit` et documentée comme limite.

La prochaine frontière prévue est le **Core Read-Only Context Provider**. Elle
reste hors périmètre de cette passe.
