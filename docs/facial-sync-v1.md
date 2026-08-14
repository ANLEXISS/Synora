# Résidents — synchronisation faciale V1

La racine canonique est `/var/lib/synora/vision/face`. Elle est déjà déclarée
par `configs/security.yaml` et `deployments/systemd/synora-api.service`; elle
peut être remplacée par `SYNORA_FACE_DATA_ROOT`. Discovery et synora-api
résolvent la même valeur.

L’arborescence est locale et privée :

* `uploads/` contient les `.part-*` temporaires ;
* `sources/<resident_id>/` contient les sources finalisées ;
* `datasets/staging/` contient les constructions incomplètes ;
* `datasets/versions/<version>/` contient les versions immuables ;
* `datasets/current` est un pointeur atomique vers la dernière version
  confirmée par Vision ;
* `legacy/` est réservé au signalement des anciennes données non associables.

Les anciennes racines historiques (`/var/lib/synora/services/vision-worker/data/faces`
et `/var/opt/synora/services/vision-worker/data/faces`) ne sont ni fusionnées,
ni supprimées, ni importées par le seul nom d’un dossier. Une migration doit
fournir une association explicite avec un `resident_id`; sinon la donnée reste
legacy/orpheline.

La frontière interne Vision utilise `face_dataset.embed` et
`face_dataset.reload`. Le worker doit confirmer la version chargée après avoir
validé le manifest et chargé la FaceDB complète. En cas d’échec, `current` et
la FaceDB précédente restent inchangés. Ces opérations ne sont pas des routes
HTTP et aucun embedding ou chemin interne n’est exposé par les contrats publics.

## Parcours métier V1

Le chemin canonique est `clip.ready → clip.processing → vision.* → Core →
StateStore → suivi/Engine → incident → clip.available`, avec `vision.end`
émis de façon idempotente quand l’activation est connue. Les projections
publiques ne contiennent ni chemin physique, crop, landmark, contenu photo ou
embedding.

Le référentiel V1 est : fenêtre nœud 20 s, cluster TTL 10 s, présence entrée
0,6 / sortie 0,4, decay 15 min, bonus identité +0,25, downgrade 10 s, et
priorité `idle < activity < suspicious < intrusion < break-in`. Le code actuel
ne possède pas de champ autonome `identity_bonus`; le score Engine existant
reste donc inchangé sur ce point et l’écart est explicite. L’implémentation CGE
conserve aussi son verrou existant `LockIntrusionUntilReset=true` ; il n’est
pas remplacé par une nouvelle autorité V1 ni par une extension cognitive.
