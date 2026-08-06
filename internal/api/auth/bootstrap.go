package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

var (
	// ErrInvalidEmail is returned when an email address fails validation.
	ErrInvalidEmail = errors.New("invalid email")
	// ErrUsersExist is returned when seeding is attempted but users already exist.
	ErrUsersExist = errors.New("users already exist")
)

// ValidateEmail checks that email is a bare, well-formed address.
func ValidateEmail(email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return fmt.Errorf("%w: empty", ErrInvalidEmail)
	}
	if strings.ContainsAny(email, " \t\r\n") {
		return fmt.Errorf("%w: contains whitespace", ErrInvalidEmail)
	}

	addr, err := mail.ParseAddress(email)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidEmail, err)
	}
	if addr.Address != email {
		return fmt.Errorf("%w: must be a bare address", ErrInvalidEmail)
	}

	parts := strings.Split(addr.Address, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || !strings.Contains(parts[1], ".") {
		return fmt.Errorf("%w: malformed address", ErrInvalidEmail)
	}
	return nil
}

// BootstrapFromEnv creates the first admin from env when the users table is empty.
// Skips without error when bootstrap credentials are unset or users already exist.
func BootstrapFromEnv(ctx context.Context, q *storedb.Queries, email, password string) error {
	if email == "" && password == "" {
		return nil
	}
	_, err := createAdminIfEmpty(ctx, q, email, password)
	return err
}

// SeedAdminUser creates the first admin (CLI). Returns ErrUsersExist when users are present.
func SeedAdminUser(ctx context.Context, q *storedb.Queries, email, password string) error {
	if strings.TrimSpace(email) == "" || password == "" {
		return fmt.Errorf("email and password are required")
	}
	created, err := createAdminIfEmpty(ctx, q, email, password)
	if err != nil {
		return err
	}
	if !created {
		return ErrUsersExist
	}
	return nil
}

func createAdminIfEmpty(ctx context.Context, q *storedb.Queries, email, password string) (bool, error) {
	count, err := q.CountUsers(ctx)
	if err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	if err := ValidateEmail(email); err != nil {
		return false, err
	}
	if password == "" {
		return false, fmt.Errorf("password is required")
	}

	hash, err := HashPassword(password)
	if err != nil {
		return false, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := q.CreateUser(ctx, storedb.CreateUserParams{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		return false, fmt.Errorf("create user: %w", err)
	}
	return true, nil
}

// ResolveAdminCredentials reads email/password from flags or interactive prompts.
func ResolveAdminCredentials(in io.Reader, out io.Writer, emailFlag, passwordFlag string) (string, string, error) {
	email := strings.TrimSpace(emailFlag)
	password := passwordFlag
	reader := bufio.NewReader(in)

	if email == "" {
		var err error
		email, err = promptLine(reader, out, "Email: ")
		if err != nil {
			return "", "", err
		}
	}
	if password == "" {
		var err error
		password, err = promptLine(reader, out, "Password: ")
		if err != nil {
			return "", "", err
		}
	}

	if email == "" || password == "" {
		return "", "", fmt.Errorf("email and password are required")
	}
	return email, password, nil
}

func promptLine(reader *bufio.Reader, out io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return "", err
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("unexpected EOF")
	}
	return strings.TrimSpace(line), nil
}
