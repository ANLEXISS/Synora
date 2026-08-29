package bus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"synora/pkg/contract"
)

func (s *Server) authorizeMessage(msg contract.Message, service string) error {
	if err := authorizeACL(msg, service, s.allowedServices); err != nil {
		return err
	}
	now := s.now().UTC()
	if err := verifyMessageAuthentication(msg, s.auth, now, s.replayWindow); err != nil {
		return err
	}
	if isPrivilegedMessage(msg) {
		if msg.ID == "" || msg.Timestamp.IsZero() {
			return errors.New("privileged message requires id and timestamp")
		}
		age := now.Sub(msg.Timestamp.UTC())
		if age > s.replayWindow || age < -s.replayWindow {
			return errors.New("privileged message timestamp expired")
		}
		if err := s.rememberMessage(service, msg, now); err != nil {
			return err
		}
	}
	if s.auth.enabled() {
		if err := s.rememberNonce(msg, now); err != nil {
			return err
		}
	}
	return nil
}

func authorizeACL(msg contract.Message, service string, allowed map[string]struct{}) error {
	if msg.Source != service {
		return fmt.Errorf("source mismatch: %s != %s", msg.Source, service)
	}
	if msg.Target != "" {
		if _, ok := allowed[msg.Target]; !ok {
			return fmt.Errorf("target not allowlisted: %s", msg.Target)
		}
	}

	switch msg.Kind {
	case contract.KindCommand:
		if service == "core" && msg.Type == contract.EventActionRequest && msg.Target == "actions" {
			return nil
		}
	case contract.KindRPC:
		switch service {
		case "api":
			if msg.Target == "core" || (msg.Type == "connectivity.status" && msg.Target == "connectivity") {
				return nil
			}
		case "discovery":
			if msg.Target == "core" && (strings.HasPrefix(msg.Type, "face_dataset.") || strings.HasPrefix(msg.Type, "residents.photos.") || msg.Type == "clips.list") {
				return nil
			}
		case "core":
			if msg.Target == "runtime-manager" {
				return nil
			}
		case "runtime-manager", "connectivity":
			if msg.Target == "api" || msg.Target == "core" {
				return nil
			}
		}
	case contract.KindEvent:
		if eventACLAllowed(service, msg.Type, msg.Target) {
			return nil
		}
	}
	return fmt.Errorf("message not authorized for %s: %s -> %s (%s)", service, msg.Type, msg.Target, msg.Kind)
}

func eventACLAllowed(service, eventType, target string) bool {
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	targetOK := func(targets ...string) bool {
		if target == "" {
			return true
		}
		for _, allowed := range targets {
			if target == allowed {
				return true
			}
		}
		return false
	}
	hasPrefix := func(prefixes ...string) bool {
		for _, prefix := range prefixes {
			if strings.HasPrefix(eventType, prefix) {
				return true
			}
		}
		return false
	}

	switch service {
	case "actions":
		return (eventType == contract.EventActionResult || eventType == contract.EventActionServiceStarted) && targetOK("", "core")
	case "api":
		return (hasPrefix("network.") || hasPrefix("vision.") || hasPrefix("discovery.")) && targetOK("", "core")
	case "discovery":
		return (hasPrefix("discovery.") || hasPrefix("clip.") || hasPrefix("residents.") || eventType == contract.EventDeviceOffline) && targetOK("", "core")
	case "vision":
		return (hasPrefix("vision.") || eventType == "delivery.ack") && target == "core"
	case "lab":
		return hasPrefix("vision.") && target == "core"
	case "core", "core-2":
		if eventType == "state.snapshot" {
			return targetOK("api")
		}
		return targetOK("", "api", "core", "core-2", "vision") && hasPrefix(
			"system.", "security.", "manual.", "incident.", "engine.", "cge.",
			"chain.", "clip.", "runtime.", "device.", "devices.", "residents.",
			"automations.", "topology.", "validation.", "action.")
	}
	return false
}

func isPrivilegedMessage(msg contract.Message) bool {
	if msg.Kind == contract.KindCommand || msg.Kind == contract.KindRPC {
		return true
	}
	if msg.Kind != contract.KindEvent {
		return false
	}
	typeName := strings.ToLower(strings.TrimSpace(msg.Type))
	return strings.HasPrefix(typeName, "action.") || strings.HasPrefix(typeName, "security.") ||
		typeName == contract.EventManualRisk || typeName == contract.EventSystemStateReset ||
		typeName == contract.EventAutomationAction
}

func (s *Server) rememberNonce(msg contract.Message, now time.Time) error {
	key := msg.AuthKeyID + "\x00" + msg.AuthNonce
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneReplayLocked(now)
	if _, exists := s.seenNonces[key]; exists {
		return errors.New("replayed bus nonce")
	}
	s.seenNonces[key] = now
	return nil
}

func (s *Server) rememberMessage(service string, msg contract.Message, now time.Time) error {
	key := service + "\x00" + msg.ID
	fingerprint := messageFingerprint(msg)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneReplayLocked(now)
	if previous, exists := s.seenMessages[key]; exists {
		if previous.fingerprint != fingerprint {
			return errors.New("message id reused with different payload")
		}
		return errors.New("replayed bus message")
	}
	s.seenMessages[key] = messageReplay{seenAt: now, fingerprint: fingerprint}
	return nil
}

func (s *Server) pruneReplayLocked(now time.Time) {
	cutoff := now.Add(-s.replayWindow)
	for key, seenAt := range s.seenNonces {
		if seenAt.Before(cutoff) {
			delete(s.seenNonces, key)
		}
	}
	for key, replay := range s.seenMessages {
		if replay.seenAt.Before(cutoff) {
			delete(s.seenMessages, key)
		}
	}
}

func messageFingerprint(msg contract.Message) string {
	unsigned := msg
	unsigned.AuthKeyID = ""
	unsigned.AuthNonce = ""
	unsigned.AuthSignature = ""
	data, err := json.Marshal(unsigned)
	if err != nil {
		return "marshal-error"
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
