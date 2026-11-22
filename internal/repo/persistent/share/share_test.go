package share

import "testing"

// The expiry condition is one parenthesized expression. Without the
// parentheses, the ANDs that scope the share to its owner and folder bind first
// and the unparenthesized OR lets another account's unexpired share through.
func TestActiveShareWhereParenthesizesTheOr(t *testing.T) {
	t.Parallel()

	sql, args, err := activeShareWhere().ToSql()
	if err != nil {
		t.Fatalf("ToSql() error = %v, want nil", err)
	}

	want := "(expires_at IS NULL OR expires_at > now())"
	if sql != want {
		t.Errorf("SQL = %q, want %q", sql, want)
	}

	if len(args) != 0 {
		t.Errorf("args = %v, want none", args)
	}
}
