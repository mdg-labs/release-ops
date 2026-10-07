// Command seed-admin creates the first admin user interactively or via flags.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

func main() {
	log.SetFlags(0)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		stop()
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("seed-admin", flag.ContinueOnError)
	emailFlag := fs.String("email", "", "admin email address")
	passwordFlag := fs.String("password", "", "admin password")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	email, password, err := auth.ResolveAdminCredentials(stdin, stdout, *emailFlag, *passwordFlag)
	if err != nil {
		return fmt.Errorf("credentials: %w", err)
	}

	applied, err := store.Migrate(ctx, cfg.AppDBPath)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	for _, m := range applied {
		log.Printf("migrations: applied %s_%s", m.Version, m.Slug)
	}

	db, err := store.OpenPath(cfg.AppDBPath)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			log.Printf("database close: %v", cerr)
		}
	}()

	queries := storedb.New(db)
	if err := auth.SeedAdminUser(ctx, queries, email, password); err != nil {
		if errors.Is(err, auth.ErrUsersExist) {
			return errors.New("seed-admin: users already exist")
		}
		return fmt.Errorf("seed-admin: %w", err)
	}

	_, err = fmt.Fprintln(stdout, "Admin user created.")
	return err
}
