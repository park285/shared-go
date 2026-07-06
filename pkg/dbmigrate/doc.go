// Package dbmigrate는 embed.FS의 manifest.txt 순서대로 SQL 파일을 실행합니다.
//
// # 패키지 개요
//
// 이 패키지는 bot별 migrations embed.FS와 database/sql 또는 pgx 실행 함수를
// 주입받아 manifest 순서를 공통으로 처리합니다. manifest 라인은 "NNN file.sql"
// 형식을 기대하며 빈 줄과 '#' 주석은 무시합니다.
//
// # 주요 사용 패턴
//
//	err := dbmigrate.Apply(ctx, migrations.FS, dbmigrate.SQLExec(db))
//	err = dbmigrate.Apply(ctx, migrations.FS, func(ctx context.Context, query string, args ...any) error {
//	    _, execErr := conn.Exec(ctx, query, args...)
//	    return execErr
//	}, dbmigrate.WithOnly("0001_repository_baseline.sql"))
package dbmigrate
