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

	err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(exitCode(err, os.Stderr))
}

// usageError marks a flag parsing failure the FlagSet has already reported.
type usageError struct{ error }

func (e usageError) Unwrap() error { return e.error }

// exitCode maps run's result to the process exit status: help requests exit 0,
// flag errors exit 2 (as flag.ExitOnError does), every other error exits 1.
// Errors the FlagSet already printed are not printed again.
func exitCode(err error, stderr io.Writer) int {
	var uerr usageError
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.As(err, &uerr):
		return 2
	default:
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("seed-admin", flag.ContinueOnError)
	fs.SetOutput(stderr)
	emailFlag := fs.String("email", "", "admin email address")
	passwordFlag := fs.String("password", "", "admin password")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageError{err}
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
