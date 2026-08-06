// Command seed-admin creates the first admin user interactively or via flags.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

func main() {
	log.SetFlags(0)

	emailFlag := flag.String("email", "", "admin email address")
	passwordFlag := flag.String("password", "", "admin password")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	email, password, err := auth.ResolveAdminCredentials(os.Stdin, os.Stdout, *emailFlag, *passwordFlag)
	if err != nil {
		log.Fatalf("credentials: %v", err)
	}

	db, err := store.OpenPath(cfg.AppDBPath)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("database close: %v", err)
		}
	}()

	queries := storedb.New(db)
	if err := auth.SeedAdminUser(context.Background(), queries, email, password); err != nil {
		if errors.Is(err, auth.ErrUsersExist) {
			log.Fatal("seed-admin: users already exist")
		}
		log.Fatalf("seed-admin: %v", err)
	}

	fmt.Println("Admin user created.")
}
