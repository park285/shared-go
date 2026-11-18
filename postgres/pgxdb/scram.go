package pgxdb

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/xdg-go/stringprep"
	"golang.org/x/text/secure/precis"
)

var postgresSASLprep = stringprep.Profile{
	Mappings: []stringprep.Mapping{
		stringprep.SASLprep.Mappings[1],
		stringprep.TableB1,
	},
	Prohibits: stringprep.SASLprep.Prohibits,
	CheckBiDi: true,
}

func SCRAMVerifier(password string) (string, error) {
	prepared, err := compatibleSCRAMPassword(password)
	if err != nil {
		return "", err
	}

	var salt [16]byte

	rand.Read(salt[:])

	const iterations = 4096

	salted, err := pbkdf2.Key(sha256.New, prepared, salt[:], iterations, sha256.Size)
	if err != nil {
		return "", errors.New("pgxdb: derive SCRAM key failed")
	}

	mac := hmac.New(sha256.New, salted)

	_, _ = mac.Write([]byte("Client Key"))

	var clientKey [sha256.Size]byte

	storedKey := sha256.Sum256(mac.Sum(clientKey[:0]))
	mac.Reset()

	_, _ = mac.Write([]byte("Server Key"))

	var serverKey [sha256.Size]byte

	mac.Sum(serverKey[:0])

	encode := base64.StdEncoding.EncodeToString

	return fmt.Sprintf("SCRAM-SHA-256$%d:%s$%s:%s", iterations,
		encode(salt[:]), encode(storedKey[:]), encode(serverKey[:])), nil
}

func compatibleSCRAMPassword(password string) (string, error) {
	if password == "" || strings.IndexByte(password, 0) >= 0 || !utf8.ValidString(password) {
		return "", errors.New("pgxdb: password is not compatible with PostgreSQL clients")
	}

	postgresPassword := postgresSCRAMPassword(password)

	pgxPassword := pgxSCRAMPassword(password)

	if postgresPassword != pgxPassword {
		return "", errors.New("pgxdb: password is not compatible with PostgreSQL clients")
	}

	return postgresPassword, nil
}

func postgresSCRAMPassword(password string) string {
	ascii := true

	for i := range len(password) {
		if password[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}

	if ascii {
		return password
	}

	mapped, err := postgresSASLprep.Prepare(password)
	if err != nil || mapped == "" {
		return password
	}

	return postgresNFKC(mapped)
}

func pgxSCRAMPassword(password string) string {
	for _, r := range password {
		if stringprep.TableA1.Contains(r) {
			return password
		}
	}

	normalized, err := precis.OpaqueString.String(password)
	if err != nil {
		return password
	}

	return normalized
}
