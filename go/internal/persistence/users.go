package persistence

import (
	"context"

	sqlcgen "github.com/termix/termix/go/gen/sqlc"
)

type User struct {
	ID           string
	Email        string
	DisplayName  string
	PasswordHash string
	Role         string
	Status       string
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	row, err := sqlcgen.New(s.Pool).GetUserByEmail(ctx, email)
	if err != nil {
		return User{}, err
	}
	return User{
		ID:           row.ID.String(),
		Email:        row.Email,
		DisplayName:  row.DisplayName,
		PasswordHash: row.PasswordHash,
		Role:         row.Role,
		Status:       row.Status,
	}, nil
}

func (s *Store) UpdateUserLastLogin(ctx context.Context, userID string) error {
	id, err := parseUUID(userID)
	if err != nil {
		return err
	}
	return s.queries.UpdateUserLastLogin(ctx, id)
}
