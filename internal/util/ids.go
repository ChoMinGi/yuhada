package util

import (
	"fmt"
	"math/rand/v2"

	"github.com/google/uuid"
)

// NewID — google/uuid v7 (시간순 정렬 가능, SQLite 인덱스 친화적)
func NewID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New().String() // fallback to v4
	}
	return id.String()
}

// NewCardUUID — "CARD-XXXX" 형식 (4자리 숫자).
// 중복 가능성 있으나 Go 레이어에서 확인 후 재시도. 사장님 누나용 규모에선 실질 충돌 X.
func NewCardUUID() string {
	return fmt.Sprintf("CARD-%04d", rand.IntN(10000))
}
