// Command migrate applies golang-migrate using the CGO-free sqlite driver (modernc).
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	log.SetFlags(0)

	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	migrationsPath := fs.String("path", "migrations", "migration files directory")
	databaseURL := fs.String("database", "", "database URL (sqlite://<path>)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		log.Fatal(err)
	}

	args := fs.Args()
	if *databaseURL == "" || len(args) < 1 {
		log.Fatal("usage: migrate -path <dir> -database sqlite://<path> up|down [N]")
	}

	sourceURL, err := fileSourceURL(*migrationsPath)
	if err != nil {
		log.Fatal(err)
	}

	m, err := migrate.New(sourceURL, *databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		sourceErr, dbErr := m.Close()
		if sourceErr != nil {
			log.Fatal(sourceErr)
		}
		if dbErr != nil {
			log.Fatal(dbErr)
		}
	}()

	switch args[0] {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatal(err)
		}
	case "down":
		steps := 1
		if len(args) > 1 {
			steps, err = strconv.Atoi(args[1])
			if err != nil || steps < 1 {
				log.Fatal("down requires a positive step count")
			}
		}
		if err := m.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unsupported command %q", args[0])
	}
}

func fileSourceURL(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve migrations path: %w", err)
	}
	return "file://" + filepath.ToSlash(abs), nil
}
