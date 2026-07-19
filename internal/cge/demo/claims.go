package demo

// Claims is the single local source for the demonstrator's product claims.
// Qualification numbers are intentionally absent unless a versioned report
// is loaded by a future integration.
func Claims() ClaimsFile {
	return ClaimsFile{Claims: []Claim{
		{ID: "local-first", Title: "Traitement local", Status: "demonstrated", Evidence: []string{"synthetic local run", "field-trial privacy boundary"}, Limitations: []string{"Le périmètre démontré est le CGE ; l’écosystème complet peut avoir d’autres communications."}},
		{ID: "deterministic-replay", Title: "Mémoire rejouable", Status: "proven", Evidence: []string{"ShadowEngine.DurableStateDigest", "journal recovery tests"}, Limitations: []string{"Le store d’évaluation de déviation est volontairement éphémère."}},
		{ID: "ambiguity-preservation", Title: "Hypothèses concurrentes", Status: "proven", Evidence: []string{"association ambiguity qualification", "real ShadowEngine fixture"}, Limitations: []string{"La résolution automatique n’est pas activée."}},
		{ID: "routine-learning", Title: "Routines locales", Status: "demonstrated", Evidence: []string{"routine extraction and registry snapshots"}, Limitations: []string{"La calibration comportementale reste à valider sur des foyers réels."}},
		{ID: "pre-learning-deviation", Title: "Déviation avant apprentissage", Status: "proven", Evidence: []string{"deviation-v1 shadow integration"}, Limitations: []string{"Un épisode synthétique n’est pas une mesure de sécurité."}},
		{ID: "continuous-adaptation", Title: "Adaptation progressive", Status: "demonstrated", Evidence: []string{"routine shift campaign"}, Limitations: []string{"La vitesse d’adaptation doit encore être calibrée sur le terrain."}},
		{ID: "transactional-durability", Title: "Durabilité transactionnelle", Status: "proven", Evidence: []string{"WAL and recovery qualification"}, Limitations: []string{"Le démonstrateur n’utilise jamais le runtime installé."}},
		{ID: "privacy-preserving-field-trial", Title: "Essai terrain pseudonymisé", Status: "proven", Evidence: []string{"field-trial privacy tests"}, Limitations: []string{"La démonstration elle-même est entièrement synthétique."}},
		{ID: "security-authority", Title: "Autorité de sécurité", Status: "future", Evidence: []string{"Shadow Mode boundary"}, Limitations: []string{"Le CGE ne décide pas d’une intrusion et ne déclenche aucune action."}},
	}}
}
