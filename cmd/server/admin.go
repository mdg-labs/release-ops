package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/store"
)

const adminUsage = "usage: release-ops admin login-link"

// runAdminCommand is `release-ops admin <command>`, the operator's tools from the container's
// shell. Shell access to the container already implies admin, so there is nothing to
// authenticate: the only command is login-link, the break-glass sign-in (specs.html § 3.6).
func runAdminCommand(ctx context.Context, args []string, out io.Writer) error {
	if len(args) != 1 || args[0] != "login-link" {
		return errors.New(adminUsage)
	}
	cfg, err := config.LoadServer()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return printLoginLink(ctx, cfg, out)
}

// printLoginLink opens the app database the running server owns, stores the hash of a new
// one-time token for the bootstrap admin and prints the link containing the token once. It
// refuses, printing nothing, while no bootstrap admin has signed in.
func printLoginLink(ctx context.Context, cfg *config.Config, out io.Writer) error {
	db, err := store.OpenPath(cfg.AppDBPath)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer func() { _ = db.Close() }()

	cipher, err := crypto.NewCipherFromHex(cfg.AppEncryptionKey)
	if err != nil {
		return fmt.Errorf("encryption: %w", err)
	}

	link, err := auth.CreateLoginLink(ctx, store.New(db, cipher).SignIn(), cfg.AppURL, time.Now())
	if errors.Is(err, store.ErrNoBootstrapAdmin) {
		return errors.New("no bootstrap admin yet: sign in with GitHub once as the first admin, then run this again")
	}
	if err != nil {
		return fmt.Errorf("login link: %w", err)
	}
	_, err = fmt.Fprintf(out, "One-time sign-in link for the bootstrap admin, valid for %d minutes:\n%s\n", int(auth.LoginLinkTTL.Minutes()), link)
	return err
}
