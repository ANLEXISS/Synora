# Diagnostics opérateur

Ce répertoire est réservé aux outils de support et de diagnostic qui ne sont
pas des services runtime. Les diagnostics réseau, santé, vision et système
doivent rester admin/opérateur, redacted et sans secret en sortie.

Le harnais système principal reste `tools/synora_system_test.sh` car les cibles
Make et la documentation le référencent déjà. Une migration de chemin devra
ajouter un wrapper de compatibilité et ses tests.
