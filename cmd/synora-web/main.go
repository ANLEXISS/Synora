package main

import (
	"log"
	"os/exec"
	"time"
)

func startNginx() *exec.Cmd {

	cmd := exec.Command("nginx", "-g", "daemon off;")

	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()

	err := cmd.Start()
	if err != nil {
		log.Fatal("failed to start nginx:", err)
	}

	log.Println("nginx started")

	return cmd
}

func main() {

	for {

		cmd := startNginx()

		err := cmd.Wait()

		log.Println("nginx stopped:", err)

		time.Sleep(2 * time.Second)

		log.Println("restarting nginx")

	}

}
