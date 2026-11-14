package entity

import (
	"errors"
	"testing"
)

func TestUploadPartCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		size    int64
		part    int64
		want    int
		wantErr error
	}{
		{name: "exact multiple", size: 100 << 20, part: 50 << 20, want: 2},
		{name: "remainder", size: 10<<20 + 1, part: 5 << 20, want: 3},
		{name: "ten gibibytes", size: 10 << 30, part: 50 << 20, want: 205},
		{name: "zero bytes", size: 0, part: 50 << 20, want: 0},
		{name: "below minimum part", size: 10, part: 1, wantErr: ErrInvalidUpload},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := UploadPartCount(tt.size, tt.part)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("UploadPartCount() error = %v, want %v", err, tt.wantErr)
			}

			if got != tt.want {
				t.Errorf("UploadPartCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestUploadSessionExpectedPartSize(t *testing.T) {
	t.Parallel()

	session := UploadSession{Size: 10<<20 + 7, PartSize: 5 << 20, PartCount: 3}

	first, err := session.ExpectedPartSize(1)
	if err != nil || first != 5<<20 {
		t.Fatalf("part 1 = %d, %v; want %d, nil", first, err, 5<<20)
	}

	last, err := session.ExpectedPartSize(3)
	if err != nil || last != 7 {
		t.Fatalf("part 3 = %d, %v; want 7, nil", last, err)
	}

	if _, err := session.ExpectedPartSize(0); !errors.Is(err, ErrUploadPartMismatch) {
		t.Errorf("part 0 error = %v, want %v", err, ErrUploadPartMismatch)
	}

	if _, err := session.ExpectedPartSize(4); !errors.Is(err, ErrUploadPartMismatch) {
		t.Errorf("part 4 error = %v, want %v", err, ErrUploadPartMismatch)
	}
}
