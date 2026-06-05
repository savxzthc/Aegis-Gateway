package auth

import (
	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

func HashPassword(password string) (hash, salt string, err error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", "", err
	}
	return string(b), "", nil
}

func VerifyPassword(password, hash, salt string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
