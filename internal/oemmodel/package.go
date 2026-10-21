// Package oemmodel validates local, declarative OEM model packages. It never
// executes package-provided code or loads a model into a runtime.
package oemmodel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const SchemaVersion = "synora.model.package/v1"

var digestPattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
var safeID = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)
var strictVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

type Package struct {
	SchemaVersion          string            `json:"schema_version"`
	PackageID              string            `json:"package_id"`
	ModuleID               string            `json:"module_id"`
	ModelVersion           string            `json:"model_version"`
	ArtifactFormat         string            `json:"artifact_format"`
	ArtifactFilename       string            `json:"artifact_filename"`
	ArtifactSHA256         string            `json:"artifact_sha256"`
	RuntimeAdapterID       string            `json:"runtime_adapter_id"`
	PostprocessAdapterID   string            `json:"postprocess_adapter_id"`
	TargetArchitectures    []string          `json:"target_architectures"`
	MinimumRuntimeVersions map[string]string `json:"minimum_runtime_versions"`
	Input                  InputContract     `json:"input_contract"`
	Output                 OutputContract    `json:"output_contract"`
	LabelContract          string            `json:"label_contract"`
	NormalizationContract  string            `json:"normalization_contract"`
	Provenance             string            `json:"provenance"`
	LicenseReference       string            `json:"license_reference"`
	QualificationState     string            `json:"qualification_state"`
	CompatibilityPolicy    string            `json:"compatibility_policy"`
}

type InputContract struct {
	TensorName   string    `json:"tensor_name"`
	Shape        []int     `json:"shape"`
	DType        string    `json:"dtype"`
	Layout       string    `json:"layout"`
	ColorOrder   string    `json:"color_order"`
	ResizePolicy string    `json:"resize_policy"`
	Mean         []float64 `json:"mean,omitempty"`
	Std          []float64 `json:"std,omitempty"`
	Quantization string    `json:"quantization,omitempty"`
}
type OutputContract struct {
	TensorNames        []string `json:"tensor_names"`
	ExpectedShapes     [][]int  `json:"expected_shapes"`
	DType              string   `json:"dtype"`
	SemanticOutputKind string   `json:"semantic_output_kind"`
}

type Adapter struct {
	ID            string
	Format        string
	Architectures []string
	Modules       []string
	TestOnly      bool
}
type Registry struct {
	adapters    map[string]Adapter
	postprocess map[string][]string
}

type Authorization struct {
	PackageID      string
	ArtifactSHA256 string
	AllowLoad      bool
	TestProfile    bool
}
type Availability struct {
	State         string
	ErrorCategory string
}
type SemanticResult struct {
	ModuleID  string
	Status    string
	Aggregate map[string]float64
}

// RuntimeAdapter is the only extension point between validated packages and
// module semantics. Implementations are statically linked; manifests carry no
// executable code. Current hardware adapters intentionally fail closed until
// their audited runtime implementations are included.
type RuntimeAdapter interface {
	ID() string
	ValidatePackage(Package) error
	ObserveAvailability(context.Context) Availability
	LoadIfAuthorized(context.Context, Package, Authorization) error
	RunInference(context.Context, Package, []float32) (SemanticResult, error)
	NormalizeSemanticResult(Package, SemanticResult) (SemanticResult, error)
	Release(context.Context) error
}

type PostprocessAdapter interface {
	ID() string
	Normalize(Package, SemanticResult) (SemanticResult, error)
	Release(context.Context) error
}

type aggregatePostprocessor struct{ id, module string }

func (p aggregatePostprocessor) ID() string { return p.id }
func (p aggregatePostprocessor) Normalize(pkg Package, result SemanticResult) (SemanticResult, error) {
	if pkg.ModuleID != result.ModuleID || (p.module != "*" && pkg.ModuleID != p.module) {
		return SemanticResult{}, errors.New("postprocess module mismatch")
	}
	if !member([]string{"available", "unavailable", "simulated_test", "blocked"}, result.Status) {
		return SemanticResult{}, errors.New("semantic status invalid")
	}
	for key, value := range result.Aggregate {
		if !member([]string{"presence_ratio", "quality_ratio", "motion_ratio", "availability_ratio"}, key) || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return SemanticResult{}, errors.New("aggregate semantic result invalid")
		}
	}
	return result, nil
}
func (aggregatePostprocessor) Release(context.Context) error { return nil }
func PostprocessAdapters() map[string]PostprocessAdapter {
	return map[string]PostprocessAdapter{
		"person-aggregate-v1": aggregatePostprocessor{id: "person-aggregate-v1", module: "vision-person"}, "pose-aggregate-v1": aggregatePostprocessor{id: "pose-aggregate-v1", module: "vision-pose"},
		"face-aggregate-v1": aggregatePostprocessor{id: "face-aggregate-v1", module: "vision-face"}, "vehicle-aggregate-v1": aggregatePostprocessor{id: "vehicle-aggregate-v1", module: "vision-vehicle"},
		"plate-aggregate-v1": aggregatePostprocessor{id: "plate-aggregate-v1", module: "vision-plate"}, "animal-aggregate-v1": aggregatePostprocessor{id: "animal-aggregate-v1", module: "vision-animal"},
		"mock-aggregate-v1": aggregatePostprocessor{id: "mock-aggregate-v1", module: "*"},
	}
}
func ValidatePostprocessSelection(p Package) error {
	adapter, ok := PostprocessAdapters()[p.PostprocessAdapterID]
	if !ok {
		return errors.New("postprocess adapter unavailable")
	}
	_, err := adapter.Normalize(p, SemanticResult{ModuleID: p.ModuleID, Status: "unavailable"})
	return err
}

type unavailableAdapter struct {
	id    string
	state Availability
}

func (a unavailableAdapter) ID() string { return a.id }
func (a unavailableAdapter) ValidatePackage(p Package) error {
	if p.RuntimeAdapterID != a.id {
		return errors.New("runtime adapter mismatch")
	}
	return p.Validate(DefaultRegistry())
}
func (a unavailableAdapter) ObserveAvailability(context.Context) Availability { return a.state }
func (a unavailableAdapter) LoadIfAuthorized(_ context.Context, p Package, auth Authorization) error {
	if !auth.AllowLoad || auth.PackageID != p.PackageID || auth.ArtifactSHA256 != p.ArtifactSHA256 {
		return errors.New("model load authorization denied")
	}
	return errors.New("runtime adapter unavailable")
}
func (a unavailableAdapter) RunInference(context.Context, Package, []float32) (SemanticResult, error) {
	return SemanticResult{}, errors.New("runtime adapter unavailable")
}
func (a unavailableAdapter) NormalizeSemanticResult(_ Package, result SemanticResult) (SemanticResult, error) {
	return SemanticResult{}, errors.New("runtime adapter unavailable")
}
func (a unavailableAdapter) Release(context.Context) error { return nil }

func RuntimeAdapters() map[string]RuntimeAdapter {
	return map[string]RuntimeAdapter{
		"rk3588-rknn": unavailableAdapter{id: "rk3588-rknn", state: Availability{State: "unavailable", ErrorCategory: "runtime_not_implemented"}},
		"onnx-cpu":    unavailableAdapter{id: "onnx-cpu", state: Availability{State: "unavailable", ErrorCategory: "runtime_not_implemented"}},
		"mock":        mockRuntime{},
		"replay":      unavailableAdapter{id: "replay", state: Availability{State: "test_only", ErrorCategory: "replay_adapter_not_implemented"}},
	}
}

type mockRuntime struct{}

func (mockRuntime) ID() string { return "mock" }
func (mockRuntime) ValidatePackage(p Package) error {
	if p.RuntimeAdapterID != "mock" {
		return errors.New("runtime adapter mismatch")
	}
	return p.Validate(DefaultRegistry())
}
func (mockRuntime) ObserveAvailability(context.Context) Availability {
	return Availability{State: "test_only"}
}
func (mockRuntime) LoadIfAuthorized(_ context.Context, p Package, a Authorization) error {
	if err := p.Validate(DefaultRegistry()); err != nil {
		return err
	}
	if !a.AllowLoad || !a.TestProfile || p.ArtifactFormat != "mock" || a.PackageID != p.PackageID || !strings.EqualFold(a.ArtifactSHA256, p.ArtifactSHA256) {
		return errors.New("mock adapter authorization denied")
	}
	return nil
}
func (mockRuntime) RunInference(_ context.Context, p Package, features []float32) (SemanticResult, error) {
	if err := p.Validate(DefaultRegistry()); err != nil {
		return SemanticResult{}, err
	}
	if p.ArtifactFormat != "mock" || p.QualificationState == "qualified" || len(features) == 0 || len(features) > 16 {
		return SemanticResult{}, errors.New("mock request invalid")
	}
	for _, v := range features {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || v < 0 || v > 1 {
			return SemanticResult{}, errors.New("mock feature invalid")
		}
	}
	return SemanticResult{ModuleID: p.ModuleID, Status: "simulated_test"}, nil
}
func (m mockRuntime) NormalizeSemanticResult(p Package, r SemanticResult) (SemanticResult, error) {
	if err := p.Validate(DefaultRegistry()); err != nil {
		return SemanticResult{}, err
	}
	if r.ModuleID != p.ModuleID || r.Status != "simulated_test" || len(r.Aggregate) != 0 {
		return SemanticResult{}, errors.New("mock semantic result invalid")
	}
	return r, nil
}
func (mockRuntime) Release(context.Context) error { return nil }

func DefaultRegistry() Registry {
	return Registry{adapters: map[string]Adapter{
		"rk3588-rknn": {ID: "rk3588-rknn", Format: "rknn", Architectures: []string{"arm64"}, Modules: []string{"vision-person", "vision-pose"}},
		"onnx-cpu":    {ID: "onnx-cpu", Format: "onnx", Architectures: []string{"amd64", "arm64"}, Modules: []string{"vision-person", "vision-pose", "vision-face", "vision-vehicle", "vision-plate", "vision-animal"}},
		"mock":        {ID: "mock", Format: "mock", Architectures: []string{"any"}, Modules: allVisionModules(), TestOnly: true},
		"replay":      {ID: "replay", Format: "replay", Architectures: []string{"any"}, Modules: allVisionModules(), TestOnly: true},
	}, postprocess: map[string][]string{
		"person-v1": {"person-aggregate-v1", "mock-aggregate-v1"}, "pose-v1": {"pose-aggregate-v1", "mock-aggregate-v1"},
		"face-v1": {"face-aggregate-v1", "mock-aggregate-v1"}, "vehicle-v1": {"vehicle-aggregate-v1", "mock-aggregate-v1"},
		"plate-v1": {"plate-aggregate-v1", "mock-aggregate-v1"}, "animal-v1": {"animal-aggregate-v1", "mock-aggregate-v1"},
	}}
}
func allVisionModules() []string {
	return []string{"vision-person", "vision-pose", "vision-face", "vision-vehicle", "vision-plate", "vision-animal"}
}

func Decode(data []byte) (Package, error) {
	if len(data) == 0 || len(data) > 64*1024 {
		return Package{}, errors.New("package manifest size invalid")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p Package
	if err := dec.Decode(&p); err != nil {
		return Package{}, fmt.Errorf("decode package: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Package{}, errors.New("package manifest must contain one JSON value")
	}
	return p, p.Validate(DefaultRegistry())
}

func Load(path string) (Package, []byte, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return Package{}, nil, e
	}
	p, e := Decode(b)
	return p, b, e
}

func (p Package) Validate(r Registry) error {
	if p.SchemaVersion != SchemaVersion || !safeID.MatchString(p.PackageID) || !safeID.MatchString(p.ModuleID) || !strictVersion.MatchString(p.ModelVersion) {
		return errors.New("package identity/schema invalid")
	}
	if !member([]string{"vision-person", "vision-pose", "vision-face", "vision-vehicle", "vision-plate", "vision-animal"}, p.ModuleID) {
		return errors.New("unknown module")
	}
	if p.ArtifactFormat != "rknn" && p.ArtifactFormat != "onnx" && p.ArtifactFormat != "mock" && p.ArtifactFormat != "replay" {
		return errors.New("artifact format unsupported")
	}
	if p.ArtifactFilename == "" || filepath.IsAbs(p.ArtifactFilename) || filepath.Base(p.ArtifactFilename) != p.ArtifactFilename || strings.Contains(p.ArtifactFilename, "..") || strings.ContainsAny(p.ArtifactFilename, "\\/\x00") {
		return errors.New("artifact filename invalid")
	}
	if !digestPattern.MatchString(p.ArtifactSHA256) {
		return errors.New("artifact hash invalid")
	}
	a, ok := r.adapters[p.RuntimeAdapterID]
	if !ok || a.Format != p.ArtifactFormat || !member(a.Modules, p.ModuleID) {
		return errors.New("runtime adapter incompatible")
	}
	if !member(r.postprocess[strings.TrimPrefix(p.ModuleID, "vision-")+"-v1"], p.PostprocessAdapterID) {
		return errors.New("postprocess adapter incompatible")
	}
	if err := ValidatePostprocessSelection(p); err != nil {
		return errors.New("postprocess adapter incompatible")
	}
	if len(p.TargetArchitectures) == 0 || len(p.MinimumRuntimeVersions) == 0 {
		return errors.New("compatibility metadata missing")
	}
	for _, arch := range p.TargetArchitectures {
		if arch != "arm64" && arch != "amd64" && arch != "any" {
			return errors.New("target architecture invalid")
		}
	}
	for arch, v := range p.MinimumRuntimeVersions {
		if !member(p.TargetArchitectures, arch) || !version(v) {
			return errors.New("minimum runtime version invalid")
		}
	}
	if p.Input.TensorName == "" || !shapeValid(p.Input.Shape) || !member([]string{"float32", "uint8", "int8"}, p.Input.DType) || !member([]string{"NCHW", "NHWC"}, p.Input.Layout) || !member([]string{"RGB", "BGR", "none"}, p.Input.ColorOrder) || !member([]string{"letterbox", "stretch", "fixed"}, p.Input.ResizePolicy) {
		return errors.New("input contract invalid")
	}
	if p.Input.DType == "uint8" && p.Input.Quantization != "" {
		return errors.New("uint8 input cannot declare affine quantization")
	}
	if len(p.Input.Mean) != 0 && len(p.Input.Mean) != 3 || len(p.Input.Std) != 0 && len(p.Input.Std) != 3 || len(p.Input.Mean) != len(p.Input.Std) {
		return errors.New("normalization contract invalid")
	}
	for _, v := range append(append([]float64{}, p.Input.Mean...), p.Input.Std...) {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e6 {
			return errors.New("normalization value invalid")
		}
	}
	for _, v := range p.Input.Std {
		if v == 0 {
			return errors.New("normalization standard deviation must be nonzero")
		}
	}
	if len(p.Output.TensorNames) == 0 || len(p.Output.TensorNames) != len(p.Output.ExpectedShapes) || !member([]string{"float32", "uint8", "int8"}, p.Output.DType) || p.Output.SemanticOutputKind != "aggregate_features" {
		return errors.New("output contract invalid")
	}
	for _, s := range p.Output.ExpectedShapes {
		if !shapeValid(s) {
			return errors.New("output shape invalid")
		}
	}
	if p.LabelContract == "" || p.NormalizationContract == "" || p.Provenance == "" || p.LicenseReference == "" {
		return errors.New("provenance/semantic contract missing")
	}
	for _, s := range []string{p.LabelContract, p.NormalizationContract, p.Provenance, p.LicenseReference} {
		if strings.ContainsAny(s, "/\\") || strings.Contains(s, "..") || strings.Contains(s, "http:") || strings.Contains(s, "https:") {
			return errors.New("unsafe package metadata")
		}
	}
	if p.QualificationState != "prepared" && p.QualificationState != "qualified" {
		return errors.New("qualification state invalid")
	}
	if p.CompatibilityPolicy != "reject_incompatible" {
		return errors.New("compatibility policy invalid")
	}
	if a.TestOnly && (p.QualificationState == "qualified") {
		return errors.New("test adapter cannot be qualified")
	}
	return nil
}

type PreflightResult struct {
	RequestedState       string `json:"requested_state"`
	PackageID            string `json:"package_id"`
	ModuleID             string `json:"module_id"`
	ArtifactFormat       string `json:"artifact_format"`
	CompatibilityStatus  string `json:"compatibility_status"`
	QualificationStatus  string `json:"qualification_status"`
	ArtifactStatus       string `json:"artifact_status"`
	ErrorCategory        string `json:"error_category,omitempty"`
	ArtifactHashVerified bool   `json:"artifact_hash_verified"`
	ObservedAdapterState string `json:"observed_adapter_state"`
	EffectiveState       string `json:"effective_state"`
}

// Preflight validates an optional artifact under a trusted root, without opening
// a model runtime. Qualified status is never inferred from the manifest alone.
func Preflight(p Package, root string, arch, runtimeVersion string, allowTest bool, proofHashes map[string]bool) PreflightResult {
	r := PreflightResult{PackageID: p.PackageID, ModuleID: p.ModuleID, ArtifactFormat: p.ArtifactFormat, CompatibilityStatus: "compatible", QualificationStatus: "prepared", ArtifactStatus: "absent", EffectiveState: "unavailable", ObservedAdapterState: "unavailable"}
	if err := p.Validate(DefaultRegistry()); err != nil {
		r.CompatibilityStatus = "blocked"
		r.ErrorCategory = "package_invalid"
		return r
	}
	a := DefaultRegistry().adapters[p.RuntimeAdapterID]
	if a.TestOnly && !allowTest {
		r.CompatibilityStatus = "blocked"
		r.ErrorCategory = "test_adapter_forbidden"
		return r
	}
	if a.TestOnly && allowTest {
		if p.RuntimeAdapterID == "mock" {
			r.ObservedAdapterState = "available_test_only"
			r.EffectiveState = "simulated_test"
		} else {
			r.ObservedAdapterState = "not_implemented"
			r.ErrorCategory = "test_adapter_unavailable"
		}
	} else if p.RuntimeAdapterID == "rk3588-rknn" || p.RuntimeAdapterID == "onnx-cpu" {
		r.ObservedAdapterState = "not_implemented"
		r.ErrorCategory = "runtime_adapter_unavailable"
	}
	if !member(p.TargetArchitectures, arch) && !member(p.TargetArchitectures, "any") {
		r.CompatibilityStatus = "blocked"
		r.ErrorCategory = "architecture_incompatible"
		return r
	}
	min := p.MinimumRuntimeVersions[arch]
	if min == "" {
		min = p.MinimumRuntimeVersions["any"]
	}
	if !versionAtLeast(runtimeVersion, min) {
		r.CompatibilityStatus = "blocked"
		r.ErrorCategory = "runtime_incompatible"
		return r
	}
	if p.QualificationState == "qualified" && !proofHashes[strings.ToLower(p.ArtifactSHA256)] {
		r.QualificationStatus = "blocked"
		r.ErrorCategory = "qualification_proof_missing"
		r.EffectiveState = "blocked"
	}
	if root == "" {
		if p.QualificationState == "qualified" {
			r.QualificationStatus = "blocked"
			r.EffectiveState = "blocked"
			if r.ErrorCategory == "" {
				r.ErrorCategory = "artifact_missing"
			}
		}
		if a.TestOnly && allowTest {
			r.ArtifactStatus = "not_required"
			return r
		}
		if r.ErrorCategory == "" {
			r.ErrorCategory = "artifact_root_unconfigured"
		}
		return r
	}
	base, err := filepath.Abs(root)
	if err != nil {
		r.CompatibilityStatus = "blocked"
		r.ErrorCategory = "artifact_root_invalid"
		return r
	}
	rootInfo, err := os.Lstat(base)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		r.CompatibilityStatus = "blocked"
		r.ErrorCategory = "artifact_root_invalid"
		return r
	}
	path := filepath.Join(base, p.ArtifactFilename)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return r
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > 2<<30 {
		r.CompatibilityStatus = "blocked"
		r.ErrorCategory = "artifact_invalid"
		return r
	}
	f, err := os.Open(path)
	if err != nil {
		r.CompatibilityStatus = "blocked"
		r.ErrorCategory = "artifact_unreadable"
		return r
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, io.LimitReader(f, 2<<30+1)); err != nil {
		r.CompatibilityStatus = "blocked"
		r.ErrorCategory = "artifact_unreadable"
		return r
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(p.ArtifactSHA256) {
		r.CompatibilityStatus = "blocked"
		r.ErrorCategory = "artifact_hash_mismatch"
		return r
	}
	r.ArtifactStatus = "verified"
	r.ArtifactHashVerified = true
	if p.QualificationState == "qualified" && proofHashes[strings.ToLower(p.ArtifactSHA256)] {
		r.QualificationStatus = "qualified"
	}
	if !a.TestOnly && r.ObservedAdapterState != "not_implemented" {
		r.EffectiveState = "prepared"
	}
	return r
}

func shapeValid(s []int) bool {
	if len(s) == 0 || len(s) > 8 {
		return false
	}
	product := int64(1)
	for _, v := range s {
		if v < 1 || v > 100000 {
			return false
		}
		product *= int64(v)
		if product > 200000000 {
			return false
		}
	}
	return true
}
func member(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}
func version(s string) bool {
	var a, b, c int
	n, e := fmt.Sscanf(s, "%d.%d.%d", &a, &b, &c)
	return e == nil && n == 3 && a >= 0 && b >= 0 && c >= 0
}
func versionAtLeast(got, want string) bool {
	if want == "" {
		return true
	}
	var a, b, c, x, y, z int
	if _, e := fmt.Sscanf(got, "%d.%d.%d", &a, &b, &c); e != nil {
		return false
	}
	if _, e := fmt.Sscanf(want, "%d.%d.%d", &x, &y, &z); e != nil {
		return false
	}
	if a != x {
		return a > x
	}
	if b != y {
		return b > y
	}
	return c >= z
}
