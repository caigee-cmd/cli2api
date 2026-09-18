package control

import "testing"

func TestModelContextPolicy(t *testing.T) {
	for _, tt := range []struct {
		model, key string
		context    int
	}{
		{"", "", 180000},
		{" \t", "", 180000},
		{"auto", "auto", 180000},
		{" MINIMAX_M3 ", "minimax-m3", 1000000},
		{"MiniMax M3", "minimax-m3", 1000000},
		{" GLM_5.2 ", "glm-5.2", 180000},
	} {
		t.Run(tt.model, func(t *testing.T) {
			if got := ModelContextKey(tt.model); got != tt.key {
				t.Fatalf("key=%q want %q", got, tt.key)
			}
			if got := DefaultContextForModel(tt.model); got != tt.context {
				t.Fatalf("context=%d want %d", got, tt.context)
			}
		})
	}
}
