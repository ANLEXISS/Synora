# Vision Worker

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
événement `runtime.component.flapping` au lieu de polluer le CGE.

`make doctor` et `make install-models` indiquent explicitement les fichiers
RKNN manquants. Ils ne rendent pas le runtime fatal lorsque les modèles ne sont
pas présents.
