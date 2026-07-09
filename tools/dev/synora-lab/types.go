package main

import (
	"synora/internal/simulation"
	"synora/pkg/contract"
)

const (
	defaultAPIURL     = "http://127.0.0.1:8080/api/state"
	defaultBusPath    = "/run/synora/bus.sock"
	defaultDevice     = "cam_01"
	defaultIdentity   = "alexis"
	defaultConfidence = 0.92
)

type Config struct {
	BusPath       string
	APIURL        string
	HealthURL     string
	Token         string
	DeviceID      string
	CameraID      string
	NodeID        string
	Identity      string
	Confidence    float64
	SendType      string
	Scenario      string
	Watch         bool
	NoTUI         bool
	ListScenarios bool
	DryRunActions bool

	identityExplicit bool
}

type SimCamera struct {
	ID             string
	CameraID       string
	DeviceID       string
	NodeID         string
	Online         bool
	CurrentTrackID string
	CurrentClipID  string
}

type EventOptions = simulation.EventBuildOptions
type ScenarioStep = simulation.ScenarioStep
type Scenario = simulation.Scenario

type EventSender interface {
	Send(contract.Message) error
}
