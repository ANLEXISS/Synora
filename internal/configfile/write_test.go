package configfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicWithBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.yaml")
	if err := os.WriteFile(path, []byte("old\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomicWithBackup(path, []byte("new\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new\n" {
		t.Fatalf("committed data=%q err=%v", data, err)
	}
	backups, err := filepath.Glob(filepath.Join(dir, "backups", "devices.*.yaml"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v err=%v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || string(backup) != "old\n" {
		t.Fatalf("backup data=%q err=%v", backup, err)
	}
}
