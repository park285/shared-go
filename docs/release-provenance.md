# 릴리스 출처 검증

GitHub Actions OIDC와 Sigstore keyless 인증서가 서명 주체다. CI에 워크스테이션 키나
장기 조직 키를 복사하지 않으며, 기존 태그·릴리스를 이동하거나 재생성·소급 서명하지 않는다.

## 모듈과 태그

`.github/workflows/release.yml`과 기존 prepare/finalize/verify/publish 스크립트는 다음
조합만 허용한다. 버전은 선행 0이 없는 안정판 `MAJOR.MINOR.PATCH`이며 prerelease와
build metadata는 받지 않는다. 모듈 디렉터리·태그/ref·산출물 이름은 서로 구분한다.

| 모듈 디렉터리 | 모듈 경로 | 허용 태그 | 첫 전환 버전의 산출물 접두사 |
|---|---|---|---|
| `.` | `github.com/park285/shared-go/v2` | `v2.*.*` | `shared-go-root-v2.24.0` |
| `core` | `github.com/park285/shared-go/core` | `core/v1.*.*` | `shared-go-core-v1.0.0` |
| `postgres` | `github.com/park285/shared-go/postgres` | `postgres/v1.*.*` | `shared-go-postgres-v1.0.0` |
| `transport` | `github.com/park285/shared-go/transport` | `transport/v1.*.*` | `shared-go-transport-v1.0.0` |
| `openai` | `github.com/park285/shared-go/openai` | `openai/v1.*.*` | `shared-go-openai-v1.0.0` |

최초 전환은 provider-free 루트 `v2.24.0`을 먼저 게시하고 이를 요구하는 어댑터의
`v1.0.0`을 게시한 뒤 소비자의 실제 pin을 전환한다. core는 루트 의존성이 없다.
이 문서와 코드 변경은 태그 생성·원격 게시 승인이 아니다.

## CI와 로컬 검증

PR·main CI는 checkout의 다섯 모듈로 일회용 workspace를 만들고 각 모듈의 source
tidy drift·lint·race를 실행한다. main·주간·수동 보안 검사도 같은 source workspace에서
모듈별 govulncheck를 실행한다. 미게시 루트를 요구하는 최초 전환 PR도 검사할 수 있다.
`go-tooling.sh source-workspace <새 go.work 경로>`는 각 go.mod가 실제 요구하는 sibling 버전만
읽어 version-specific 로컬 replace를 생성합니다. 최초 버전을 workflow에 복제하지 않으며
기존 파일을 덮어쓰지 않습니다. 생성된 workspace는 개발·CI 소스 검사에만 사용합니다.
Unicode 생성 테이블과 결정적 할당 상한은 루트에서 검사한다. SQL ownership은 각 모듈에 적용한다.
postgres의 실제 DB 검사는 기존 `pgstore-release-gate.sh`가 소유한다.
pgstore용 일회용 PostgreSQL과 pgxdb 자체 일회용 서버를 사용하며, 두 패키지의
필수 사례 pass와 전체 skip=0을 요구한다. Podman/DB 준비 실패는 통과가 아니다.

```bash
# 체크아웃 안에 생성한 소스 workspace로 모든 모듈의 소스 통합을 검사한다.
work_dir=$(mktemp -d "$PWD/.ci-workspace.XXXXXXXX")
bash scripts/ci/go-tooling.sh source-workspace "$work_dir/go.work"
LINT_GOWORK="$work_dir/go.work" make lint
make test-race vulncheck SOURCE_GOWORK="$work_dir/go.work"
make source-tidy-check
bash scripts/check-sql-ownership.sh
bash scripts/ci/pgstore-release-gate.sh --gowork "$work_dir/go.work"
rm -rf -- "$work_dir"

# 게시 가능한 의존성만으로 모든 모듈을 검사한다.
bash scripts/ci/release-gate.sh

# 최초 루트 게시 전에는 루트만 독립적으로 검사할 수 있다.
bash scripts/ci/release-gate.sh .
```

`make`의 모듈 대상은 기본적으로 `. core postgres transport openai`다.
`MODULES=postgres`처럼 좁힐 수 있다. `make source-tidy-check`는 기존 go-tooling
스크립트에서 임시 modfile에만 sibling replace를 넣어 tidy한 뒤, 삽입한 replace만
제거하고 canonical go.mod와 비교한다. go.sum도 비교하되 그 로컬 sibling의 게시
checksum만 제외한다. 원본 go.mod/go.sum에는 쓰지 않는다.
이 검사는 checkout 소스의 통합 증거이지 게시된 pin이나 checksum의 증거가 아니다.

`make tidy-check`는 실제 `go mod tidy -diff`를 각 모듈에서 실행한다.
release gate는 항상 `GOWORK=off`이며 임시 replace를 사용하지 않는다.
release workflow는 태그에 해당하는 모듈만 검사하여 최초 루트 릴리스가 미게시
어댑터에 의존하지 않게 한다. 루트 `v2.24.0` 또는 새 모듈이 아직 게시되지 않았다면
어댑터·소비자의 실제 pin 검증은 미완료다. 개발 workspace나 source tidy 성공으로
이를 통과 처리하지 않는다.

필요 도구는 Go 1.27.4, 저장소가 고정한 Python 3.15.2와 uv 부트스트랩,
golangci-lint/govulncheck의 기존 버전 관리 스크립트, Git, GNU tar/gzip/coreutils,
PostgreSQL용 Podman·jq다. 게시 검증에는 GitHub CLI와 OIDC/attestation 권한,
GitHub의 immutable release 설정도 필요하다. SBOM은 기존 Syft v1.57.0을 사용한다.
도구·의존성 버전을 이 전환 때문에 올리지 않는다.

## 번들과 서명

각 번들은 결정적 소스 archive, SPDX JSON SBOM, schemaVersion 2 identity manifest,
`SHA256SUMS`로 구성된다. 루트 archive는 네 하위 모듈을 제외한다. 하위 모듈
archive는 해당 모듈 디렉터리와 저장소의 `LICENSE`·`THIRD_PARTY_NOTICES.md`만
포함한다. 선택한 모듈의 `go.mod`·`go.sum`과 라이선스 메타데이터가 없으면 거부한다.
SBOM은 checkout 전체가 아니라 이 archive를 풀어 놓은 디렉터리만 스캔하므로 다른
모듈의 의존성 목록이 섞이지 않는다. 포함된 라이선스/NOTICE는 공통 배포 메타데이터다.

manifest는 모듈 경로·디렉터리·버전·안전한 산출물 이름, 정확한 tag ref·commit,
서명 workflow와 `https://token.actions.githubusercontent.com` issuer를 기록한다.
prepare와 verify는 HEAD·tag target·GitHub ref/SHA 일치와 `origin/main` 포함 여부를
검사한다. verify는 정확한 파일 집합·체크섬·manifest·SPDX 문서 형식과 commit에서
재생성한 모듈 archive의 바이트 일치를 검사한다.

publish job만 `contents: write`, `id-token: write`, `attestations: write`를 가진다.
모든 asset의 provenance와 archive에 결합된 SPDX attestation을 생성하며,
publish 스크립트가 repository·workflow·OIDC issuer·tag ref·commit·GitHub-hosted
runner를 검증한 뒤 게시한다. 기존 릴리스나 조회 실패는 재게시 허용으로 바꾸지 않는다.
게시 응답 뒤 GitHub가 자동 생성하는 release attestation의 조회가 HTTP 404나 빈 배열이면
정확한 tag object의 `predicate_type=release` 조회만 5초 간격·120초 기한으로 기다린다.
그 밖의 조회 오류나 잘못된 응답은 즉시 실패하며, 기한 안에 증거가 없으면 게시됨·검증 불가로 실패한다.
발견 뒤 `gh release verify`의 암호학적 검증은 한 번만 실행한다. 서명 실패를
재시도로 숨기거나 릴리스를 재게시하지 않는다.
하위 모듈은 `--latest=false`로 게시하여 repository의 Latest를 루트 릴리스로 유지한다.

## 다운로드 검증

아래 값은 검증할 실제 태그와 산출물에 맞춰 지정한다. 로컬 태그는 신뢰한 원격에서
확보해야 하며 checksum 일치만으로 수용하지 않는다.

```bash
tag=postgres/v1.0.0
artifact=shared-go-postgres-v1.0.0
repo=park285/shared-go
commit="$(git rev-parse --verify "refs/tags/${tag}^{commit}")"
gh release download "$tag" --repo "$repo" --dir release
(
  cd release
  sha256sum --check --strict SHA256SUMS
)
for file in release/*; do
  gh attestation verify "$file" \
    --repo "$repo" \
    --signer-workflow "$repo/.github/workflows/release.yml" \
    --cert-oidc-issuer https://token.actions.githubusercontent.com \
    --source-ref "refs/tags/${tag}" \
    --source-digest "$commit" \
    --deny-self-hosted-runners
done
gh attestation verify "release/${artifact}.tar.gz" \
  --repo "$repo" \
  --signer-workflow "$repo/.github/workflows/release.yml" \
  --cert-oidc-issuer https://token.actions.githubusercontent.com \
  --source-ref "refs/tags/${tag}" \
  --source-digest "$commit" \
  --predicate-type https://spdx.dev/Document/v2.3 \
  --deny-self-hosted-runners
gh release verify "$tag" --repo "$repo"
```

소비자 pin의 게시 검증 완료는 immutable release와 위 암호학적 검증의 성공 이후에만
선언한다. 로컬 소스 통합이나 번들 생성은 원격 게시·서명 성공의 증거가 아니다.
