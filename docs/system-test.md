# Synora System Test

`tools/synora_system_test.sh` est un harnais reproductible de vérification du
runtime Synora. Il teste l’API, les contrats JSON, le Core/Bus, le CGE, les
chaînes, le mode sécurité, le risque manuel, les données de configuration, la
webapp statique et les journaux récents.

Le script ne démarre, n’arrête, ne redémarre, n’installe et ne déploie aucun
service. Il ne touche pas au pipeline Python `services/vision-worker/**`.

## Utilisation

Depuis le repo local avec un runtime déjà lancé :

```bash
export SYNORA_API_TOKEN='…'
./tools/synora_system_test.sh --target local --base-url http://127.0.0.1:8080 --mode smoke
```

Depuis le repo du prototype, le token est lu automatiquement depuis
`/etc/synora/security.yaml` :

```bash
./tools/synora_system_test.sh --target ssh --host rock@100.80.170.47 --mode full
```

La commande SSH exécute le même script dans `~/Synora` sur la machine distante.
Elle ne lance aucun restart ni `make install`.

## Modes

- `smoke` : contrôles rapides non destructifs, endpoints critiques, webapp,
  services et logs.
- `full` : smoke plus armement/désarmement, manual-risk, validation CGE
  single/sequence, événement invalide et contrôles après injection. Les
  mutations sont bornées et nettoyées en fin de test.
- `readonly` : mêmes contrôles de lecture que smoke, sans mutation.
- `stress-lite` : smoke plus 30 requêtes parallèles courtes sur les endpoints
  critiques.

Les réponses validation `200` et `202` sont acceptées, car l’API peut utiliser
`202 Accepted` pour un traitement `queued`.

## Critères bloquants

Le script échoue notamment si :

- un endpoint critique dépasse 3 secondes, retourne 500 ou un JSON invalide ;
- `chain-sequence` valide n’est pas `queued` ;
- un événement non supporté ne retourne pas `400 validation_failed` ;
- `security_mode`, `security_armed` ou `expected_occupancy` sont nuls ;
- un format `events/chains` ne peut pas être normalisé en tableau ;
- `medium_high` est absent du catalogue d’automations ;
- les routes SPA ou les assets statiques ne sont pas servis ;
- les journaux contiennent `incoming channel full`, panic, deadlock, data race,
  erreur runtime ou timeout critique ;
- Core, Bus ou API ne sont pas actifs lorsque `systemctl` est disponible.

Discovery, Actions et MediaMTX peuvent être dégradés en mode normal. Utiliser
`--strict-services` pour rendre les services optionnels bloquants.

## Rapport JSON

Chaque exécution écrit :

```text
artifacts/system-test/synora-system-test-YYYYMMDD-HHMMSS.json
```

Le dossier est ignoré par Git. Le rapport contient les compteurs PASS/WARN/FAIL,
la durée de chaque requête, les codes HTTP, les détails, les échecs bloquants et
les lignes de logs détectées.

En cas d’échec, le script conserve aussi les réponses HTTP dans un répertoire
temporaire et indique le chemin du body concerné dans la sortie.

## Diagnostic rapide

### `incoming channel full`

Conserver le rapport et les lignes de journal affichées. Vérifier ensuite la
latence Core/RPC, la fréquence de polling webapp et la persistance synchrone.
Ne pas masquer le problème en relançant immédiatement les services.

### Endpoint timeout

Examiner les bodies sauvegardés, les logs Core/Bus et le nombre de requêtes
parallèles. Un timeout indique généralement une saturation RPC, une écriture
persistante lente ou un client Bus bloqué.

### `chain-sequence` en 500

Comparer le body avec le contrat de validation, vérifier les logs Core/API et
confirmer que l’événement invalide retourne bien 400. Le script ne corrige pas
le runtime et ne redémarre rien.

## Makefile

Les raccourcis locaux suivants sont disponibles :

```bash
make system-test-smoke
make system-test-full
make system-test-readonly
make system-test-stress-lite
```

Ils utilisent `BASE_URL` si défini, sinon `http://127.0.0.1:8080`, et lisent le
token depuis `SYNORA_API_TOKEN`. Aucun target n’utilise `sudo`.

## Limites

Ce harnais complète les tests unitaires et les builds ; il ne les remplace pas.
Il ne remplace pas non plus une vraie passe de charge longue durée, une analyse
de crash/restart, ni les tests dédiés du pipeline vision Python, qui sont
explicitement exclus.
