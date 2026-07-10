package topology

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synora/pkg/contract"
)

func TestResidentCRUDDefaultsAndExplicitIdentityPatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "residents.yaml")
	if err := os.WriteFile(path, []byte("residents: []\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	residents := map[string]*Resident{}
	created, err := CreateResident(path, residents, Resident{
		ID: "guest_1", Name: "Guest", Role: contract.ResidentRoleGuest,
		IdentityProfile: contract.IdentityProfile{FaceIDs: []string{"face-1"}, Aliases: []string{"Guest"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created.Enabled || created.Trusted {
		t.Fatalf("guest defaults=%#v", created)
	}
	name := "Guest updated"
	contact := contract.Contact{Email: "guest@example.test"}
	updated, err := PatchResident(path, residents, "guest_1", contract.ResidentPatch{Name: &name, Contact: &contact})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != name || updated.Contact.Email == "" || len(updated.IdentityProfile.FaceIDs) != 1 {
		t.Fatalf("patch erased an omitted identity profile: %#v", updated)
	}
	emptyIdentity := contract.IdentityProfile{}
	updated, err = PatchResident(path, residents, "guest_1", contract.ResidentPatch{IdentityProfile: &emptyIdentity})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.IdentityProfile.FaceIDs) != 0 {
		t.Fatalf("explicit identity clear was ignored: %#v", updated.IdentityProfile)
	}
	deleted, err := SoftDeleteResident(path, residents, "guest_1")
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Enabled || deleted.DeletedAt == nil {
		t.Fatalf("soft delete=%#v", deleted)
	}
	if _, err := CreateResident(path, residents, Resident{ID: "guest_1", Name: "Duplicate"}); contract.APIErrorCode(err) != contract.ErrorDuplicateID {
		t.Fatalf("duplicate error=%v", err)
	}
}

func TestResidentLegacyYAMLPreservesUnknownFieldsAndPublicViewIsCompact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "residents.yaml")
	initial := `residents:
  - id: alexis
    name: Alexis
    role: resident
    contact: {phone: "+33123456789"}
    identity_profile: {face_ids: [face-private]}
    vendor_extension: {secret: keep-in-file}
`
	if err := os.WriteFile(path, []byte(initial), 0o640); err != nil {
		t.Fatal(err)
	}
	residents, err := LoadResidents(path)
	if err != nil {
		t.Fatal(err)
	}
	resident := residents["alexis"]
	if resident == nil || !resident.Enabled || !resident.Trusted {
		t.Fatalf("legacy defaults=%#v", resident)
	}
	public := resident.PublicView()
	if public.ID != "alexis" {
		t.Fatalf("public=%#v", public)
	}
	metadata := map[string]any{"label": "home"}
	if _, err := PatchResident(path, residents, "alexis", contract.ResidentPatch{Metadata: &metadata}); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "vendor_extension:") || !strings.Contains(string(written), "keep-in-file") {
		t.Fatalf("unknown resident config was lost:\n%s", written)
	}
}

func TestResidentWriteFailureLeavesMapUntouched(t *testing.T) {
	residents := map[string]*Resident{}
	if _, err := CreateResident(t.TempDir(), residents, Resident{ID: "owner", Name: "Owner", Role: contract.ResidentRoleOwner}); err == nil {
		t.Fatal("expected persistence failure")
	}
	if len(residents) != 0 {
		t.Fatalf("failed write changed residents: %#v", residents)
	}
}
