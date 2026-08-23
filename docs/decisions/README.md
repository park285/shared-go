# shared-go 결정 카탈로그

이 디렉터리는 `shared-go` 하나에만 해당하는 결정 레코드(`records/*.json`)와 생성 색인 `INDEX.md`, 이 저장소 `docs/` 제목에 대한 `inventory.allowlist`를 둡니다. 둘 이상의 저장소에 걸치는 결정은 iris-stack 메타 저장소의 `docs/agent-workflows/decisions/`에 있습니다.

스키마, 상태 값, 인벤토리 규칙, 명령은 iris-stack의 `docs/agent-workflows/decisions/README.md`가 정본입니다. 검증과 색인 생성은 iris-stack checkout에서 `bash tools/checks/check-decision-catalog.sh`(`render`, `check --submodules`, `list --repo shared-go`)로 수행하며, 이 저장소의 레코드는 `scope`가 `["shared-go"]`여야 하고 `sources`·`evidence`·`implementation`의 상대 경로는 이 저장소 루트 기준입니다. 다른 저장소의 파일은 `<repo>@<sha>:<path>` 형식으로 리비전을 고정해 가리킵니다.
