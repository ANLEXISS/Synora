# Passe 48 — Shadow Workflow Qualification Gate

Cette passe qualifie l’intégration logicielle du Shadow Workflow de bout en bout. Elle ajoute des fixtures, des providers synthétiques et de l’injection de pannes ; elle n’ajoute aucune couche cognitive ni aucun comportement métier.

```text
Historical Shadow CGE
        │
        └── copie expurgée non bloquante
                     ↓
              bounded queue
                     ↓
              single worker
                     ↓
        experimental cognitive pipeline
                     ↓
              durable transaction
                     ↓
               WAL + checkpoint
                     ↓
             status / metrics / recovery
```

## Portée

Les tests couvrent l’implémentation réelle de `shadowworkflow.Runtime` et du `durableworkflow.Coordinator`. Le workflow reste désactivé par défaut, asynchrone, mono-worker, borné et isolé du Shadow CGE historique. Les entrées de test ne contiennent que des `ObservationRef` synthétiques ; elles ne contiennent ni média, payload original, embedding, biométrie brute, secret, token ni décision de sécurité.

`Implemented does not mean physically qualified.` Le gate logiciel précède toute qualification sur hub.

## Fixtures et fault injection

Les providers synthétiques construisent uniquement le catalogue abstrait de `capabilitymapping` et un inventaire avec `provider-alpha` et des identifiants `capability-instance-*`. Le provider d’autorisation produit des contextes, policies et snapshots de grants synthétiques : default deny, allow explicite, confirmation requise, grant valide, expiré ou révoqué. Aucun dispositif réel, permission système, grant d’exécution ou token n’est créé.

`TestQualificationSyntheticAuthorizationProviderScenarios` vérifie les quatre derniers modes au travers de la frontière intégrée : confirmation requise, grant valide, grant expiré et grant révoqué.

`qualificationStore` enveloppe un store durable contrôlé et injecte append failure, fsync failure, échec de checkpoint, append suivi d’une interruption de publication, limite de WAL et réouverture après recovery. Les tests de troncature et de corruption utilisent le `FileStore` dans `t.TempDir()`. Le provider synthétique injecte erreur, blocage contrôlé, timeout et panic. `qualificationClock` contrôle l’horloge du circuit breaker et des fenêtres temporelles sans longs `sleep`.

## Pipeline et providers absents

`TestQualificationFullPipelineWithSyntheticProviders` vérifie le chemin :

```text
event → episode → facts → hypotheses → discrimination → advisory
      → capability mapping → authorization boundary → durable commit
```

Toutes les couches configurées sont `fresh`, leurs filiations correspondent et le digest du status est celui de l’état durable. Les tests `TestQualificationMissingCapabilityProviderSkipsWithoutFabrication` et `TestQualificationMissingAuthorizationProviderSkipsWithoutAllow` vérifient qu’un provider absent ne fabrique ni inventaire, ni policy, ni grant, ni assessment positif. Avec un provider d’autorisation vide, `TestQualificationAuthorizationDefaultDenyIntegrated` vérifie `denied_by_default`, même si le mapping amont est préféré ou très utile.

## WAL, recovery et checkpoints

| Preuve | Test |
| --- | --- |
| WAL intermédiaire corrompu | `TestQualificationCorruptMiddleWALFailsClosed` |
| Dernier record tronqué autorisé/refusé | `TestQualificationTruncatedFinalWALIsPolicyControlled` |
| Échec de checkpoint après commits | `TestQualificationCheckpointFailurePreservesCommittedState` |
| Append durable avant publication mémoire | `TestQualificationAppendBeforePublicationReplaysAfterRestart` |
| Fsync de commit échoué | `TestQualificationCommitFsyncFailureDoesNotPublishMemoryState` |
| Limite de taille du WAL | `TestQualificationWALLimitStopsOnlyWorkflow` |
| Doublon après recovery | `TestQualificationDuplicateAfterRecoveryKeepsDigest` |
| Checkpoint au nombre configuré | `TestQualificationPeriodicCheckpointAtConfiguredCount` |
| Append failure sans publication | `TestQualificationAppendFailurePublishesNothing` |

Une corruption intermédiaire passe à `recovery_failed` et refuse les nouvelles entrées ; le Shadow historique reste actif. Une troncature finale n’est ignorée que lorsque `AllowTruncatedFinalRecord` le permet. Une erreur de checkpoint est distincte d’une perte de durabilité de commit : les transactions déjà commitées restent valides, mais le runtime est `degraded`. La limite de WAL produit `storage_limit_reached`, sans suppression, troncature ou fallback mémoire silencieux.

## Circuit breaker, pannes et quotas

`TestQualificationCircuitClosedOpenHalfOpenSuccessAndFailure` couvre `closed → open → half_open → closed` et `half_open → open`, avec une seule sonde admise et une horloge contrôlée. Les tests `TestQualificationPanicInProviderStageIsContained` et `TestQualificationProviderTimeoutDoesNotCommitAndNextEventRuns` vérifient qu’un panic devient `panic.recovered`, qu’un timeout devient `quota.processing_timeout`, qu’aucun état partiel n’est publié et que les événements suivants restent traitables lorsque le circuit est fermé.

`TestQualificationMappingAndAuthorizationQuotasFailBeforeCommit` couvre les quotas de cardinalité. `TestQualificationQueueFullRejectsNewestWithoutBlocking` vérifie la queue bornée `drop_newest` : une entrée déjà acceptée n’est pas supprimée et `TrySubmit` ne bloque pas.

## Arrêt, status et logs

`TestQualificationNormalShutdownStopsWorkerAndStore` couvre le drain et la fermeture normale. `TestQualificationShutdownTimeoutLeavesStoppingState` vérifie qu’un worker bloqué ne se déclare pas faussement `stopped` et qu’aucun deadlock n’est créé. `TestQualificationStatusSnapshotIsDefensivelyCloned` vérifie les copies défensives sous mutation externe.

Le runtime ne logue pas les payloads. `TestQualificationLogsContainNoSensitiveInput` injecte des marqueurs synthétiques et vérifie qu’ils ne sortent pas du logger. Les diagnostics autorisés sont des codes d’étape/erreur, révisions, sequences, compteurs et fingerprints abrégés ; aucune identité, grant, scope sensible, secret ou chemin privé n’est exposé.

## Isolation historique

`TestShadowWorkflowGoldenHistoricalRegression` exécute le même corpus avec le workflow désactivé puis activé et compare les résultats historiques, chaînes, associations, routines, déviations, snapshots, fingerprint de configuration, decision fingerprints et compteurs historiques pertinents. La soumission est un branchement observateur : son résultat ne peut ni modifier une décision, ni refuser l’événement historique. Aucun provider, mapping, éligibilité ou panne du workflow ne possède d’autorité de production.

## Readiness

`QualificationReadiness()` expose les preuves logicielles attendues par cette passe. `ReadyForPhysicalShadowQualification` est vrai pour le gate logiciel couvert par les fixtures ; les champs suivants restent explicitement faux :

```text
PhysicalDeploymentPerformed  = false
MultiDayStabilityValidated   = false
ProductionAuthority          = false
ActiveObservationImplemented = false
ActionExecutionImplemented   = false
SecurityAuthority            = false
```

La readiness ne lance aucun déploiement et n’accorde aucune autorité.

## Tableau de qualification

```text
full pipeline                         TestQualificationFullPipelineWithSyntheticProviders
missing capability provider           TestQualificationMissingCapabilityProviderSkipsWithoutFabrication
missing authorization provider        TestQualificationMissingAuthorizationProviderSkipsWithoutAllow
default deny                          TestQualificationAuthorizationDefaultDenyIntegrated
corrupt middle WAL                    TestQualificationCorruptMiddleWALFailsClosed
truncated final WAL                   TestQualificationTruncatedFinalWALIsPolicyControlled
checkpoint failure                    TestQualificationCheckpointFailurePreservesCommittedState
append before publication             TestQualificationAppendBeforePublicationReplaysAfterRestart
commit durability failure             TestQualificationCommitFsyncFailureDoesNotPublishMemoryState
WAL limit                             TestQualificationWALLimitStopsOnlyWorkflow
breaker closed/open                   TestQualificationCircuitClosedOpenHalfOpenSuccessAndFailure
panic recovery                        TestQualificationPanicInProviderStageIsContained
pipeline timeout                      TestQualificationProviderTimeoutDoesNotCommitAndNextEventRuns
normal shutdown                       TestQualificationNormalShutdownStopsWorkerAndStore
shutdown timeout                      TestQualificationShutdownTimeoutLeavesStoppingState
golden historical regression          TestShadowWorkflowGoldenHistoricalRegression
TrySubmit isolation                   TestShadowWorkflowGoldenHistoricalRegression
queue full                            TestQualificationQueueFullRejectsNewestWithoutBlocking
duplicate after recovery              TestQualificationDuplicateAfterRecoveryKeepsDigest
periodic checkpoint                   TestQualificationPeriodicCheckpointAtConfiguredCount
quotas                                TestQualificationMappingAndAuthorizationQuotasFailBeforeCommit
status defensive clone                TestQualificationStatusSnapshotIsDefensivelyCloned
logs redacted                         TestQualificationLogsContainNoSensitiveInput
```

## Benchmarks

Les benchmarks ciblés mesurent `TrySubmit` sous worker bloqué, le rejet d’une queue pleine, le status sous charge et un cycle complet avec providers synthétiques. Ils rapportent `ns/op`, allocations et bytes/op ; ils ne fixent aucun objectif absolu pour `fsync` matériel. L’objectif est un temps de soumission borné, une saturation prévisible et l’absence de blocage historique.

## Limites restantes

La queue n’est pas persistée : une entrée acceptée mais non commitée peut être perdue lors d’un crash. Le worker reste mono-worker et le store ne supporte pas deux processus écrivains. Il n’y a ni compaction WAL, ni endpoint, ni déploiement physique, ni stabilité multi-jour qualifiée.

```text
Implemented does not mean physically qualified.
Recovery primitives must be tested through the integrated runtime.
A software qualification gate precedes deployment on the hub.
No qualification result grants production authority.
No capability is invoked.
No authorization eligibility becomes an execution grant.
```

La prochaine étape possible est **Physical Shadow Qualification**.
