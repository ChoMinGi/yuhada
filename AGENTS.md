# YUHADA HAIR — 에이전트 작업 규칙

## 프로젝트 개요

누나가 운영 중인 미용실(유하다 헤어)의 정액권 관리 SaaS. **실 운영 중 (Cafe24 VPS)**.

- **스택**: Go 1.22+ / chi / templ / SQLite / sqlc / goose / Tailwind / HTMX + Alpine
- **DB**: `var/yuhada.db` (단일 SQLite 파일, 운영 데이터)
- **알림**: 알리고 LMS (충전/차감/가입)
- **암호화**: PII(이름·전화번호) AES-256-GCM
- **배포**: GitHub Actions → Cafe24 VPS (cross-compile Linux amd64)

## 작업 원칙

1. **운영 DB는 절대 직접 건드리지 마라.** 마이그레이션은 `migrations/` 디렉토리에 goose 파일로 추가하고, 로컬 `var/yuhada.db`에서 검증.
2. **templ 변경 시 `make templ` 필수.** `_templ.go`는 생성 산출물.
3. **Tailwind 클래스 변경 시 `make css` 필수.** `static/css/app.css`는 생성 산출물.
4. **베이지+세리프 럭셔리 톤 유지** — 색/폰트/마진 컨벤션은 기존 `internal/view/` 컴포넌트 참조.
5. **SMS 메시지 템플릿은 `internal/sms/aligo.go`에 집중**. 핸들러는 호출만.

## 디렉토리 컨벤션

```
cmd/server/              엔트리포인트
internal/
  config/                env 로드
  db/queries/            sqlc 입력 SQL
  db/dbgen/              sqlc 생성 산출물 (수동 편집 금지)
  service/               비즈니스 로직 (트랜잭션 경계)
  handler/               chi 라우터 + HTTP 핸들러
  view/page/             templ 페이지
  view/partial/          templ 부분 (modals 등)
  view/component/        templ 재사용 컴포넌트
  view/layout/           templ 레이아웃
  auth/, crypto/, sms/, util/   기타 패키지
migrations/              goose 마이그레이션 (운영 DB 스키마)
static/                  공개 정적 파일
tailwind/                Tailwind 빌드 입력
```

## 하네스: v2 런칭

**목표:** 운영 DB/v1 코드 무손상으로 v2(정액권 정책 도입)를 격리된 패키지·DB·URL에서 안전하게 런칭.

**트리거:** v2 런칭, v2 평가, v2 구축, v2 마이그레이션, 정액권 정책, `/v2` 라우터, `membership_plans`, `memberships` 관련 작업 요청 시 `v2-launch-orchestrator` 스킬을 사용하라. 단순 질문(스키마 설명, 디자인 의견)은 직접 응답 가능.

**변경 이력:**
| 날짜 | 변경 내용 | 대상 | 사유 |
|------|----------|------|------|
| 2026-06-28 | 초기 구성 | 전체 | v2 런칭 하네스 신규 구축 (plan-reviewer, db-architect, backend+ui builder 팀, safety-auditor, launch-coordinator) |
