package documents

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"Falcon/Models"

	"gorm.io/gorm"
)

// ValidationError is a refused request. Code is a stable key the dashboard
// translates; Msg is the English fallback and is safe to show.
type ValidationError struct{ Code, Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func refuse(code, msg string) error { return &ValidationError{Code: code, Msg: msg} }

// ErrNotFound is an owner, document, version or file that does not exist.
var ErrNotFound = errors.New("documents: not found")

// Actor is who is making a change. The name is stored beside the change, so
// the history still reads after an account is renamed or removed.
type Actor struct {
	ID   uint
	Name string
}

// FileRef places one uploaded file in a version.
type FileRef struct {
	ID   uint   `json:"id"`
	Slot string `json:"slot"`
}

// VersionInput is the body of a new version or of an edit to one. Everything
// is optional; a version only has to hold something (a date, a note or a file).
type VersionInput struct {
	// A new version of this document (a renewal). Zero for the first version,
	// which names the type instead.
	DocumentID uint   `json:"document_id"`
	TypeKey    string `json:"type_key"`
	// Tells apart the documents of a type an owner holds several of.
	Label      string `json:"label"`
	IssueDate  string `json:"issue_date"`  // YYYY-MM-DD or ""
	ExpiryDate string `json:"expiry_date"` // YYYY-MM-DD or ""
	Note       string `json:"note"`
	// The version's files, in order. On an edit, nil leaves them as they are
	// and a list is the complete new set.
	Files *[]FileRef `json:"files"`
}

// VersionView is one version as the API returns it.
type VersionView struct {
	ID            uint                  `json:"id"`
	DocumentID    uint                  `json:"-"`
	IssueDate     *string               `json:"issue_date"`
	ExpiryDate    *string               `json:"expiry_date"`
	Note          string                `json:"note"`
	Source        string                `json:"source"`
	CreatedByName string                `json:"created_by_name"`
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedByName string                `json:"updated_by_name"`
	UpdatedAt     *time.Time            `json:"updated_at"`
	Hidden        bool                  `json:"hidden" gorm:"-"`
	HiddenByName  string                `json:"hidden_by_name"`
	HiddenAt      *time.Time            `json:"hidden_at"`
	Files         []Models.DocumentFile `json:"files" gorm:"-"`
}

// DocumentView is one document with its versions, newest first.
type DocumentView struct {
	ID      uint   `json:"id"`
	TypeKey string `json:"type_key"`
	Label   string `json:"label"`
	// The visible version with the highest id; nil when every version is hidden.
	CurrentVersionID *uint `json:"current_version_id"`
	// The viewer may see that the files exist but not open them.
	Restricted bool          `json:"restricted"`
	Versions   []VersionView `json:"versions"`
}

// versionColumns reads a version with its dates as YYYY-MM-DD text: scanned
// straight into a string a date column arrives as a timestamp.
const versionColumns = `id, document_id,
    to_char(issue_date, 'YYYY-MM-DD')  AS issue_date,
    to_char(expiry_date, 'YYYY-MM-DD') AS expiry_date,
    note, source, created_by_name, created_at,
    updated_by_name, updated_at, hidden_by_name, hidden_at`

// ListOwner is every document an owner holds, in catalogue order, each with
// its history. Hidden versions are a manager's to see; for everyone else a
// document whose versions are all hidden is not there.
func ListOwner(db *gorm.DB, ownerKind string, ownerID uint, manager bool) ([]DocumentView, error) {
	out := []DocumentView{}
	var docs []Models.Document
	if err := db.Where("owner_kind = ? AND owner_id = ?", ownerKind, ownerID).
		Order("id").Find(&docs).Error; err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return out, nil
	}
	docIDs := make([]uint, len(docs))
	for i, d := range docs {
		docIDs[i] = d.ID
	}

	q := `SELECT ` + versionColumns + ` FROM document_versions WHERE document_id IN ?`
	if !manager {
		q += ` AND hidden_at IS NULL`
	}
	var versions []VersionView
	if err := db.Raw(q+` ORDER BY id DESC`, docIDs).Scan(&versions).Error; err != nil {
		return nil, err
	}
	filesByVersion := map[uint][]Models.DocumentFile{}
	if len(versions) > 0 {
		versionIDs := make([]uint, len(versions))
		for i, v := range versions {
			versionIDs[i] = v.ID
		}
		var files []Models.DocumentFile
		if err := db.Where("version_id IN ?", versionIDs).
			Order("position, id").Find(&files).Error; err != nil {
			return nil, err
		}
		for _, f := range files {
			filesByVersion[*f.VersionID] = append(filesByVersion[*f.VersionID], f)
		}
	}
	byDoc := map[uint][]VersionView{}
	for _, v := range versions {
		v.Hidden = v.HiddenAt != nil
		v.Files = filesByVersion[v.ID]
		if v.Files == nil {
			v.Files = []Models.DocumentFile{}
		}
		byDoc[v.DocumentID] = append(byDoc[v.DocumentID], v)
	}

	order := map[string]int{}
	for i, t := range catalogue {
		order[t.Key] = i
	}
	for _, d := range docs {
		typ, known := TypeByKey(d.TypeKey)
		vs := byDoc[d.ID]
		if !known || len(vs) == 0 {
			continue
		}
		view := DocumentView{
			ID: d.ID, TypeKey: d.TypeKey, Label: d.Label,
			Restricted: typ.Sensitive && !manager,
			Versions:   vs,
		}
		for i := range vs {
			if !vs[i].Hidden {
				id := vs[i].ID
				view.CurrentVersionID = &id
				break
			}
		}
		out = append(out, view)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := order[out[i].TypeKey], order[out[j].TypeKey]; a != b {
			return a < b
		}
		return out[i].Label < out[j].Label
	})
	return out, nil
}

// OverviewRow is one document's current version, for the fleet-wide view.
type OverviewRow struct {
	DocumentID uint    `json:"document_id"`
	OwnerID    uint    `json:"owner_id"`
	TypeKey    string  `json:"type_key"`
	Label      string  `json:"label"`
	VersionID  uint    `json:"version_id"`
	IssueDate  *string `json:"issue_date"`
	ExpiryDate *string `json:"expiry_date"`
	FileCount  int     `json:"file_count"`
}

// Overview is the current version of every document held by the owners of one
// kind that still exist.
func Overview(db *gorm.DB, ownerKind string) ([]OverviewRow, error) {
	o, ok := owners[ownerKind]
	if !ok {
		return nil, refuse("unknown_owner_kind", "unknown owner kind")
	}
	rows := []OverviewRow{}
	err := db.Raw(fmt.Sprintf(`
        SELECT d.id AS document_id, d.owner_id, d.type_key, d.label,
               v.id AS version_id,
               to_char(v.issue_date, 'YYYY-MM-DD')  AS issue_date,
               to_char(v.expiry_date, 'YYYY-MM-DD') AS expiry_date,
               (SELECT count(*) FROM document_files f WHERE f.version_id = v.id) AS file_count
        FROM documents d
        JOIN %s o ON o.id = d.owner_id AND o.deleted_at IS NULL
        JOIN LATERAL (
            SELECT id, issue_date, expiry_date FROM document_versions
            WHERE document_id = d.id AND hidden_at IS NULL
            ORDER BY id DESC LIMIT 1
        ) v ON true
        WHERE d.owner_kind = ?
        ORDER BY d.owner_id, d.id`, o.table), ownerKind).Scan(&rows).Error
	return rows, err
}

// CreateVersion adds a version: the first one of a document, or a renewal of
// one that exists. It returns the document the version belongs to.
func CreateVersion(tx *gorm.DB, ownerKind string, ownerID uint, in VersionInput, by Actor) (Models.Document, error) {
	var doc Models.Document
	carType, err := loadOwner(tx, ownerKind, ownerID)
	if err != nil {
		return doc, err
	}

	var typ Type
	if in.DocumentID != 0 {
		if err := tx.First(&doc, in.DocumentID).Error; err != nil {
			return doc, notFound(err)
		}
		if doc.OwnerKind != ownerKind || doc.OwnerID != ownerID {
			return doc, ErrNotFound
		}
		var known bool
		if typ, known = TypeByKey(doc.TypeKey); !known {
			return doc, refuse("unknown_type", "unknown document type")
		}
	} else {
		var known bool
		typ, known = TypeByKey(in.TypeKey)
		if !known || typ.OwnerKind != ownerKind {
			return doc, refuse("unknown_type", "unknown document type")
		}
		label, err := cleanLabel(typ, in.Label)
		if err != nil {
			return doc, err
		}
		doc, err = findOrCreateDocument(tx, typ, ownerKind, ownerID, label)
		if err != nil {
			return doc, err
		}
	}
	if !typ.appliesToCarType(carType) {
		return doc, refuse("not_applicable", "this document does not apply to this vehicle")
	}

	issue, expiry, note, err := cleanFields(in)
	if err != nil {
		return doc, err
	}
	files := []FileRef{}
	if in.Files != nil {
		files = *in.Files
	}
	if issue == nil && expiry == nil && note == "" && len(files) == 0 {
		return doc, refuse("empty", "add a file, a date or a note")
	}

	versionID, err := insertVersion(tx, doc.ID, issue, expiry, note, "upload", by)
	if err != nil {
		return doc, err
	}
	if _, err := setFiles(tx, typ, versionID, files); err != nil {
		return doc, err
	}
	return doc, mirror(tx, doc)
}

// EditVersion corrects a version in place: its dates, note, files and, for a
// labelled document, the label. It returns the document and the stored names
// of the files the edit dropped, for the caller to remove from disk once the
// transaction has committed.
func EditVersion(tx *gorm.DB, versionID uint, in VersionInput, by Actor) (Models.Document, []string, error) {
	doc, typ, err := documentOfVersion(tx, versionID)
	if err != nil {
		return doc, nil, err
	}
	issue, expiry, note, err := cleanFields(in)
	if err != nil {
		return doc, nil, err
	}

	if typ.Multiple {
		label, err := cleanLabel(typ, in.Label)
		if err != nil {
			return doc, nil, err
		}
		if label != doc.Label {
			var taken int64
			if err := tx.Model(&Models.Document{}).
				Where("owner_kind = ? AND owner_id = ? AND type_key = ? AND label = ? AND id <> ?",
					doc.OwnerKind, doc.OwnerID, doc.TypeKey, label, doc.ID).
				Count(&taken).Error; err != nil {
				return doc, nil, err
			}
			if taken > 0 {
				return doc, nil, refuse("label_taken", "a document with this name already exists")
			}
			if err := tx.Model(&doc).Update("label", label).Error; err != nil {
				return doc, nil, err
			}
			doc.Label = label
		}
	}

	var removed []string
	fileCount := int64(0)
	if in.Files != nil {
		if removed, err = setFiles(tx, typ, versionID, *in.Files); err != nil {
			return doc, nil, err
		}
		fileCount = int64(len(*in.Files))
	} else if err := tx.Model(&Models.DocumentFile{}).
		Where("version_id = ?", versionID).Count(&fileCount).Error; err != nil {
		return doc, nil, err
	}
	if issue == nil && expiry == nil && note == "" && fileCount == 0 {
		return doc, nil, refuse("empty", "add a file, a date or a note")
	}

	if err := tx.Exec(`
        UPDATE document_versions
        SET issue_date = ?::date, expiry_date = ?::date, note = ?,
            updated_by_name = ?, updated_at = now()
        WHERE id = ?`, issue, expiry, note, by.Name, versionID).Error; err != nil {
		return doc, nil, err
	}
	return doc, removed, mirror(tx, doc)
}

// HideVersion takes a version out of sight. It stays in the history for
// managers, who can restore it; the version before it becomes current.
func HideVersion(tx *gorm.DB, versionID uint, by Actor) (Models.Document, error) {
	return setHidden(tx, versionID, true, by)
}

// RestoreVersion brings a hidden version back.
func RestoreVersion(tx *gorm.DB, versionID uint, by Actor) (Models.Document, error) {
	return setHidden(tx, versionID, false, by)
}

func setHidden(tx *gorm.DB, versionID uint, hidden bool, by Actor) (Models.Document, error) {
	doc, _, err := documentOfVersion(tx, versionID)
	if err != nil {
		return doc, err
	}
	q := `UPDATE document_versions SET hidden_at = NULL, hidden_by_name = '' WHERE id = ?`
	args := []any{versionID}
	if hidden {
		q = `UPDATE document_versions SET hidden_at = now(), hidden_by_name = ?
             WHERE id = ? AND hidden_at IS NULL`
		args = []any{by.Name, versionID}
	}
	if err := tx.Exec(q, args...).Error; err != nil {
		return doc, err
	}
	return doc, mirror(tx, doc)
}

// ── helpers ─────────────────────────────────────────────────────────────

func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

// loadOwner checks the owner exists and returns its car type ("" for owners
// that are not cars), which decides whether a type applies to it.
func loadOwner(tx *gorm.DB, ownerKind string, ownerID uint) (string, error) {
	o, ok := owners[ownerKind]
	if !ok {
		return "", refuse("unknown_owner_kind", "unknown owner kind")
	}
	carType := "''"
	if ownerKind == OwnerCar {
		carType = "COALESCE(car_type, '')"
	}
	var rows []struct{ CarType string }
	if err := tx.Raw(fmt.Sprintf(
		`SELECT %s AS car_type FROM %s WHERE id = ? AND deleted_at IS NULL`, carType, o.table,
	), ownerID).Scan(&rows).Error; err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", ErrNotFound
	}
	return rows[0].CarType, nil
}

func documentOfVersion(tx *gorm.DB, versionID uint) (Models.Document, Type, error) {
	var doc Models.Document
	err := tx.Raw(`
        SELECT d.* FROM documents d
        JOIN document_versions v ON v.document_id = d.id
        WHERE v.id = ?`, versionID).Scan(&doc).Error
	if err != nil {
		return doc, Type{}, err
	}
	if doc.ID == 0 {
		return doc, Type{}, ErrNotFound
	}
	typ, known := TypeByKey(doc.TypeKey)
	if !known {
		return doc, Type{}, refuse("unknown_type", "unknown document type")
	}
	return doc, typ, nil
}

const (
	maxLabelRunes = 80
	maxNoteRunes  = 1000
)

// cleanLabel trims a label. Only the types an owner holds several of have one.
func cleanLabel(typ Type, raw string) (string, error) {
	if !typ.Multiple {
		return "", nil
	}
	label := strings.Join(strings.Fields(raw), " ")
	if utf8.RuneCountInString(label) > maxLabelRunes {
		return "", refuse("label_too_long", "the name is too long")
	}
	return label, nil
}

func cleanFields(in VersionInput) (issue, expiry *string, note string, err error) {
	if issue, err = cleanDate(in.IssueDate); err != nil {
		return
	}
	if expiry, err = cleanDate(in.ExpiryDate); err != nil {
		return
	}
	// YYYY-MM-DD compares as text.
	if issue != nil && expiry != nil && *expiry < *issue {
		err = refuse("expiry_before_issue", "the expiry date is before the issue date")
		return
	}
	note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(note) > maxNoteRunes {
		err = refuse("note_too_long", "the note is too long")
	}
	return
}

func cleanDate(raw string) (*string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return nil, nil
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return nil, refuse("invalid_date", "dates must be YYYY-MM-DD")
	}
	return &v, nil
}

// findOrCreateDocument returns the owner's document of a type. A type an
// owner holds one of is renewed by a new version, so the existing row is the
// answer. A labelled type is a new document each time, so a label already in
// use is refused, unless everything under it was hidden, which frees it.
func findOrCreateDocument(tx *gorm.DB, typ Type, ownerKind string, ownerID uint, label string) (Models.Document, error) {
	var doc Models.Document
	err := tx.Where("owner_kind = ? AND owner_id = ? AND type_key = ? AND label = ?",
		ownerKind, ownerID, typ.Key, label).First(&doc).Error
	switch {
	case err == nil:
		if typ.Multiple {
			var visible int64
			if err := tx.Table("document_versions").
				Where("document_id = ? AND hidden_at IS NULL", doc.ID).
				Count(&visible).Error; err != nil {
				return doc, err
			}
			if visible > 0 {
				return doc, refuse("label_taken", "a document with this name already exists")
			}
		}
		return doc, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		doc = Models.Document{OwnerKind: ownerKind, OwnerID: ownerID, TypeKey: typ.Key, Label: label}
		return doc, tx.Create(&doc).Error
	default:
		return doc, err
	}
}

func insertVersion(tx *gorm.DB, documentID uint, issue, expiry *string, note, source string, by Actor) (uint, error) {
	var id uint
	err := tx.Raw(`
        INSERT INTO document_versions
            (document_id, issue_date, expiry_date, note, source, created_by, created_by_name)
        VALUES (?, ?::date, ?::date, ?, ?, ?, ?)
        RETURNING id`, documentID, issue, expiry, note, source, by.ID, by.Name).Scan(&id).Error
	return id, err
}

// setFiles makes refs the version's files, in order. A ref is either a file
// the version already holds (kept, possibly moved) or a staged upload (now
// attached). Files the version held that refs leaves out are deleted; their
// stored names are returned.
func setFiles(tx *gorm.DB, typ Type, versionID uint, refs []FileRef) ([]string, error) {
	var held []Models.DocumentFile
	if err := tx.Where("version_id = ?", versionID).Find(&held).Error; err != nil {
		return nil, err
	}
	heldByID := map[uint]Models.DocumentFile{}
	for _, f := range held {
		heldByID[f.ID] = f
	}

	perSlot := map[string]int{}
	seen := map[uint]bool{}
	for position, ref := range refs {
		slot, ok := typ.slot(ref.Slot)
		if !ok {
			return nil, refuse("unknown_slot", "this document does not take that file")
		}
		if perSlot[slot.Key]++; perSlot[slot.Key] > slot.Max {
			return nil, refuse("too_many_files", "too many files")
		}
		if seen[ref.ID] {
			return nil, refuse("duplicate_file", "the same file is listed twice")
		}
		seen[ref.ID] = true

		file, mine := heldByID[ref.ID]
		if !mine {
			if err := tx.First(&file, ref.ID).Error; err != nil {
				return nil, notFound(err)
			}
			if file.VersionID != nil {
				return nil, refuse("file_in_use", "this file belongs to another document")
			}
		}
		if typ.ImagesOnly && !isImage(file.ContentType) {
			return nil, refuse("images_only", "this document takes pictures only")
		}
		if err := tx.Model(&Models.DocumentFile{}).Where("id = ?", file.ID).
			Updates(map[string]any{"version_id": versionID, "slot": slot.Key, "position": position}).
			Error; err != nil {
			return nil, err
		}
	}

	var removed []string
	for _, f := range held {
		if seen[f.ID] {
			continue
		}
		if err := tx.Delete(&Models.DocumentFile{}, f.ID).Error; err != nil {
			return nil, err
		}
		removed = append(removed, f.StoredName)
	}
	return removed, nil
}
