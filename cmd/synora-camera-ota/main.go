package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"synora/internal/cameraota"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "explain":
		fmt.Println("camera OTA verifies a signed Zero 3W manifest, queues offline cameras, validates after reboot, then marks good; interrupted phases request rollback.")
	case "version":
		fmt.Printf("camera-ota manifest_schema=%d\n", cameraota.ManifestSchemaVersion)
	case "doctor":
		set := flag.NewFlagSet("doctor", flag.ExitOnError)
		root := set.String("root", "/var/lib/synora/camera-ota", "camera OTA state root")
		_ = set.Parse(os.Args[2:])
		info, err := os.Stat(*root)
		if err != nil || !info.IsDir() {
			fmt.Fprintln(os.Stderr, "camera OTA doctor: state root unavailable")
			os.Exit(1)
		}
		devices, err := os.ReadDir(filepath.Join(*root, "devices"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "camera OTA doctor: device journal unavailable")
			os.Exit(1)
		}
		fmt.Printf("camera OTA doctor: ok (%d journal(s))\n", len(devices))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() { fmt.Fprintln(os.Stderr, "usage: synora-camera-ota doctor|version|explain") }
