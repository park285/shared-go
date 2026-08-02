package pgxdb

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

const sqlstateUniqueViolation = "23505"

// IsDuplicateKey는 SQLSTATE 23505(unique_violation)를 가진 *pgconn.PgError만 참으로 본다.
// 드라이버 타입을 잃은 문자열 판정은 지원하지 않는다: 사용자 입력이 그대로 들어간 에러 문구가
// 판정을 뒤집을 수 있어, 타입이 보존된 pgx 에러만 계약으로 삼는다.
func IsDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	pgErr, ok := errors.AsType[*pgconn.PgError](err)

	return ok && pgErr.Code == sqlstateUniqueViolation
}
