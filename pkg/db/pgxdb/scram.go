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

// PostgreSQL 18.6의 pg_saslprep은 C.1.2를 B.1보다 먼저 매핑한다.
// 금지 문자·미할당 문자·bidi 검사는 NFKC 출력이 아닌 매핑된 입력을 사용한다.
// Xdg 기본 SASLprep과 다르므로 같은 표를 사용하되 검사 대상을 맞춘다.
// https://github.com/postgres/postgres/blob/REL_18_6/src/common/saslprep.c
var postgresSASLprep = stringprep.Profile{
	Mappings: []stringprep.Mapping{
		stringprep.SASLprep.Mappings[1],
		stringprep.TableB1,
	},
	Prohibits: stringprep.SASLprep.Prohibits,
	CheckBiDi: true,
}

// SCRAMVerifier는 PostgreSQL 18 기본 정책(4096회, 새 128-bit salt)의
// SCRAM-SHA-256 verifier를 생성한다. 입력은 UTF-8 텍스트이며 DB 접속·역할 변경·로깅은 하지 않는다.
// PostgreSQL/libpq SASLprep과 pgx 5.11 PRECIS의 준비 실패 시 원문 사용까지
// 반영한 바이트가 다르거나 빈 값·NUL·잘못된 UTF-8이면 안전한 오류로 거부한다.
// 호출자는 성공한 뒤에만 역할을 변경하고 반환한 verifier도 자격증명으로 보호해야 한다.
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
	// 배포 자격은 UTF-8 텍스트다. libpq는 빈 값이나 NUL 뒤의 바이트를 사용할 수 없다.
	if password == "" || strings.IndexByte(password, 0) >= 0 || !utf8.ValidString(password) {
		return "", errors.New("pgxdb: password is not compatible with PostgreSQL clients")
	}

	postgresPassword := postgresSCRAMPassword(password)

	pgxPassword, err := precis.OpaqueString.String(password)
	if err != nil {
		pgxPassword = password
	}

	if postgresPassword != pgxPassword {
		return "", errors.New("pgxdb: password is not compatible with PostgreSQL clients")
	}

	return postgresPassword, nil
}

func postgresSCRAMPassword(password string) string {
	// PostgreSQL의 ASCII fast path는 제어 문자도 그대로 보존한다.
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

	// 호출자가 UTF-8을 검증했다. 금지 문자·빈 매핑 결과는 원문으로 인증한다.

	mapped, err := postgresSASLprep.Prepare(password)
	if err != nil || mapped == "" {
		return password
	}

	return postgresNFKC(mapped)
}
