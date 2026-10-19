// keystone-init generates the deployment secrets into the secrets volume.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/civr/keystone/internal/secrets"
)

const dir = "/run/keystone"

func main() {
	missing := map[string]bool{}
	for _, name := range secrets.Files {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			missing[name] = true
		}
	}
	if err := secrets.Ensure(dir); err != nil {
		log.Fatal(err)
	}
	for _, name := range secrets.Files {
		if missing[name] {
			log.Printf("created %s", name)
		} else {
			log.Printf("kept existing %s", name)
		}
	}
}
