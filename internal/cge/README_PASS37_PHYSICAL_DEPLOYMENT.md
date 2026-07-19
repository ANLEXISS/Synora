# Passe 37 — préparation du premier essai physique

Cette passe prépare un essai Shadow local sans l’installer. Elle distingue le
code qualifié, la configuration prête, l’installation manuelle et la session
réellement démarrée. Aucun fichier n’est écrit dans `/etc` ou
`/var/lib/synora` par les tests et aucun service n’est démarré.

## Progression evidence

Phase 0 : préflight hors service. Phase 1 : 24 heures avec
`SYNORA_CGE_SHADOW_AUTO_EVIDENCE_ENABLED=false`. Phase 2 : activation
manuelle de `true` après revue. Phase 3 : campagne de plusieurs semaines. Le
changement de phase n’est jamais automatisé.

## Commandes opératoires

```text
synora-cge-trial key generate --output <clé>
synora-cge-trial topology validate --file <topologie>
synora-cge-trial preflight --env-file <env> --key-file <clé> --topology-file <topologie>
synora-cge-trial prepare --env-file <env> --key-file <clé> --topology-file <topologie> --output <manifest>
synora-cge-trial start --env-file <env> --root <root> --session <id> --key-file <clé> --topology-file <topologie> --deployment-manifest <manifest>
synora-cge-trial status --root <root> --session <id>
synora-cge-trial doctor --root <root> --session <id>
synora-cge-trial smoke-check --root <root> --session <id> --require-events 1
synora-cge-trial checkpoint --root <root> --session <id>
synora-cge-trial verify --root <root> --session <id>
synora-cge-trial close --root <root> --session <id>
synora-cge-trial report --root <root> --session <id>
synora-cge-trial export --root <root> --session <id> --output <export>
synora-cge-trial export-verify --dir <export>
```

Le binaire de développement initialise uniquement la session du recorder;
l’attachement au service et toute modification système restent manuels. Une
session existante dont l’empreinte cognitive diffère est refusée.

`preflight` ne démarre pas de session. `prepare` écrit uniquement le manifeste
à l’emplacement de sortie demandé. `start` exige ce manifeste par défaut et
refuse une dérive de fingerprint; `--without-deployment-manifest` est réservé
au développement et aux tests. `status` et `doctor` peuvent comparer une
configuration fournie par `--env-file` à l’empreinte de session. Aucun de ces
contrôles n’écrit dans `/etc` ou dans `/var/lib/synora` pendant les tests.

La topologie d’exemple dans `deploy/examples/cge-topology.example.json` est
générique. Elle doit être adaptée aux `node_id` réellement produits par
Synora et validée avec `topology validate`; elle ne décrit aucun domicile
réel. La clé est générée séparément avec `key generate`, conservée localement
en mode `0600`, et n’est jamais exportée.

## Exploitation

Chaque jour : `status`, `doctor`, `checkpoint`. Chaque semaine : `verify`,
rapport intermédiaire, contrôle espace/quota, annotations bornées, export de
sauvegarde optionnel et vérification de version/topologie. Les annotations ne
sont jamais transmises au CGE et ne changent pas le WAL.

Scénarios contrôlés admissibles : résident à une heure inhabituelle, trajet
inhabituel, passage extérieur/intérieur, topologie absente, caméra indisponible
ou identité de test inconnue. Aucun scénario dangereux n’est automatisé.

Rollback : désactiver l’environnement field trial et le Shadow cognitif selon
le contrôle de changement du site, conserver les sessions et vérifier le
moteur historique. Ce document ne l’exécute pas.

## Distinction de readiness

`ReadyForPhysicalShadowDeployment` indique que le code et les essais Shadow
précédents restent qualifiés. `ReadyForManualInstallation` ajoute les gates
offline de cette passe : configuration et fingerprint, stockage temporaire,
clé, topologie, doctor/smoke-check, export vérifiable et runbooks. Cela ne
signifie ni qu’une installation a eu lieu, ni qu’une session réelle est
ouverte, ni que le service est activé.
