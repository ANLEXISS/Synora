# Passe 56 — Durable Cognitive Calibration Ledger

Cette passe ajoute `internal/cge/calibrationledger`, un historique durable,
append-only et expurgé des `HistoricalDecisionComparison`. Il est indépendant
du WAL de `durableworkflow` et désactivé par défaut.

The ledger records comparisons; it does not calibrate automatically.
Alignment does not prove correctness.
Divergence does not prove an error.
No threshold or weight is changed by the ledger.
Historical production authority remains unchanged.

## Format et genesis

Le fichier est un NDJSON versionné, une enveloppe complète par ligne avec
newline final. La première enveloppe référence le genesis déterministe
`calibration-ledger-genesis-v1`. Le genesis contient le fingerprint de la policy
sémantique du ledger et ne dépend ni du chemin, ni de la machine, ni du temps,
ni d’un secret. `Fsync` et `RepairTrailingRecord` sont des choix opérationnels
et sont exclus de ce fingerprint afin qu’une réparation explicitement activée
ne change pas l’identité du ledger.

Chaque enveloppe vérifie la séquence, le fingerprint du record, le hash du
record, le hash de l’enveloppe et le hash de l’enveloppe précédente. Le store
publie son index et son snapshot seulement après écriture complète, flush et
`fsync` lorsque la policy l’exige.

## Expurgation

Le record ne conserve que des fingerprints, codes fermés, compteurs, booléens,
timestamps source déjà disponibles et valeurs permille bornées. Il ne conserve
ni payload, image, frame, audio, embedding, identifiant brut, adresse réseau,
token, credential, commande, action ou texte utilisateur. Les dimensions sont
réduites à `Kind`, `Status`, scores, comparabilité et fingerprint.

Les marqueurs d’un record sont tous obligatoirement vrais. Le ledger est
descriptif: il ne modifie aucun seuil ou poids, n’entraîne rien, n’émet ni
alerte ni commande et ne remplace jamais la décision historique.

## Recovery

Un fichier absent ou vide est valide. Une corruption avant la dernière ligne
est fatale et n’est jamais ignorée. Une dernière ligne sans newline est
signalée par `ErrTrailingRecordTruncated`; par défaut elle n’est pas modifiée.
Avec `RepairTrailingRecord`, seule cette ligne terminale est supprimée, puis
le fichier est synchronisé. Aucune réparation de corruption intermédiaire
n’est tentée.

## Shadow et qualification

Lorsque les variables `SYNORA_CGE_CALIBRATION_LEDGER_*` activent le ledger,
le démarrage récupère le workflow durable, reconstruit la projection cognitive,
puis récupère le ledger avant le worker. Après un commit durable, la comparaison
volatile est publiée puis adaptée en record et appendée. Un échec du ledger
dégrade uniquement son status et ses métriques; le commit, le Core et la
projection cognitive restent valides.

Les anciennes lignes ne sont jamais réinjectées dans la projection cognitive.
Les samples de qualification ne contiennent que les agrégats ledger, jamais les
records.

## Limites explicites

Les percentiles sont exacts pour les scores entiers `[0,1000]` grâce à trois
histogrammes fixes de 1001 buckets. Les queries sont internes, déterministes et
limitées à 1000 résultats. Il n’existe aucun endpoint HTTP, WebSocket, CLI de
production, feedback automatique, mise à jour de seuil, mise à jour de poids,
override de décision ou exécution d’action.
