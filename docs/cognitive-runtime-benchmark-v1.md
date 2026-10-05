# Benchmark runtime cognitif V1

Le benchmark de runtime cognitif est séparé du runner système. La commande
`make cognitive-runtime-benchmark-v1` mesure le chargeur CPU, l’initialisation
Core, les décisions, les latences des quatre heads V1, le Store et le dispatch
d’action simulé. Il n’ouvre ni caméra, ni réseau, ni périphérique physique.

La couverture de bout en bout appartient exclusivement à
`make test-central-v1`, avec ses fixtures déclaratives versionnées. Les
anciens replays vidéo et injecteurs ont été supprimés ;
`/home/rock/test3.mp4` reste réservé au smoke test explicite RTMPose et ne
sert pas à mesurer ou qualifier une chute.
