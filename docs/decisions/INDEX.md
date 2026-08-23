# shared-go 결정 카탈로그 색인

iris-stack의 `bash tools/checks/check-decision-catalog.sh render`가 생성하는 파일입니다. 직접 편집하지 말고 레코드를 고친 뒤 다시 생성하십시오. 규칙은 iris-stack의 `docs/agent-workflows/decisions/README.md`에 있고, 둘 이상의 저장소에 걸치는 결정은 그쪽 색인에 있습니다.

레코드 2건: proposed 0, accepted 1, rejected 0, withdrawn 0, superseded 1

| ID | 제목 | 결정 상태 | 이행 상태 | scope | 결정일 | 재검토 | 대체 관계 | 원본 |
|---|---|---|---|---|---|---|---|---|
| [DEC-20260612-shared-go-transport-idle-conns](records/DEC-20260612-shared-go-transport-idle-conns.json) | shared-go TransportProfile MaxIdleConns 기본값은 external 128 / internal 256 | accepted | implemented | shared-go | 2026-06-12 | - | - | [iris-stack: 28_open_decisions.md](../../../docs/performance_reliability_program_v2/28_open_decisions.md) |
| [DEC-20260610-shared-go-telemetry-retention](records/DEC-20260610-shared-go-telemetry-retention.json) | 소비자가 없는 shared-go/pkg/telemetry를 삭제할지 OTel 대비로 유지할지 | superseded | unknown | shared-go | 2026-06-10 | - | superseded by DEC-20260805-shared-go-telemetry-retained | [iris-stack: 2026-06-10-iris-stack-refactoring-roadmap.md](../../../docs/agent-workflows/plans/2026-06-10-iris-stack-refactoring-roadmap.md) |
