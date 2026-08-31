package Observability

import (
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"gorm.io/gorm"
)

// slowQuery is the threshold above which a query is worth a breadcrumb of its
// own. Everything slower than this is recorded; faster queries are not, because
// a busy request would otherwise push its own useful breadcrumbs out of the
// buffer with hundreds of 2ms selects.
const slowQuery = 100 * time.Millisecond

// InstrumentDB records slow and failing queries as Sentry breadcrumbs.
//
// When a 500 fires, the event then carries the queries that ran just before it
// and how long each took — which answers most of "why was this slow" and "what
// did it just run" without a profiler.
//
// The SQL is recorded WITHOUT its bound parameters. GORM can render a statement
// with values interpolated, and on this schema those values are driver names,
// phone numbers and plates. The scrubber redacts by key and cannot clean a SQL
// string, so the parameters must never be put into one.
func InstrumentDB(db *gorm.DB) error {
	before := func(tx *gorm.DB) {
		tx.InstanceSet("sentry:start", time.Now())
	}

	after := func(op string) func(*gorm.DB) {
		return func(tx *gorm.DB) {
			v, ok := tx.InstanceGet("sentry:start")
			if !ok {
				return
			}
			start, ok := v.(time.Time)
			if !ok {
				return
			}
			elapsed := time.Since(start)
			failed := tx.Error != nil && tx.Error != gorm.ErrRecordNotFound

			if !failed && elapsed < slowQuery {
				return
			}

			level := sentry.LevelInfo
			if failed {
				level = sentry.LevelError
			}

			sentry.AddBreadcrumb(&sentry.Breadcrumb{
				Type:     "query",
				Category: "db." + op,
				Level:    level,
				// tx.Statement.SQL is the prepared text; the parameters live in
				// tx.Statement.Vars and are deliberately not touched.
				Message: truncate(tx.Statement.SQL.String(), 500),
				Data: map[string]interface{}{
					"table":    tx.Statement.Table,
					"rows":     tx.Statement.RowsAffected,
					"duration": elapsed.String(),
					"failed":   failed,
				},
			})
		}
	}

	for _, cb := range []struct {
		op   string
		reg  func() error
		name string
	}{
		{"query", func() error {
			return db.Callback().Query().Before("gorm:query").Register("sentry:before_query", before)
		}, ""},
		{"create", func() error {
			return db.Callback().Create().Before("gorm:create").Register("sentry:before_create", before)
		}, ""},
		{"update", func() error {
			return db.Callback().Update().Before("gorm:update").Register("sentry:before_update", before)
		}, ""},
		{"delete", func() error {
			return db.Callback().Delete().Before("gorm:delete").Register("sentry:before_delete", before)
		}, ""},
	} {
		if err := cb.reg(); err != nil {
			return err
		}
	}

	if err := db.Callback().Query().After("gorm:query").Register("sentry:after_query", after("query")); err != nil {
		return err
	}
	if err := db.Callback().Create().After("gorm:create").Register("sentry:after_create", after("create")); err != nil {
		return err
	}
	if err := db.Callback().Update().After("gorm:update").Register("sentry:after_update", after("update")); err != nil {
		return err
	}
	return db.Callback().Delete().After("gorm:delete").Register("sentry:after_delete", after("delete"))
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
