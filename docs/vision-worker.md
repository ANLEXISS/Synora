# Vision Worker V1

Le Vision Worker est une capacité optionnelle de Discovery. Le pipeline actif
est `clip-v1`, sélectionné explicitement par la requête ou le job ; la
configuration globale reste désactivée par défaut.

Le pipeline traite un clip ou un segment fermé, échantillonne les frames,
applique le détecteur configuré, conserve les pistes dans l’ordre temporel et
publie des observations normalisées. Le runtime centralisé des segments est
également opt-in et utilise une fenêtre de réordonnancement bornée.

Pour un track, le pipeline produit au plus un résumé final sous
`synora.vision.clip-summary/v1`. Le résumé contient l’épisode, la topologie,
la piste, la confiance agrégée, l’état des enrichissements, les métriques
backend et les indications de priorité. Une alerte préliminaire peut être
émise uniquement pour une évidence d’objet sensible configurée ; elle ne
constitue pas une commande.

Les valeurs par défaut sont conservatrices : durée maximale de clip de 10 s,
fenêtre de continuité d’épisode de 5 s, cinq références ROI maximum par track,
seuil d’alerte sensible de 0,90 et trois éléments au maximum en vol dans le
parcours de priorité. Le runtime segmenté utilise des segments d’une seconde,
une fenêtre de réordonnancement de quatre segments, un TTL d’épisode de 45 s
et au plus 32 épisodes actifs.

Le détecteur réel est limité au replay contrôlé lorsque le mode
`real_replay` est explicitement demandé. Sans backend disponible, le worker
retourne un statut d’indisponibilité typé et poursuit proprement le cycle sans
fabriquer d’observation.

Les payloads bus sont des résumés validés. Les images, embeddings, plaques et
autres médias ne sont jamais sérialisés comme données brutes : seuls des
références locales opaques et des champs normalisés autorisés par le contrat
peuvent apparaître. Le worker ne déclenche aucune action physique et ne
contourne ni Core ni le Safety Gate.

Le Vision Worker est une capacité optionnelle de Discovery. Son socket est
`/run/synora/vision-worker.sock`; les unités systemd créent auparavant le
répertoire runtime `synora` avec les permissions attendues.

Au démarrage, chaque modèle reçoit un statut : `present`, `missing`, `invalid`,
`unavailable` ou `available`. Les erreurs RKNN sont typées (`missing_file`,
`invalid_model`, `rknn_runtime_error`, `backend_unavailable`).

ArcFace absent ne désactive que `face_recognition`. `FaceRecognizer` conserve
une capability explicite et renvoie un résultat indisponible au traitement,
sans exception fatale. Le même principe est appliqué aux détecteurs dont le
modèle manque. Le worker fournit `/healthz` et `/capabilities`, ainsi qu'une
réponse JSON claire `no_models_available` lorsqu'un clip ne peut pas être
traité.

Discovery réessaie la connexion au socket avec un nombre limité de tentatives,
marque la capability indisponible et continue son serveur d'ingress. Les
événements de worker sont limités dans le temps ; plusieurs crashes donnent un
événement `runtime.component.flapping` au lieu de polluer le flux d’observations.

`make doctor` et `make install-models` indiquent explicitement les fichiers
RKNN manquants. Ils ne rendent pas le runtime fatal lorsque les modèles ne sont
pas présents.
