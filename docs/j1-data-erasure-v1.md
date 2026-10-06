# J1/J4 — qualification de la suppression coordonnée contrôlée

Date de vérification : 2026-10-06

## Périmètre qualifié

`DELETE /api/system/data` est une capacité d’administration coordonnée. La
route exige une session admin ou un bearer admin ; une session web doit fournir
une origine autorisée et le jeton CSRF. La requête porte une raison explicite.
L’API attend les accusés `core` et `discovery` avant de répondre
`status=erased`, `scope=global`.

Le Core n’accepte que `target_state=empty`, efface son état durable avec
marqueur de récupération. Discovery quiesce l’ingress caméra, draine la queue
Vision, efface le spool clips, le face store, le cache de snapshots, les
observations caméra et les résultats d’actions volatils ; la configuration de
déploiement n’est pas supprimée.

Le protocole est idempotent. L’état effacé ne réapparaît pas après réouverture
du store. Une panne ou une réponse invalide reste exposée comme `unknown` côté
API ; aucune réussite fictive n’est produite.

La coordination écrit un marqueur API avant les deux RPC et Discovery écrit un
marqueur local avant sa purge. Un redémarrage rejoue les deux scopes et ne
supprime le marqueur qu’après succès ; un échec conserve le marqueur pour la
reprise suivante.

## Preuves

| Vérification | Résultat |
| --- | --- |
| `go test ./...` | vert |
| `PYTHONPATH=services/vision-worker python3 -m unittest discover -s services/vision-worker/tests -p 'test_*.py'` | 89 tests, vert |
| `npm run build` dans `synora-web` | vert |
| `make test-central-v1` | 402/402, `failed=0` |
| cas central `system-state-reset` | `data_reset_status=erased`, scopes Core + Discovery |
| manifeste central | `51d61b86e292ce63ddd9b801f37d48a915e35dd19937f46e02cf73e320834602` |
| `git diff --check` | vert |
| tests de marqueur/reprise API + Discovery | vert |

## Limites et décision

Cette preuve couvre les stores et caches actuellement actifs dans le runtime V1.
La coordination est séquentielle et ne constitue pas encore une transaction
deux-phases : une panne entre les deux accusés reste affichée comme `unknown`,
mais les marqueurs permettent la reprise automatique au redémarrage ou à la
prochaine tentative. J1 et J4 restent donc ouverts tant que le burn-in et la
validation critique ne sont pas démontrés.

L’installation `/opt/synora` reste sur un commit antérieur et ne peut pas être
mise à jour sans autorité privilégiée. Aucun jalon critique n’est déclaré
validé sur cette seule preuve logicielle.
