package postgres

import "testing"

func TestStatReportsZeroWithoutAPool(t *testing.T) {
	t.Parallel()

	pg := &Postgres{}

	if got := pg.Stat(); got != (PoolStat{}) {
		t.Errorf("Stat() = %+v, want the zero value", got)
	}
}

func TestStatReportsZeroWhenThePoolHasNoStats(t *testing.T) {
	t.Parallel()

	pg := &Postgres{pool: &fakePool{}}

	if got := pg.Stat(); got != (PoolStat{}) {
		t.Errorf("Stat() = %+v, want the zero value", got)
	}
}
