# YUHADA HAIR — 정액권 관리 시스템

미용실 정액권(선불 잔액) 관리 웹앱.
관리자(태블릿)가 회원 등록/충전/차감하고, 고객(모바일)은 NFC 카드 태깅으로 잔액을 확인합니다.

## 스택

| 레이어 | 기술 |
|---|---|
| 서버 | Go + [chi](https://github.com/go-chi/chi) |
| 뷰 | [templ](https://templ.guide) + [HTMX](https://htmx.org) + [Alpine.js](https://alpinejs.dev) |
| 스타일 | Tailwind CSS v4 (standalone CLI), self-hosted 폰트 |
| DB | SQLite ([modernc](https://pkg.go.dev/modernc.org/sqlite), pure Go) + [sqlc](https://sqlc.dev) + [goose](https://github.com/pressly/goose) |
| 세션 | [scs](https://github.com/alexedwards/scs) / sqlite3store |
| SMS | [알리고](https://smartsms.aligo.in) LMS API |
| 배포 | Cafe24 VPS · systemd + nginx · GitHub Actions CD |

## 아키텍처

```
cmd/server/main.go          ← 엔트리포인트
internal/
├── auth/                    ← 세션 미들웨어 (RequireAdmin, RedirectIfAuthed)
├── config/                  ← .env 로딩
├── crypto/                  ← AES-256-GCM PII 암호화
├── db/
│   ├── db.go                ← SQLite 연결 (WAL, busy_timeout)
│   ├── queries/             ← SQL 쿼리 원본
│   └── dbgen/               ← sqlc 생성 코드
├── handler/                 ← HTTP 핸들러 (chi 라우터)
│   ├── router.go            ← 라우팅 + 미들웨어 체인
│   ├── dashboard.go         ← 대시보드 KPI
│   ├── members.go           ← 회원 CRUD + 메모
│   ├── wallet.go            ← 충전/차감 + SMS 알림
│   ├── cards.go             ← 카드 관리
│   ├── transactions.go      ← 거래 내역
│   ├── customer.go          ← 고객 모바일 (NFC 진입)
│   └── login.go             ← PIN 로그인
├── service/                 ← 비즈니스 로직
│   ├── member.go            ← 회원 (암호화 포함)
│   ├── wallet.go            ← 충전/차감 트랜잭션
│   ├── transaction.go       ← 거래 조회 + 필터
│   ├── stats.go             ← 대시보드 집계
│   └── admin.go             ← PIN 인증 + 부트스트랩
├── sms/                     ← 알리고 SMS 클라이언트
├── ui/                      ← Lucide SVG 아이콘 임베드
├── util/                    ← 포맷 (KRW, 전화번호), ID 생성
└── view/                    ← templ 뷰 레이어
    ├── layout/              ← Admin shell, Bare shell
    ├── component/           ← Wordmark, PageHeader, Stat, Icon
    ├── page/                ← 페이지 템플릿
    └── partial/             ← HTMX swap 전용 모달
migrations/                  ← goose SQL 마이그레이션
static/                      ← CSS, 폰트, 아이콘
tailwind/                    ← input.css + Tailwind CLI
```

## 주요 기능

**관리자 (태블릿)**
- PIN 6자리 로그인
- 회원 등록/검색/삭제
- 잔액 충전/차감 (트랜잭션 보장)
- NFC 카드 발급/분실 신고/재활성화
- 대시보드 KPI (기간별 충전/차감 합계)
- 거래 내역 조회 (기간/타입/회원 필터)

**고객 (모바일)**
- NFC 카드 태깅 → 잔액 조회 (로그인 없음)
- 잔액 부족/분실 카드 안내 화면

**알림**
- 충전/차감 시 고객에게 LMS 자동 발송
- 최초 충전 시 이용약관 안내 포함

**보안**
- 회원 PII (이름/전화/메모) AES-256-GCM 암호화
- 전화번호: 결정적 암호화 (UNIQUE 제약 유지)
- 이름/메모: 랜덤 nonce (더 강한 보안)
- bcrypt PIN 해싱
- debug 엔드포인트 프로덕션 자동 차단

## 기술 선택 이유

| 선택 | 이유 |
|---|---|
| **Go + SQLite** | 단일 바이너리 배포, VPS 1대로 충분한 소규모 매장. CGO 없이 크로스 컴파일 가능. |
| **templ + HTMX** | SPA 프레임워크 없이 서버 렌더링 + 부분 갱신. JS 번들 0KB. |
| **Alpine.js** | 모달/드로어 등 클라이언트 상태만 최소한으로. |
| **sqlc** | SQL을 직접 작성하되 타입 안전한 Go 코드 자동 생성. ORM 없이 투명한 쿼리. |
| **AES-256-GCM** | 고객 개인정보 DB 유출 시 평문 노출 방지. `enc:` 접두어로 하위호환 마이그레이션. |
| **SMS (알리고)** | 국내 문자 API. 핸들러에서 goroutine 비동기 발송, 실패해도 트랜잭션 무관. |

## 로컬 실행

```bash
make tools          # templ, sqlc, goose, tailwind CLI 설치
cp .env.example .env
make migrate-up     # DB 마이그레이션
make seed           # 시드 데이터
make dev            # templ generate + CSS build + go run
```

`http://localhost:8080` 으로 접속. PIN은 `.env`의 `ADMIN_BOOTSTRAP_PIN` 참고.

## 테스트

```bash
go test ./...
```

| 패키지 | 테스트 범위 |
|---|---|
| `crypto` | 암복호화 라운드트립, 결정적 암호화, nil 핸들링, 하위호환 |
| `util` | KRW 포맷, 전화번호 정규화/포맷, 타임스탬프 |
| `sms` | 메시지 템플릿 내용 검증, 클라이언트 활성/비활성 |
| `service` | 회원 CRUD, 지갑 충전/차감/잔액부족, 암호화 통합, 관리자 PIN 인증, 거래 내역, 대시보드 KPI (in-memory SQLite) |

## 배포

`main` 브랜치 push 시 GitHub Actions가 자동 배포:

1. templ generate + Tailwind CSS build
2. Linux amd64 크로스 컴파일 (`CGO_ENABLED=0`)
3. 바이너리 + static + migrations를 VPS에 업로드
4. goose 마이그레이션 실행
5. systemd 서비스 재시작

## 주요 명령

```bash
make dev            # 개발 모드
make build          # Linux amd64 크로스 컴파일
make css-watch      # Tailwind watch
make templ-watch    # templ watch
make migrate-up     # 마이그레이션 적용
make test           # 테스트
```
