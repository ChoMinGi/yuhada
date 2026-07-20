-- +goose Up
-- 6종 정액권 시드 (pricing-model.md §3 표 그대로, 2026-06-28 확정).
-- idempotent: INSERT OR IGNORE — 매 부팅마다 실행되어도 중복 없음.
-- id 는 의미 있는 slug (디버깅 편의). 사장이 추후 추가/수정할 때는 앱에서 UUID.
--
-- display_order 규칙: tier(20→30→50) * 10 + payment_method(card=1, cash=2)
--   → 11, 12, 21, 22, 31, 32
--
-- price      = 손님이 실제 내는 돈 (세금 신고 매출)
-- credit_amount = 잔액에 적립되는 금액 (선수금)
-- 보너스     = credit_amount - price (실질 할인/마케팅 비용)

-- +goose StatementBegin
INSERT OR IGNORE INTO v2_membership_plans
  (id, tier_label, tier_amount, payment_method, price, credit_amount, perk_label, display_order, is_active)
VALUES
  ('plan-20-card', '20만원 회원권', 200000, 'card', 200000, 220000, NULL,                        11, 1),
  ('plan-20-cash', '20만원 회원권', 200000, 'cash', 200000, 240000, NULL,                        12, 1),
  ('plan-30-card', '30만원 회원권', 300000, 'card', 300000, 330000, '홈케어 18,000원 증정',       21, 1),
  ('plan-30-cash', '30만원 회원권', 300000, 'cash', 300000, 360000, '홈케어 18,000원 증정',       22, 1),
  ('plan-50-card', '50만원 회원권', 500000, 'card', 500000, 550000, '모로칸 오일 32,000원 증정',  31, 1),
  ('plan-50-cash', '50만원 회원권', 500000, 'cash', 500000, 600000, '모로칸 오일 32,000원 증정',  32, 1);
-- +goose StatementEnd

-- +goose Down
-- 시드는 되돌리지 않음 (수동 삭제 필요 시 DELETE FROM v2_membership_plans WHERE id LIKE 'plan-%').
SELECT 1;
