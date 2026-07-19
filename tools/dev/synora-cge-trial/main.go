package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	cge "synora/internal/cge"
	"synora/internal/cge/fieldtrial"
	"synora/internal/version"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] == "--help" || os.Args[1] == "-h" {
		usage()
		return
	}
	var err error
	switch os.Args[1] {
	case "start":
		err = start(os.Args[2:])
	case "status":
		err = status(os.Args[2:])
	case "checkpoint":
		err = checkpoint(os.Args[2:])
	case "close":
		err = closeSession(os.Args[2:])
	case "sessions":
		err = sessions(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	case "annotate":
		err = annotate(os.Args[2:])
	case "report":
		err = report(os.Args[2:])
	case "export":
		err = exportSession(os.Args[2:])
	case "prune":
		err = prune(os.Args[2:])
	case "topology":
		err = topologyCommand(os.Args[2:])
	case "key":
		err = keyCommand(os.Args[2:])
	case "preflight":
		err = preflight(os.Args[2:])
	case "prepare":
		err = prepare(os.Args[2:])
	case "doctor":
		err = doctor(os.Args[2:])
	case "smoke-check":
		err = smokeCheck(os.Args[2:])
	case "export-verify":
		err = exportVerify(os.Args[2:])
	default:
		usage()
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("synora-cge-trial commands: start status checkpoint close sessions verify annotate report export prune topology key preflight prepare doctor smoke-check export-verify")
	fmt.Println("common options: --root <dir> --session <id> --json")
}

type commonFlags struct {
	root    string
	session string
	json    bool
}

func common(fs *flag.FlagSet) *commonFlags {
	value := &commonFlags{}
	fs.StringVar(&value.root, "root", fieldtrial.DefaultRootDir, "field-trial root")
	fs.StringVar(&value.session, "session", "", "session id")
	fs.BoolVar(&value.json, "json", false, "JSON output")
	return value
}

func adminConfig(value commonFlags, repair bool) fieldtrial.Config {
	config := fieldtrial.DefaultConfig()
	config.Enabled = true
	config.RootDir = value.root
	config.SessionID = value.session
	config.RepairTerminalPartial = repair
	return config
}

func sessionDir(value commonFlags) (string, error) {
	if value.session == "" {
		return "", errors.New("--session is required")
	}
	if !validSession(value.session) {
		return "", fieldtrial.ErrInvalidSessionID
	}
	return filepath.Join(value.root, value.session), nil
}

func start(args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	value := common(fs)
	envFile := fs.String("env-file", "", "reference environment file")
	keyFile := fs.String("key-file", "", "pseudonymization key")
	topologyFile := fs.String("topology-file", "", "static topology")
	deploymentPath := fs.String("deployment-manifest", "", "prepared deployment manifest")
	withoutManifest := fs.Bool("without-deployment-manifest", false, "development/test override")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rootOverride := ""
	if flagWasSet(fs, "root") {
		rootOverride = value.root
	}
	shadowConfig, fingerprint, report, err := operationalPreflight(*envFile, rootOverride, *keyFile, *topologyFile)
	if err != nil {
		return err
	}
	if !shadowConfig.Enabled {
		return errors.New("shadow configuration is disabled")
	}
	if !report.Success {
		return errors.New("preflight failed")
	}
	if *deploymentPath == "" && !*withoutManifest {
		return errors.New("deployment manifest is required; use --without-deployment-manifest only for development")
	}
	if *deploymentPath != "" {
		keyPath := *keyFile
		if keyPath == "" {
			keyPath = shadowConfig.FieldTrial.PseudonymizationKeyFile
		}
		topologyPath := *topologyFile
		if topologyPath == "" {
			topologyPath = shadowConfig.FieldTrial.TopologyFile
		}
		if err := compareDeployment(*deploymentPath, fingerprint.CombinedFingerprint, *envFile, keyPath, topologyPath); err != nil {
			return err
		}
	}
	config := shadowConfig.FieldTrial
	config.Enabled = true
	if value.session != "" {
		config.SessionID = value.session
	}
	if *keyFile != "" {
		config.PseudonymizationKeyFile = *keyFile
	}
	if *topologyFile != "" {
		config.TopologyFile = *topologyFile
	}
	metadata := cge.FieldTrialMetadataForConfig(shadowConfig)
	metadata.CognitiveConfigurationFingerprint = fingerprint.CombinedFingerprint
	recorder, err := fieldtrial.Open(context.Background(), config, metadata)
	if err != nil {
		return err
	}
	manifest := recorder.Manifest()
	shutdownErr := recorder.Shutdown(context.Background())
	if value.json {
		return printJSON(struct {
			Manifest    fieldtrial.SessionManifest `json:"manifest"`
			Fingerprint string                     `json:"cognitive_configuration_fingerprint"`
		}{manifest, fingerprint.CombinedFingerprint})
	}
	fmt.Printf("session=%s status=%s root=%s\n", manifest.SessionID, manifest.Status, filepath.Join(config.RootDir, manifest.SessionID))
	return shutdownErr
}

func status(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	value := common(fs)
	envFile := fs.String("env-file", "", "reference environment file")
	keyFile := fs.String("key-file", "", "pseudonymization key")
	topologyFile := fs.String("topology-file", "", "static topology")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := sessionDir(*value)
	if err != nil {
		return err
	}
	manifest, err := fieldtrial.ReadManifest(dir)
	if err != nil {
		return err
	}
	statusValue := operationalStatus(dir, manifest)
	if *envFile != "" {
		loaded, loadErr := loadOperationalConfig(*envFile, "", *keyFile, *topologyFile)
		if loadErr != nil {
			return loadErr
		}
		populateQuotaStatus(&statusValue, filepath.Dir(dir), loaded.Config.FieldTrial.MaximumTotalBytes)
		statusValue.ConfigurationDrift = loaded.Fingerprint.CombinedFingerprint != manifest.CognitiveConfigurationFingerprint
		if statusValue.ConfigurationDrift {
			statusValue.Warnings = append(statusValue.Warnings, "configuration drift")
		}
	}
	if value.json {
		return printJSON(statusValue)
	}
	fmt.Printf("session=%s status=%s events=%d segments=%d drift=%t bytes_path=%s\n", manifest.SessionID, manifest.Status, manifest.EventCount, manifest.SegmentCount, statusValue.ConfigurationDrift, dir)
	return nil
}

func checkpoint(args []string) error {
	fs := flag.NewFlagSet("checkpoint", flag.ContinueOnError)
	value := common(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	config := adminConfig(*value, false)
	recorder, err := fieldtrial.Open(context.Background(), config, fieldtrial.OpenMetadata{})
	if err != nil {
		return err
	}
	manifest, err := recorder.Checkpoint(context.Background(), time.Now().UTC())
	shutdownErr := recorder.Shutdown(context.Background())
	if err != nil {
		return err
	}
	if value.json {
		if jsonErr := printJSON(manifest); jsonErr != nil {
			return jsonErr
		}
	} else {
		fmt.Printf("checkpoint session=%s sequence=%d\n", manifest.SessionID, manifest.LastSequence)
	}
	return shutdownErr
}

func closeSession(args []string) error {
	fs := flag.NewFlagSet("close", flag.ContinueOnError)
	value := common(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	recorder, err := fieldtrial.Open(context.Background(), adminConfig(*value, false), fieldtrial.OpenMetadata{})
	if err != nil {
		return err
	}
	err = recorder.Close(context.Background(), time.Now().UTC())
	manifest := recorder.Manifest()
	if value.json && err == nil {
		return printJSON(manifest)
	}
	if err == nil {
		fmt.Printf("closed session=%s\n", manifest.SessionID)
	}
	return err
}

func sessions(args []string) error {
	fs := flag.NewFlagSet("sessions", flag.ContinueOnError)
	root := fs.String("root", fieldtrial.DefaultRootDir, "field-trial root")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	entries, err := os.ReadDir(*root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	manifests := make([]fieldtrial.SessionManifest, 0)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		manifest, readErr := fieldtrial.ReadManifest(filepath.Join(*root, entry.Name()))
		if readErr == nil {
			manifests = append(manifests, manifest)
		}
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].CreatedAt.Before(manifests[j].CreatedAt) })
	if *asJSON {
		return printJSON(manifests)
	}
	for _, manifest := range manifests {
		fmt.Printf("%s %s %d\n", manifest.SessionID, manifest.Status, manifest.EventCount)
	}
	return nil
}

func verify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	value := common(fs)
	repair := fs.Bool("repair-terminal-partial", false, "repair only a terminal incomplete line")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := sessionDir(*value)
	if err != nil {
		return err
	}
	manifest, err := fieldtrial.VerifySession(context.Background(), dir, *repair)
	if err != nil {
		return err
	}
	if value.json {
		return printJSON(manifest)
	}
	fmt.Printf("verified session=%s events=%d annotations=%d\n", manifest.SessionID, manifest.EventCount, manifest.AnnotationCount)
	return nil
}

func annotate(args []string) error {
	fs := flag.NewFlagSet("annotate", flag.ContinueOnError)
	value := common(fs)
	event := fs.String("event", "", "opaque event reference")
	label := fs.String("label", "", "annotation label")
	source := fs.String("source", "manual", "bounded source code")
	note := fs.String("note-code", "", "bounded note code")
	if err := fs.Parse(args); err != nil {
		return err
	}
	recorder, err := fieldtrial.Open(context.Background(), adminConfig(*value, false), fieldtrial.OpenMetadata{})
	if err != nil {
		return err
	}
	err = recorder.AddAnnotation(context.Background(), fieldtrial.AnnotationInput{EventRef: *event, Label: fieldtrial.AnnotationLabel(*label), AnnotatedAt: time.Now().UTC(), Source: *source, NoteCode: *note})
	shutdownErr := recorder.Shutdown(context.Background())
	if err != nil {
		return err
	}
	return shutdownErr
}

func report(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	value := common(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := sessionDir(*value)
	if err != nil {
		return err
	}
	result, err := fieldtrial.BuildReport(context.Background(), dir)
	if err != nil {
		return err
	}
	if value.json {
		return printJSON(result)
	}
	fmt.Printf("session=%s events=%d technical_success=%t\n", result.SessionID, result.EventCount, result.TechnicalSuccess)
	return nil
}

func exportSession(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	value := common(fs)
	output := fs.String("output", "", "export directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *output == "" {
		return errors.New("--output is required")
	}
	dir, err := sessionDir(*value)
	if err != nil {
		return err
	}
	manifest, err := fieldtrial.ExportSession(context.Background(), dir, *output, fieldtrial.ExportOptions{IncludeEvents: true, IncludeAnnotations: true, IncludeDailySummaries: true})
	if err != nil {
		return err
	}
	if value.json {
		return printJSON(manifest)
	}
	fmt.Printf("exported session=%s files=%d\n", manifest.Session.SessionID, len(manifest.Files))
	return nil
}

func prune(args []string) error {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	root := fs.String("root", fieldtrial.DefaultRootDir, "field-trial root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	config := fieldtrial.DefaultConfig()
	config.Enabled = true
	config.RootDir = *root
	deleted, err := fieldtrial.Prune(context.Background(), config, time.Now().UTC())
	if err != nil {
		return err
	}
	fmt.Printf("deleted_sessions=%d\n", deleted)
	return nil
}

func topologyCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("topology requires validate or inspect")
	}
	fs := flag.NewFlagSet("topology", flag.ContinueOnError)
	file := fs.String("file", "", "topology JSON")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("--file is required")
	}
	topology, err := fieldtrial.LoadTopologyFile(*file)
	if err != nil {
		return err
	}
	switch args[0] {
	case "validate":
		if *asJSON {
			return printJSON(struct {
				Revision string `json:"revision"`
				Nodes    int    `json:"nodes"`
				Edges    int    `json:"edges"`
			}{topology.Revision, len(topology.Nodes), len(topology.Edges)})
		}
		fmt.Printf("valid revision=%s nodes=%d edges=%d\n", topology.Revision, len(topology.Nodes), len(topology.Edges))
		return nil
	case "inspect":
		if *asJSON {
			return printJSON(topology)
		}
		for _, node := range topology.Nodes {
			fmt.Printf("node=%s kind=%s zone=%s parent=%s entry=%t exterior=%t\n", node.ID, node.Kind, node.ZoneID, node.ParentID, node.EntryPoint, node.Exterior)
		}
		for _, edge := range topology.Edges {
			fmt.Printf("edge=%s->%s directed=%t traversal=%s\n", edge.From, edge.To, edge.Directed, edge.TraversalKind)
		}
		return nil
	default:
		return fmt.Errorf("unknown topology command %q", args[0])
	}
}

func keyCommand(args []string) error {
	if len(args) == 0 || args[0] != "generate" {
		return errors.New("key generate is required")
	}
	fs := flag.NewFlagSet("key generate", flag.ContinueOnError)
	output := fs.String("output", "", "key path")
	force := fs.Bool("force", false, "overwrite existing key")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *output == "" {
		return errors.New("--output is required")
	}
	digest, err := fieldtrial.GenerateKey(*output, *force)
	if err != nil {
		return err
	}
	fmt.Printf("key_generated path=%s fingerprint=%s\n", *output, digest)
	return nil
}

type loadedOperationalConfig struct {
	Config                 cge.ShadowConfig
	Fingerprint            cge.CognitiveConfigurationFingerprint
	EnvironmentFingerprint string
}

func loadOperationalConfig(path string, root, key, topology string) (loadedOperationalConfig, error) {
	values := map[string]string{}
	if path != "" {
		data, err := os.Open(path)
		if err != nil {
			return loadedOperationalConfig{}, err
		}
		defer data.Close()
		scanner := bufio.NewScanner(data)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "export ")
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				return loadedOperationalConfig{}, errors.New("invalid environment line")
			}
			values[strings.TrimSpace(parts[0])] = strings.Trim(strings.TrimSpace(parts[1]), "\"")
		}
		if err := scanner.Err(); err != nil {
			return loadedOperationalConfig{}, err
		}
	}
	getenv := func(name string) string {
		if value, ok := values[name]; ok {
			return value
		}
		return os.Getenv(name)
	}
	config, err := cge.LoadShadowConfig(getenv)
	if err != nil {
		return loadedOperationalConfig{}, err
	}
	if root != "" {
		config.FieldTrial.RootDir = root
	}
	if key != "" {
		config.FieldTrial.PseudonymizationKeyFile = key
	}
	if topology != "" {
		config.FieldTrial.TopologyFile = topology
	}
	fingerprint, err := cge.CognitiveConfigurationFingerprintFor(config)
	if err != nil {
		return loadedOperationalConfig{}, err
	}
	envFingerprint := ""
	if path != "" {
		envFingerprint, err = fieldtrial.FingerprintFile(path)
		if err != nil {
			return loadedOperationalConfig{}, err
		}
	}
	return loadedOperationalConfig{config, fingerprint, envFingerprint}, nil
}

func operationalPreflight(envFile, root, key, topology string) (cge.ShadowConfig, cge.CognitiveConfigurationFingerprint, fieldtrial.PreflightReport, error) {
	loaded, err := loadOperationalConfig(envFile, root, key, topology)
	if err != nil {
		return cge.ShadowConfig{}, cge.CognitiveConfigurationFingerprint{}, fieldtrial.PreflightReport{}, err
	}
	report, err := fieldtrial.RunPreflight(context.Background(), fieldtrial.PreflightOptions{Config: loaded.Config.FieldTrial, KeyFile: key, TopologyFile: topology, CognitiveConfigurationFingerprint: loaded.Fingerprint.CombinedFingerprint})
	if err != nil {
		return loaded.Config, loaded.Fingerprint, report, err
	}
	if !loaded.Config.Enabled {
		report.Success = false
		report.BlockingReasons = append(report.BlockingReasons, "config.shadow_disabled")
	}
	if !loaded.Config.FieldTrial.Enabled {
		report.Success = false
		report.BlockingReasons = append(report.BlockingReasons, "config.field_trial_disabled")
	}
	return loaded.Config, loaded.Fingerprint, report, nil
}

func preflight(args []string) error {
	fs := flag.NewFlagSet("preflight", flag.ContinueOnError)
	env := fs.String("env-file", "", "environment file")
	root := fs.String("root", "", "field-trial root")
	key := fs.String("key-file", "", "key file")
	topology := fs.String("topology-file", "", "topology file")
	output := fs.String("output", "", "report output")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, _, report, err := operationalPreflight(*env, *root, *key, *topology)
	if err != nil {
		return err
	}
	if *output != "" {
		if err := writeJSONFile(*output, report); err != nil {
			return err
		}
	}
	if *asJSON {
		return printJSON(report)
	}
	fmt.Printf("preflight success=%t available_bytes=%d quota=%d\n", report.Success, report.AvailableBytes, report.ConfiguredQuotaBytes)
	for _, reason := range report.BlockingReasons {
		fmt.Println("blocking", reason)
	}
	return nil
}

func prepare(args []string) error {
	fs := flag.NewFlagSet("prepare", flag.ContinueOnError)
	env := fs.String("env-file", "", "environment file")
	root := fs.String("root", "", "field-trial root")
	key := fs.String("key-file", "", "key file")
	topology := fs.String("topology-file", "", "topology file")
	session := fs.String("session", "", "planned session id")
	output := fs.String("output", "", "manifest output")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	loadedConfig, fingerprint, report, err := operationalPreflight(*env, *root, *key, *topology)
	if err != nil {
		return err
	}
	if !report.Success {
		return errors.New("preflight failed")
	}
	environmentFingerprint := ""
	if *env != "" {
		environmentFingerprint, err = fieldtrial.FingerprintFile(*env)
		if err != nil {
			return err
		}
	}
	versionManifest := version.LoadOrFallback("")
	manifest := fieldtrial.DeploymentManifest{PreparedAt: time.Now().UTC(), SynoraVersion: versionManifest.SynoraVersion, Commit: versionManifest.GitCommit, Architecture: runtime.GOARCH, CognitiveConfigurationFingerprint: fingerprint.CombinedFingerprint, EnvironmentTemplateFingerprint: environmentFingerprint, FieldTrialRoot: loadedConfig.FieldTrial.RootDir, PlannedSessionID: *session, PreflightPassed: true}
	topologyPath := *topology
	if topologyPath == "" {
		topologyPath = loadedConfig.FieldTrial.TopologyFile
	}
	if topologyPath != "" {
		manifest.TopologyFingerprint, err = fieldtrial.FingerprintFile(topologyPath)
		if err != nil {
			return err
		}
	}
	keyPath := *key
	if keyPath == "" {
		keyPath = loadedConfig.FieldTrial.PseudonymizationKeyFile
	}
	if keyPath != "" {
		manifest.KeyFingerprint, err = fieldtrial.FingerprintFile(keyPath)
		if err != nil {
			return err
		}
	}
	if *output == "" {
		return errors.New("--output is required")
	}
	if err := fieldtrial.WriteDeploymentManifest(*output, manifest); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(manifest)
	}
	fmt.Printf("prepared manifest=%s fingerprint=%s\n", *output, manifest.CognitiveConfigurationFingerprint)
	return nil
}

func compareDeployment(path, fingerprint, env, key, topology string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var manifest fieldtrial.DeploymentManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if manifest.CognitiveConfigurationFingerprint != fingerprint {
		return fieldtrial.ErrConfigurationDrift
	}
	if env != "" && manifest.EnvironmentTemplateFingerprint != "" {
		actual, err := fieldtrial.FingerprintFile(env)
		if err != nil || actual != manifest.EnvironmentTemplateFingerprint {
			return fieldtrial.ErrConfigurationDrift
		}
	}
	if key != "" && manifest.KeyFingerprint != "" {
		actual, err := fieldtrial.FingerprintFile(key)
		if err != nil || actual != manifest.KeyFingerprint {
			return fieldtrial.ErrConfigurationDrift
		}
	}
	if topology != "" && manifest.TopologyFingerprint != "" {
		actual, err := fieldtrial.FingerprintFile(topology)
		if err != nil || actual != manifest.TopologyFingerprint {
			return fieldtrial.ErrConfigurationDrift
		}
	}
	return nil
}

func doctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	value := common(fs)
	env := fs.String("env-file", "", "environment file")
	key := fs.String("key-file", "", "key file")
	topology := fs.String("topology-file", "", "topology file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := sessionDir(*value)
	if err != nil {
		return err
	}
	manifest, err := fieldtrial.VerifySession(context.Background(), dir, false)
	if err != nil {
		return err
	}
	status := operationalStatus(dir, manifest)
	if *env != "" {
		loaded, err := loadOperationalConfig(*env, "", *key, *topology)
		if err != nil {
			return err
		}
		populateQuotaStatus(&status, filepath.Dir(dir), loaded.Config.FieldTrial.MaximumTotalBytes)
		status.ConfigurationDrift = loaded.Fingerprint.CombinedFingerprint != manifest.CognitiveConfigurationFingerprint
		if status.ConfigurationDrift {
			status.Warnings = append(status.Warnings, "configuration drift")
		}
	}
	if *key != "" {
		if info, statErr := os.Lstat(*key); statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			status.BlockingReasons = append(status.BlockingReasons, "key.invalid")
		}
	}
	if *topology != "" {
		if _, topologyErr := fieldtrial.LoadTopologyFile(*topology); topologyErr != nil {
			status.BlockingReasons = append(status.BlockingReasons, "topology.invalid")
		}
	}
	if value.json {
		return printJSON(status)
	}
	state := doctorState(manifest, status)
	fmt.Printf("%s session=%s events=%d bytes=%d\n", state, status.SessionID, status.EventCount, status.TotalBytes)
	return nil
}

func doctorState(manifest fieldtrial.SessionManifest, status fieldtrial.OperationalTrialStatus) string {
	if manifest.Status == fieldtrial.SessionDegraded || len(status.BlockingReasons) > 0 {
		return "degraded"
	}
	if status.ConfigurationDrift || len(status.Warnings) > 0 {
		return "warning"
	}
	return "healthy"
}

func smokeCheck(args []string) error {
	fs := flag.NewFlagSet("smoke-check", flag.ContinueOnError)
	value := common(fs)
	required := fs.Uint64("require-events", 0, "minimum events")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := sessionDir(*value)
	if err != nil {
		return err
	}
	manifest, err := fieldtrial.VerifySession(context.Background(), dir, false)
	if err != nil {
		return err
	}
	if manifest.Status != fieldtrial.SessionOpen && manifest.Status != fieldtrial.SessionRecovered {
		return errors.New("session is not open")
	}
	if manifest.EventCount < *required {
		return fmt.Errorf("require-events not met")
	}
	if value.json {
		return printJSON(manifest)
	}
	fmt.Printf("smoke_check passed events=%d\n", manifest.EventCount)
	return nil
}

func exportVerify(args []string) error {
	fs := flag.NewFlagSet("export-verify", flag.ContinueOnError)
	dir := fs.String("dir", "", "export directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return errors.New("--dir is required")
	}
	if err := fieldtrial.VerifyExport(context.Background(), *dir); err != nil {
		return err
	}
	fmt.Println("export_verify passed")
	return nil
}

func operationalStatus(dir string, manifest fieldtrial.SessionManifest) fieldtrial.OperationalTrialStatus {
	status := fieldtrial.OperationalTrialStatus{SessionID: manifest.SessionID, Status: string(manifest.Status), StartedAt: manifest.CreatedAt, CognitiveConfigurationFingerprint: manifest.CognitiveConfigurationFingerprint, EventCount: manifest.EventCount, AnnotationCount: manifest.AnnotationCount, SegmentCount: manifest.SegmentCount, RecorderState: string(manifest.Status)}
	if manifest.ClosedAt != nil {
		status.Duration = manifest.ClosedAt.Sub(manifest.CreatedAt)
	} else {
		status.Duration = time.Since(manifest.CreatedAt)
	}
	if events, _, _, err := fieldtrial.ReadEvents(context.Background(), dir); err == nil && len(events) > 0 {
		last := events[len(events)-1].ObservedAt
		status.LastEventAt = &last
	}
	var total int64
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "events-") || entry.Name() == "annotations.ndjson" {
				if info, err := entry.Info(); err == nil {
					total += info.Size()
				}
			}
		}
	}
	status.TotalBytes = total
	return status
}

func populateQuotaStatus(status *fieldtrial.OperationalTrialStatus, root string, quota int64) {
	if status == nil || quota <= 0 {
		return
	}
	var usage syscall.Statfs_t
	if err := syscall.Statfs(root, &usage); err != nil {
		return
	}
	status.AvailableBytes = int64(usage.Bavail) * int64(usage.Bsize)
	status.QuotaRemainingBytes = quota - status.TotalBytes
	if status.QuotaRemainingBytes < 0 {
		status.QuotaRemainingBytes = 0
	}
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o640)
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func validSession(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/\\\x00\r\n:")
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(value *flag.Flag) {
		if value.Name == name {
			set = true
		}
	})
	return set
}
