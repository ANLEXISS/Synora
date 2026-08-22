# Synora V1 — changelog de la RC locale

## Candidate local

- J16–J20 : rétention, droits sensibles, backup/restore, OTA centrale, OTA
  caméra et récupération.
- J21 : harness hardware reproductible, sans résultat physique inventé.
- J22 : qualification fixture caméra/réseau pour trois caméras, avec décisions
  explicites des capacités non qualifiées.
- J23 : parcours web local, santé/version/stockage/erreurs, responsive et
  audit production npm sans vulnérabilité.
- J24 : manifeste source, SBOM/licences, provisioning, burn-in, support et
  matrice réglementaire sans fausse certification.

## Limites bloquantes avant `v1-rc1`

- unité cible, BOM confirmée, radio/caméras/capteurs et coupure réelle;
- adaptateur WireGuard/netlink et client distant réel;
- mesures Vision réelles pour faux positifs/faux négatifs;
- preuves externes CE, RED, RoHS et WEEE.

La branche et le tag local de candidate doivent porter le statut
`software_rc_audited_external_gates_open`. Le tag `v1-rc1` ne doit pas être
créé tant que ces preuves obligatoires ne sont pas disponibles.
