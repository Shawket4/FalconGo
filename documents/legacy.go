package documents

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"Falcon/Models"

	"gorm.io/gorm"
)

// The expiry dates used to be text columns on cars and drivers, and the
// images a file name column each. Three things keep the two worlds one:
//
//   - mirror: after any change to a document, the old expiry column is set to
//     its current version's date, so everything that still reads the column
//     (the fleet dashboard's SQL, the maintenance cache, the Excel export)
//     sees what the documents say.
//   - AdoptLegacyDates: the profile endpoints (and the AI advisor through
//     them) still accept those dates; a date they write is taken into the
//     document as a correction of its current version.
//   - Backfill: at boot, an owner whose old columns hold a date or an image
//     and who has no document of that type yet gets one, as its first version.

// mirror writes a document's current expiry date to its owner's old column.
func mirror(tx *gorm.DB, doc Models.Document) error {
	typ, known := TypeByKey(doc.TypeKey)
	if !known || typ.mirrorColumn == "" || doc.Label != "" {
		return nil
	}
	return tx.Exec(fmt.Sprintf(`
        UPDATE %s SET %s = COALESCE((
            SELECT to_char(v.expiry_date, 'YYYY-MM-DD') FROM document_versions v
            WHERE v.document_id = ? AND v.hidden_at IS NULL
            ORDER BY v.id DESC LIMIT 1), '')
        WHERE id = ?`, owners[doc.OwnerKind].table, typ.mirrorColumn),
		doc.ID, doc.OwnerID).Error
}

// parseLegacyDate reads one of the old text dates. Empty is no date. The
// layouts are the ones services.docDaysLeft has always accepted.
func parseLegacyDate(raw string) (date *string, ok bool) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return nil, true
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, v); err == nil {
			d := t.Format("2006-01-02")
			return &d, true
		}
	}
	return nil, false
}

// CheckLegacyDate refuses text that is neither empty nor a date. The profile
// endpoints run it on every expiry date they are sent.
func CheckLegacyDate(raw string) error {
	if _, ok := parseLegacyDate(raw); !ok {
		return refuse("invalid_date", "dates must be YYYY-MM-DD")
	}
	return nil
}

// AdoptLegacyDates takes the dates a profile endpoint just wrote to an
// owner's old expiry columns into its documents: a changed date corrects the
// current version (or starts the document when there is none), an emptied one
// clears it. Call it in the transaction that wrote the columns.
func AdoptLegacyDates(tx *gorm.DB, ownerKind string, ownerID uint, by Actor) error {
	o, ok := owners[ownerKind]
	if !ok {
		return refuse("unknown_owner_kind", "unknown owner kind")
	}
	for _, typ := range TypesFor(ownerKind) {
		if typ.mirrorColumn == "" {
			continue
		}
		var raw string
		if err := tx.Raw(fmt.Sprintf(`SELECT COALESCE(%s, '') FROM %s WHERE id = ?`,
			typ.mirrorColumn, o.table), ownerID).Scan(&raw).Error; err != nil {
			return err
		}
		want, valid := parseLegacyDate(raw)
		if !valid {
			// Old text that was never a date. The profile endpoints check
			// what they are sent (CheckLegacyDate), so this was there before
			// and is left as it is rather than blocking every later edit.
			continue
		}

		var doc Models.Document
		err := tx.Where("owner_kind = ? AND owner_id = ? AND type_key = ? AND label = ''",
			ownerKind, ownerID, typ.Key).First(&doc).Error
		if err != nil && notFound(err) != ErrNotFound {
			return err
		}
		var current []struct {
			ID         uint
			ExpiryDate *string
		}
		if doc.ID != 0 {
			if err := tx.Raw(`
                SELECT id, to_char(expiry_date, 'YYYY-MM-DD') AS expiry_date
                FROM document_versions
                WHERE document_id = ? AND hidden_at IS NULL
                ORDER BY id DESC LIMIT 1`, doc.ID).Scan(&current).Error; err != nil {
				return err
			}
		}

		switch {
		case len(current) == 1 && sameDate(current[0].ExpiryDate, want):
			// Nothing to adopt; mirror below still normalises the text.
		case len(current) == 1:
			if err := tx.Exec(`
                UPDATE document_versions
                SET expiry_date = ?::date, updated_by_name = ?, updated_at = now()
                WHERE id = ?`, want, by.Name, current[0].ID).Error; err != nil {
				return err
			}
		case want == nil:
			continue // no date and no document: nothing to record
		default:
			if doc.ID == 0 {
				doc = Models.Document{OwnerKind: ownerKind, OwnerID: ownerID, TypeKey: typ.Key}
				if err := tx.Create(&doc).Error; err != nil {
					return err
				}
			}
			if _, err := insertVersion(tx, doc.ID, nil, want, "", "profile", by); err != nil {
				return err
			}
		}
		if err := mirror(tx, doc); err != nil {
			return err
		}
	}
	return nil
}

func sameDate(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// legacyUploads is where the old image columns' files live.
var legacyUploads = "./uploads"

// Backfill imports the old columns: for every owner and every type that has
// them, a date or an image with no document yet becomes that document's first
// version. It only ever adds a missing document, so it is safe on every boot
// and never overrides what was recorded since. It returns how many documents
// it created.
func Backfill(db *gorm.DB) (int, error) {
	created := 0
	for _, typ := range catalogue {
		if typ.mirrorColumn == "" && len(typ.legacy) == 0 {
			continue
		}
		n, err := backfillType(db, typ)
		created += n
		if err != nil {
			return created, fmt.Errorf("%s: %w", typ.Key, err)
		}
	}
	return created, nil
}

func backfillType(db *gorm.DB, typ Type) (int, error) {
	o := owners[typ.OwnerKind]
	columns := []string{"o.id"}
	filled := []string{}
	add := func(column string) {
		columns = append(columns, fmt.Sprintf("COALESCE(o.%s, '')", column))
		filled = append(filled, fmt.Sprintf("COALESCE(o.%s, '') <> ''", column))
	}
	if typ.mirrorColumn != "" {
		add(typ.mirrorColumn)
	}
	for _, lf := range typ.legacy {
		add(lf.column)
	}
	rows, err := db.Raw(fmt.Sprintf(`
        SELECT %s FROM %s o
        WHERE o.deleted_at IS NULL AND (%s)
          AND NOT EXISTS (
              SELECT 1 FROM documents d
              WHERE d.owner_kind = ? AND d.owner_id = o.id AND d.type_key = ? AND d.label = '')
        ORDER BY o.id`,
		strings.Join(columns, ", "), o.table, strings.Join(filled, " OR ")),
		typ.OwnerKind, typ.Key).Rows()
	if err != nil {
		return 0, err
	}
	type legacyRow struct {
		ownerID uint
		values  []string
	}
	var pending []legacyRow
	for rows.Next() {
		r := legacyRow{values: make([]string, len(columns)-1)}
		dest := []any{&r.ownerID}
		for i := range r.values {
			dest = append(dest, &r.values[i])
		}
		if err := rows.Scan(dest...); err != nil {
			rows.Close()
			return 0, err
		}
		pending = append(pending, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	created := 0
	for _, r := range pending {
		values := r.values
		var expiry *string
		if typ.mirrorColumn != "" {
			// Text that was never a date imports as no date.
			expiry, _ = parseLegacyDate(values[0])
			values = values[1:]
		}
		// Read the images before opening the transaction: storing one
		// writes to disk, which a rollback would not undo.
		var files []Models.DocumentFile
		for i, lf := range typ.legacy {
			name := strings.TrimSpace(values[i])
			if name == "" {
				continue
			}
			// The column holds a bare file name; Base keeps it one.
			data, err := os.ReadFile(filepath.Join(legacyUploads, lf.dir, filepath.Base(name)))
			if err != nil {
				continue // the row names a file that is not on disk
			}
			file, err := store(name, data)
			if err != nil {
				continue // not a picture or a PDF
			}
			file.Slot = lf.slot
			file.Position = len(files)
			files = append(files, file)
		}
		if expiry == nil && len(files) == 0 {
			continue
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			doc := Models.Document{OwnerKind: typ.OwnerKind, OwnerID: r.ownerID, TypeKey: typ.Key}
			if err := tx.Create(&doc).Error; err != nil {
				return err
			}
			versionID, err := insertVersion(tx, doc.ID, nil, expiry, "", "legacy", Actor{})
			if err != nil {
				return err
			}
			for i := range files {
				files[i].VersionID = &versionID
				if err := tx.Create(&files[i]).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			for _, f := range files {
				RemoveStored([]string{f.StoredName})
			}
			return created, err
		}
		created++
	}
	return created, nil
}

// BackfillAtBoot runs Backfill and logs what it did; a failure is reported
// and does not stop the server.
func BackfillAtBoot(db *gorm.DB, report func(error)) {
	defer func() {
		if r := recover(); r != nil {
			report(fmt.Errorf("documents backfill panicked: %v", r))
		}
	}()
	created, err := Backfill(db)
	if created > 0 {
		log.Printf("documents: imported %d documents from the old columns", created)
	}
	if err != nil {
		log.Printf("documents: backfill: %v", err)
		report(err)
	}
}
