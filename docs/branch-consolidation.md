# Consolidation des branches et des surfaces V1

Audit réalisé le 2026-10-05 directement sur `master` dans
`/home/rock/Synora-v1`. Aucun merge automatique, aucune branche ni aucun
worktree n'a été créé. Le point de départ était `8abad9e` (`master` et
`origin/master`).

## Décision sur les branches non absorbées

Les commandes d'audit utilisées pour chaque branche étaient :

```text
git fetch --all --prune
git log --left-right --cherry-pick --oneline master...<branche>
git diff --name-status master...<branche>
git diff --stat master...<branche>
```

| Référence distante | Dernier commit | Classement | Différence utile et décision |
|---|---|---|---|
| `origin/cge/durable-cognitive-workflow` | `116b30b4850b40c3377d886e2aa2949bd4078113` — `cge: separate configured grants from applied evidence` | `obsolete` | 7 commits, 31 fichiers, principalement `internal/cge`, les grants, la décision CGE et le shadow runtime. Rien n'est compatible avec la surface canonique V1/V3. Aucun fichier intégré. |
| `origin/codex/v1-j21-hardware-harness` | `bf801d70a93f84752e63ea43ccbc23886911094d` — `fix: preserve existing qualification output parents` | `obsolete` | Le diff porte sur l'ancien `tools/v1_hardware_qualification.py`, qui n'existe plus dans l'arbre canonique. La correction concerne donc un runner supprimé et n'a pas été récupérée. |
| `origin/codex/v1-m047-rknn-rock5-itx` | `227e973bb0915e6c5aa350b50ad8e23b68ad2792` — `docs(m047): prepare physical RKNN qualification` | `obsolete` | Ajoute une procédure physique Rock 5 ITX, un statut M047 et un test de procédure. Elle ne constitue ni un contrat ni un adaptateur du runtime Vision actuel et est remplacée par la configuration/manifeste RTMPose présents dans `master`. Aucun fichier intégré. |
| `origin/codex/v1-m047-rknn-rock5t` | `ea93382981c113aa66cc3cb920fdfa5ba4f2ca83` — `docs(m047): remove superseded Rock 5 ITX procedure` | `obsolete` | Documentation de qualification physique Rock 5T et test associés, centrés sur l'ancien périmètre de modèles. Elle n'ajoute pas de logique compatible avec la frontière Edge/Discovery ni avec RTMPose V3. Aucun fichier intégré. |
| `origin/codex/v1-m048-camera-recovery` (absente sous ce nom) | — | `obsolete` | La référence réelle est `origin/codex/v1-m048-camera-pairing-live`, dernier commit `9e4c1108ddb32dcaa88530908df7640bea3a3c3d`. Elle ne contient que la procédure physique de pairing/live et son test, sans contrat ou logique compatible à récupérer. Aucun fichier intégré. |

Les 83 autres références distantes étaient déjà des ancêtres de `master` et
sont classées `absorbed` : `origin/codex/v1-j01-incidents`,
`origin/codex/v1-j02-delivery-contract`, `origin/codex/v1-j02-vision`,
`origin/codex/v1-j03-facial`, `origin/codex/v1-j03-persistent-outbox`,
`origin/codex/v1-j04-boundary`, `origin/codex/v1-j04-dispatcher-ack`,
`origin/codex/v1-j05-outbox-integration`, `origin/codex/v1-j05-tracking`,
`origin/codex/v1-j06-clips`, `origin/codex/v1-j06-core-recovery`,
`origin/codex/v1-j07-onboarding`, `origin/codex/v1-j07-state-failures`,
`origin/codex/v1-j08-discovery-uploads`, `origin/codex/v1-j08-ota`,
`origin/codex/v1-j09-api-ui`, `origin/codex/v1-j09-vision-resilience`,
`origin/codex/v1-j10-failure-matrix`, `origin/codex/v1-j11-threat-model`,
`origin/codex/v1-j12-camera-pairing`, `origin/codex/v1-j13-api-security`,
`origin/codex/v1-j14-connectivity`, `origin/codex/v1-j15-final-audit`,
`origin/codex/v1-j16-retention`, `origin/codex/v1-j17-sensitive-rights`,
`origin/codex/v1-j18-backup-restore`, `origin/codex/v1-j19-central-ota`,
`origin/codex/v1-j20-camera-recovery`, `origin/codex/v1-j22-camera-network`,
`origin/codex/v1-j23-user-flow`, `origin/codex/v1-j24-release-engineering`,
`origin/codex/v1-j25-rc-audit`, `origin/codex/v1-m001-baseline` à
`origin/codex/v1-m046-signed-update-rollback`, ainsi que
`origin/integration/synora-v1`, `origin/integration/synora-v1-baseline-20260814`,
`origin/integration/synora-v1-baseline-20260817`,
`origin/integration/synora-v1-execution` et
`origin/release/v1-rc1-local`. Aucun contenu supplémentaire n'est à
sélectionner. Les 88 références ont été supprimées explicitement avec
`git push origin --delete`, sans supprimer `master` ni aucun tag. Il ne reste
que `origin/master`.

## Audit des répertoires non suivis

Les fichiers source de `cmd/synora-api/` et `synora-web/` ont été créés ou
mis à jour entre le 20 et le 22 septembre 2026. Ils ne sont pas des chemins
legacy : le `Makefile`, `docs/intelligence-v1.md` et
`docs/mlp-trace-v1.md` les désignent comme la surface Intelligence V1
canonique.

`cmd/synora-api/` est une API de lecture seule. Elle observe uniquement les
traces `core.decision` déjà redacted, expose les GET Intelligence, le
WebSocket Intelligence et `/health`, et ne possède aucune route d'injection,
de commande ou d'action physique. Les tests vérifient notamment le retrait
des poids et embeddings des projections. Les fichiers source et tests sont
donc ajoutés tels quels à leur emplacement canonique.

`synora-web/` est la webapp statique Intelligence V1. Elle ne contient que la
lecture de la topologie et des traces redacted, sans POST/PUT/DELETE, sans
route lab et sans retour CGE/V4/shadow. `node_modules/`, `dist/` et
`.vite/` sont des dépendances ou sorties générées ignorées par
`.gitignore` et ne sont pas ajoutés. Seuls les sources, manifestes npm et
configurations TypeScript/Vite sont intégrés.

Aucun secret, média brut, bbox, crop, keypoint, embedding biométrique,
identité ou `local_track_id` n'a été trouvé dans ces surfaces. Il n'y a donc
pas de suppression à effectuer : ces deux chemins sont déjà les chemins
canoniques référencés par V1.

## Validation et garde-fous

Les changements ont été validés avant la suppression distante avec :

```text
git diff --check
go test ./...
go vet ./...
go build -buildvcs=false ./...
make test-central-v1
make test
make web-build
```

Les validations Go, le harnais central, `make test` et `make web-build` sont
passés. La consolidation a été commitée dans `e5263543a4f0f22baea9fd6f69d8b419d675ff49`
(`chore: consolidate canonical intelligence surfaces`) puis poussée sans
force-push.

Le bundle V1 reste nominal. Le bundle V3 reste candidat `active_dry_run`.
RTMPose reste `unavailable` lorsque son backend RKNN ou son modèle sont
absents. Aucun son, rendu audio, réseau de commande ou action physique n'est
introduit par cette consolidation. Aucun tag, `git gc`, `git reset`,
`git clean` ou force-push n'a été utilisé.
