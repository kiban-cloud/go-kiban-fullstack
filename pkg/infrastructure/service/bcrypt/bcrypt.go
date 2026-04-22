package bcrypt

import (
	"errors"

	errors_domain "github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/errors"

	errorsWrapper "github.com/pkg/errors"
	"golang.org/x/crypto/bcrypt"
)

type BcryptPasswordHasher struct{}

func NewBcryptPasswordHasher() *BcryptPasswordHasher {
	return &BcryptPasswordHasher{}
}

func (b *BcryptPasswordHasher) Compare(hashedPassword, plainPassword string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(plainPassword))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return errors_domain.ErrMismatchedHashAndPassword
		}
		return errorsWrapper.Wrap(err, "error when trying to compare password")
	}
	return nil
}

func (b *BcryptPasswordHasher) Hash(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", errorsWrapper.Wrap(err, "error when trying to hash password")
	}
	return string(hashed), nil
}
