package connectivity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/curve25519"
	"synora/internal/configfile"
)

const (
	IdentityFile          = "device-identity.key"
	WireGuardKeyFile      = "wireguard.key"
	IdentityFormatVersion = 1
)

type Identity struct {
	deviceID    string
	fingerprint string
	edPublic    ed25519.PublicKey
	wireguard   WireGuardIdentity
}

type WireGuardIdentity struct {
	private [32]byte
	public  [32]byte
}

type identityDisk struct {
	Version    int    `json:"version"`
	PrivateKey string `json:"private_key"`
}

type wireGuardDisk struct {
	Version    int    `json:"version"`
	PrivateKey string `json:"private_key"`
}

func LoadOrGenerateIdentity(dir string) (Identity, error) {
	if err := prepareDirectory(dir); err != nil {
		return Identity{}, err
	}
	edPath := filepath.Join(dir, IdentityFile)
	edPrivate, err := loadOrGenerateEd25519(edPath)
	if err != nil {
		return Identity{}, err
	}
	wgPath := filepath.Join(dir, WireGuardKeyFile)
	wg, err := loadOrGenerateWireGuard(wgPath)
	if err != nil {
		return Identity{}, err
	}
	public := edPrivate.Public().(ed25519.PublicKey)
	digest := sha256.Sum256(public)
	deviceID := "central_" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:])
	return Identity{deviceID: strings.ToLower(deviceID), fingerprint: fmt.Sprintf("sha256:%x", digest[:]), edPublic: append(ed25519.PublicKey(nil), public...), wireguard: wg}, nil
}

func LoadExistingIdentity(dir string) (Identity, error) {
	edPrivate, err := loadEd25519(filepath.Join(dir, IdentityFile))
	if err != nil {
		return Identity{}, err
	}
	wg, err := loadWireGuard(filepath.Join(dir, WireGuardKeyFile))
	if err != nil {
		return Identity{}, err
	}
	public := edPrivate.Public().(ed25519.PublicKey)
	digest := sha256.Sum256(public)
	deviceID := "central_" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:])
	return Identity{deviceID: strings.ToLower(deviceID), fingerprint: fmt.Sprintf("sha256:%x", digest[:]), edPublic: append(ed25519.PublicKey(nil), public...), wireguard: wg}, nil
}

func (i Identity) DeviceID() string    { return i.deviceID }
func (i Identity) Fingerprint() string { return i.fingerprint }
func (i Identity) FingerprintShort() string {
	if len(i.fingerprint) <= 23 {
		return i.fingerprint
	}
	return i.fingerprint[:23]
}
func (i Identity) HasEd25519() bool   { return len(i.edPublic) == ed25519.PublicKeySize }
func (i Identity) HasWireGuard() bool { return i.wireguard.public != [32]byte{} }
func (i Identity) WireGuardPublicFingerprint() string {
	digest := sha256.Sum256(i.wireguard.public[:])
	return fmt.Sprintf("sha256:%x", digest[:])[:23]
}

func MaterialPresence(dir string) (ed25519Present, wireGuardPresent bool) {
	for path, target := range map[string]*bool{
		filepath.Join(dir, IdentityFile):     &ed25519Present,
		filepath.Join(dir, WireGuardKeyFile): &wireGuardPresent,
	} {
		info, err := os.Lstat(path)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm() == 0600 {
			*target = true
		}
	}
	return ed25519Present, wireGuardPresent
}

func prepareDirectory(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return errors.New("connectivity data directory is required")
	}
	if info, err := os.Lstat(dir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("connectivity data directory is not a directory")
		}
	} else if !os.IsNotExist(err) {
		return errors.New("inspect connectivity data directory")
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		return errors.New("create connectivity data directory")
	}
	return os.Chmod(dir, 0750)
}

func loadOrGenerateEd25519(path string) (ed25519.PrivateKey, error) {
	private, err := loadEd25519(path)
	if err == nil {
		return private, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	_, private, err = ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, errors.New("generate device identity")
	}
	disk, _ := json.Marshal(identityDisk{Version: IdentityFormatVersion, PrivateKey: base64.StdEncoding.EncodeToString(private)})
	disk = append(disk, '\n')
	if err := configfile.WriteAtomicNew(path, disk, 0600); err != nil {
		if errors.Is(err, os.ErrExist) {
			return loadEd25519(path)
		}
		return nil, errors.New("persist device identity")
	}
	return private, nil
}

func loadEd25519(path string) (ed25519.PrivateKey, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("device identity file is not regular")
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("device identity permissions are too broad")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("read device identity")
	}
	var disk identityDisk
	if err := json.Unmarshal(data, &disk); err != nil || disk.Version != IdentityFormatVersion {
		return nil, errors.New("invalid device identity format")
	}
	private, err := base64.StdEncoding.DecodeString(disk.PrivateKey)
	if err != nil || len(private) != ed25519.PrivateKeySize || bytes.Equal(private, make([]byte, ed25519.PrivateKeySize)) {
		return nil, errors.New("invalid device identity key")
	}
	derived := ed25519.NewKeyFromSeed(private[:ed25519.SeedSize])
	if !bytes.Equal(derived, private) {
		return nil, errors.New("invalid device identity key")
	}
	return append(ed25519.PrivateKey(nil), private...), nil
}

func loadOrGenerateWireGuard(path string) (WireGuardIdentity, error) {
	identity, err := loadWireGuard(path)
	if err == nil {
		return identity, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return WireGuardIdentity{}, err
	}
	var private [32]byte
	if _, err := rand.Read(private[:]); err != nil {
		return WireGuardIdentity{}, errors.New("generate WireGuard identity")
	}
	public, err := curve25519.X25519(private[:], curve25519.Basepoint)
	if err != nil {
		return WireGuardIdentity{}, errors.New("derive WireGuard public key")
	}
	var publicArray [32]byte
	copy(publicArray[:], public)
	disk, _ := json.Marshal(wireGuardDisk{Version: IdentityFormatVersion, PrivateKey: base64.StdEncoding.EncodeToString(private[:])})
	disk = append(disk, '\n')
	if err := configfile.WriteAtomicNew(path, disk, 0600); err != nil {
		if errors.Is(err, os.ErrExist) {
			return loadWireGuard(path)
		}
		return WireGuardIdentity{}, errors.New("persist WireGuard identity")
	}
	return WireGuardIdentity{private: private, public: publicArray}, nil
}

func loadWireGuard(path string) (WireGuardIdentity, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return WireGuardIdentity{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return WireGuardIdentity{}, errors.New("WireGuard key file is not regular")
	}
	if info.Mode().Perm()&0077 != 0 {
		return WireGuardIdentity{}, errors.New("WireGuard key permissions are too broad")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return WireGuardIdentity{}, errors.New("read WireGuard identity")
	}
	var disk wireGuardDisk
	if err := json.Unmarshal(data, &disk); err != nil || disk.Version != IdentityFormatVersion {
		return WireGuardIdentity{}, errors.New("invalid WireGuard key format")
	}
	private, err := base64.StdEncoding.DecodeString(disk.PrivateKey)
	if err != nil || len(private) != 32 || bytes.Equal(private, make([]byte, 32)) {
		return WireGuardIdentity{}, errors.New("invalid WireGuard private key")
	}
	public, err := curve25519.X25519(private, curve25519.Basepoint)
	if err != nil || len(public) != 32 {
		return WireGuardIdentity{}, errors.New("invalid WireGuard key material")
	}
	var privateArray, publicArray [32]byte
	copy(privateArray[:], private)
	copy(publicArray[:], public)
	return WireGuardIdentity{private: privateArray, public: publicArray}, nil
}
