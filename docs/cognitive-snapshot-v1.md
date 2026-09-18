# CognitiveSnapshot V1

`CognitiveSnapshot` est l’entrée canonique du State Encoder V1. Le vecteur V1
possède 86 dimensions fixes. Le bloc
`VisionEvidence[44]` réutilise les faits normalisés du contrat
`state-encoder/v5`, mais il n’est pas un snapshot complet et n’alimente pas à
lui seul le MLP.

## Groupes

| Groupe | Contenu | Rôle |
|---|---|---|
| sécurité/présence | armed, degraded, known, human et résidents présents | contexte de site |
| topologie | classe sémantique et zone connue | cohérence d’action |
| périphériques | capacités abstraites et disponibilité | faisabilité |
| capteurs/événements | mouvement, accès, alarme, compteurs | faits co-observés |
| épisode | phase, segments, gaps, calme, âge | continuité |
| danger précédent | valeur persistée du cycle précédent | dynamique temporelle |
| résultats d’action | états agrégés bornés | retour événementiel |
| VisionEvidence[44] | observations Vision normalisées | évidence segmentée |

Le vecteur possède une dimension fixe et un ordre publié par le code et les
tests golden. Un champ absent reçoit une valeur neutre documentée ; une valeur
non finie ou hors plage est clampée. Les sorties teacher, le danger attendu et
les décisions antérieures non persistées n’entrent jamais dans l’encodage.

## Heads

Le cycle est explicitement séquentiel : `danger → incident → task → action`.
Chaque head voit le snapshot ; les heads suivants peuvent voir uniquement les
sorties précédentes du même cycle. Les actions sont abstraites : `no_action`,
`notify`, `record`, `light`, `lock`, `siren`, `request_review`.

## Compatibilité

V4 reste un contrat séparé et n’est pas modifié. Un manifest V4 ne peut pas
être chargé comme bundle V1 : la version, la dimension et les labels doivent
correspondre exactement. En l’absence de bundle V1 compatible, le Core persiste
les faits en `active_dry_run` fail-closed et n’envoie aucune action.
