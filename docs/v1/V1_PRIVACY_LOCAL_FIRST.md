# V1 — confidentialité local-first

## Principe

Les photos de référence, crops de revue, copies de dataset et embeddings restent
sur le stockage local Synora. Aucun export automatique vers un service distant
n’est prévu en V1. Les interfaces publiques ne renvoient ni chemin local,
`storage_key`, crop, landmark ni embedding.

## Export résident

`GET /api/residents/{resident_id}/privacy/export` est réservé à un principal
administrateur. Il produit un JSON local contenant la configuration du résident
et les métadonnées de ses photos (identifiants, statut, taille, type et dates).
Les octets image, les embeddings et les chemins sont explicitement exclus.

## Suppression

La suppression d’un résident désactive d’abord son identité de configuration et
place ses photos dans l’état `removal_pending`. L’API supprime ensuite sans
suivre de symlink les répertoires de photos legacy et les sources canoniques.
Le prochain cycle de synchronisation construit et recharge atomiquement un
dataset sans ces entrées, confirme leur retrait, puis purge immédiatement les
anciennes versions de dataset afin de ne pas conserver les embeddings pendant
la rétention opérationnelle normale.

En cas d’échec disque, les métadonnées pending restent visibles pour permettre
la reprise et le diagnostic; aucune suppression partielle n’est présentée comme
terminée.

## Stockage et support

Les répertoires biométriques sont durcis en `0750` et les fichiers créés en
`0640`; l’installateur doit en être propriétaire et le runtime refuse les
symlinks. Avant remise d’un bundle de support, `security.CleanSupportBundle`
supprime les artefacts faciaux/embeddings et applique la redaction des secrets
et chemins. Les journaux de support ne doivent contenir que des identifiants
techniques et des agrégats.
