package auth

import (
	"testing"
	"time"
)

func TestIsExpired(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{
			name:      "clearly not expired",
			expiresAt: time.Now().Add(1 * time.Hour),
			want:      false,
		},
		{
			name:      "clearly expired",
			expiresAt: time.Now().Add(-1 * time.Hour),
			want:      true,
		},
		{
			name:      "within 2-minute buffer is considered expired",
			expiresAt: time.Now().Add(1 * time.Minute),
			want:      true,
		},
		{
			name:      "just outside 2-minute buffer is not expired",
			expiresAt: time.Now().Add(3 * time.Minute),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &TokenCache{ExpiresAt: tt.expiresAt}
			got := cache.IsExpired()
			if got != tt.want {
				t.Errorf("IsExpired() = %v, want %v (expiresAt %v from now)", got, tt.want, time.Until(tt.expiresAt).Round(time.Second))
			}
		})
	}
}
