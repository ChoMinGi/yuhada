// Package page — v2 페이지 view props 정의.
//
// backend (핸들러) 와 UI (templ) 사이의 계약. 필드 이름·타입은
// backend 산출물의 참조 지점이므로 임의 변경 금지.
package page

// LoginProps — /v2/login 페이지.
type LoginProps struct {
	ErrorMsg     string
	Next         string
	KakaoEnabled bool // 서버에 카카오 OAuth 설정되어 있으면 버튼 노출
}

// DashboardProps — /v2/admin 대시보드 (v1 랜딩 스타일).
type DashboardProps struct {
	Period      string // today | this-week | this-month | all
	PeriodLabel string // 화면 표시용 "TODAY" 등 (영문 uppercase)
	DateTitle   string // 큰 h1: "2026년 7월 19일 일요일" 또는 기간 라벨
	ChargeTotal string // "₩520,000"
	DeductTotal string // "₩140,000"
	Recent      []DashActivity
}

// DashActivity — 최근 거래 리스트 한 줄 (dashboard 랜딩용).
type DashActivity struct {
	MemberID   string
	MemberName string
	Type       string // charge | deduct | refund
	Amount     string // "-₩140,000" 등 부호 포함
	Memo       string
	At         string // "7월 18일"
}

// MemberRow — 회원 목록 한 줄.
type MemberRow struct {
	ID       string
	Name     string
	Phone    string
	Balance  string
	Card     string
	ZeroBal  bool
	Inactive bool
}

// MembersProps — 회원 목록 페이지.
type MembersProps struct {
	Rows  []MemberRow
	Query string
}

// NewMemberProps — 신규 회원 등록 폼.
type NewMemberProps struct {
	Name      string
	Phone     string
	Card      string
	Memo      string
	ErrorMsg  string
	AmountStr string
}

// MemberTx — 회원 상세 최근 거래 한 줄. R3 LEFT JOIN 결과 소비 형태.
type MemberTx struct {
	Type      string // charge | deduct | refund
	Amount    string // "+₩240,000" 등
	Memo      string
	At        string
	TierLabel string // 정액권 뱃지 (비어있으면 일반)
	PayMethod string // cash | card | ""
	PerkLabel string
}

// MemberMembership — 회원 상세 MEMBERSHIPS 섹션 한 줄.
type MemberMembership struct {
	ID           string
	TierLabel    string
	PayMethod    string
	PurchasedAt  string
	CreditAmount string
	PerkLabel    string
	Status       string
}

// MemberDetailProps — 회원 상세 페이지.
type MemberDetailProps struct {
	ID          string
	Name        string
	Phone       string
	Card        string
	Memo        string
	Active      bool
	Balance     string
	BalanceInt  int64
	TotalIn     string
	TotalOut    string
	Memberships []MemberMembership
	RecentTxs   []MemberTx
	JoinedAt    string
}

// PlanRow — 가격표 관리 화면 한 줄.
type PlanRow struct {
	ID            string
	TierLabel     string
	TierAmount    int64
	PaymentMethod string
	Price         int64
	CreditAmount  int64
	Bonus         int64 // credit_amount - price
	PerkLabel     string
	DisplayOrder  int64
	IsActive      bool
}

// PlansProps — 가격표 관리 페이지.
type PlansProps struct {
	Rows     []PlanRow
	ErrorMsg string
}

// ChargeModalProps — 충전 모달 (6장 카드 + 기타 금액).
type ChargeModalProps struct {
	MemberID   string
	MemberName string // 확인 다이얼로그용
	Plans      []PlanRow
	BalanceInt int64
}

// DeductModalProps — 차감 모달.
// DeductAmts / ServiceMemos 는 활성 프리셋 기반 (설정 하위 메뉴에서 관리).
type DeductModalProps struct {
	MemberID     string
	MemberName   string // 확인 다이얼로그용
	DeductAmts   []int64
	ServiceMemos []string
	BalanceInt   int64
}

// DeductAmountRow — 차감 금액 프리셋 설정 페이지 한 줄.
type DeductAmountRow struct {
	ID           string
	Amount       int64
	DisplayOrder int64
	IsActive     bool
}

// SettingsDeductAmountsProps — /v2/admin/settings/deduct-amounts 페이지.
type SettingsDeductAmountsProps struct {
	Rows     []DeductAmountRow
	ErrorMsg string
}

// ServiceMemoRow — 시술 메모 프리셋 설정 페이지 한 줄.
type ServiceMemoRow struct {
	ID           string
	Label        string
	DisplayOrder int64
	IsActive     bool
}

// SettingsServiceMemosProps — /v2/admin/settings/service-memos 페이지.
type SettingsServiceMemosProps struct {
	Rows     []ServiceMemoRow
	ErrorMsg string
}

// ─────────────────────────────────────────────
// Sales dashboard (/v2/admin/sales)
// ─────────────────────────────────────────────

// SalesSummaryCards — 상단 8장 카드에 뿌리는 요약 (포맷 완료).
// 첫 4장 = 매출/부채/보너스/건수, 다음 4장 = 객단가/활성/정액권 비율/재구매.
type SalesSummaryCards struct {
	RealRevenue   string // "₩1,200,000"
	CreditTotal   string
	BonusCost     string
	TxCount       string // "12건"
	AvgTicket     string // "₩200,000"
	ActiveMembers string // "12명"
	PlanShare     string // "83%"
	RepeatMembers string // "3명"
	// CreditUsedPct — 잔액 적립 총액 대비 실제 소진(차감+환불) 비율 (0..100).
	// UI 에서 카드 뒷배경 프로그레스로 시각화.
	CreditUsedPct int
}

// SalesTopMember — TOP 회원 위젯 한 줄 (포맷 완료).
type SalesTopMember struct {
	MemberID   string
	Name       string // 복호화된 이름
	TotalPrice string // "₩1,200,000"
	Count      string // "3건"
	Rank       int    // 1..N
}

// SalesTierRow — 등급별 판매 리스트 한 줄 (포맷 완료).
type SalesTierRow struct {
	TierLabel   string
	PayMethod   string // "card" | "cash" | "" (일반 충전)
	IsGeneric   bool
	CountLabel  string // "3건"
	TotalPrice  string // "₩600,000"
	TotalCredit string
	TotalBonus  string // "" 이면 하이픈 표시
	HasBonus    bool
}

// SalesPayMethodRow — 결제수단 비율 한 줄.
type SalesPayMethodRow struct {
	PayMethod  string // "card" | "cash" | "unknown"
	CountLabel string // "5건"
	TotalPrice string // "₩1,500,000"
	Percent    int    // 0..100 (합계 기준)
}

// SalesRecentRow — 최근 판매 이력 한 줄.
type SalesRecentRow struct {
	Credit     string // 보너스 적립으로 판매액과 다를 때 표시
	MemberID   string
	MemberName string
	Amount     string // "+₩240,000"
	At         string // "7월 12일 오후 5:39"
	Relative   string // "3분 전"
	TierLabel  string // 정액권일 때만
	PayMethod  string // 정액권일 때만
	IsGeneric  bool
}

// SalesExpiring — 이번 달 만료 예정 정액권 한 줄.
type SalesExpiring struct {
	MemberID     string
	MemberName   string
	TierLabel    string
	PayMethod    string
	CreditAmount string // "₩240,000"
	ExpiresAt    string // "8월 3일" 형식 (KST)
}

// SalesDormant — 60일 이상 미방문 회원 한 줄.
type SalesDormant struct {
	MemberID string
	Name     string
	Balance  string // "₩240,000"
	LastAt   string // "6월 15일" 또는 "방문 이력 없음"
}

// SalesProps — /v2/admin/sales 페이지 전체.
type SalesProps struct {
	Period       string // today | this-week | this-month | last-month | all | custom
	PeriodLabel  string // 화면 상단 표시용 ("이번 달", "6월 1일 – 6월 30일" 등)
	PeriodFrom   string // YYYY-MM-DD (input[type=date] 초기값)
	PeriodTo     string // YYYY-MM-DD (input[type=date] 초기값)
	SummaryCards SalesSummaryCards
	TierRows     []SalesTierRow
	PayRows      []SalesPayMethodRow
	RecentSales  []SalesRecentRow
	// 관리 알림 (기간 필터와 무관: 항상 오늘 기준)
	ExpiringTotal string // 이번 달 만료 예정 금액 총합 "₩1,200,000"
	ExpiringCount int    // 개수
	Expiring      []SalesExpiring
	DormantCount  int    // 60일 이상 미방문 회원 수
	DormantTotal  string // 미방문 회원 잔액 총합 "₩450,000"
	Dormant       []SalesDormant
	TopMembers    []SalesTopMember
	// Chart.js 초기화용 사전 인코딩 JSON. handler 에서 준비.
	DailyLabelsJSON string // ["07-01","07-02",...]
	DailyValuesJSON string // [0,240000,0,...]
	TierChartJSON   string // {labels:[...], values:[...]}
	PayChartJSON    string // {labels:["카드","현금"], values:[3,4]}
}
