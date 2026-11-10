package Models

import (
	"time"

	"gorm.io/gorm"
)

// Documents: the papers and photos a car or a driver carries.
//
// One model for every owner and every kind of paper:
//
//	owner (kind + id) → document (one per type, or one per label for the
//	types an owner holds several of) → version (dates, note, who and when)
//	→ files (front, back, page, photo).
//
// A renewal is a new version, so the history of a document is its versions.
// The current version is the visible one with the highest id; a manager
// hides a version instead of deleting it, and can bring it back.
//
// Which types exist, what they hold and who they belong to is the catalogue
// in the documents package; nothing here knows a licence from a photo.

// Document is one paper an owner holds: its type and, for the types an owner
// holds several of (a site pass per oil company), the label that tells them
// apart. Label is empty for every other type.
type Document struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	OwnerKind string    `json:"owner_kind"`
	OwnerID   uint      `json:"owner_id"`
	TypeKey   string    `json:"type_key"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

func (Document) TableName() string { return "documents" }

// DocumentFile is one stored file. VersionID is nil while the file is staged:
// uploaded, not yet attached to a version by a save.
type DocumentFile struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	VersionID    *uint     `json:"-"`
	Slot         string    `json:"slot"`
	Position     int       `json:"position"`
	StoredName   string    `json:"-"`
	OriginalName string    `json:"original_name"`
	ContentType  string    `json:"content_type"`
	Size         int64     `json:"size"`
	HasThumb     bool      `json:"has_thumb"`
	UploadedBy   uint      `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

func (DocumentFile) TableName() string { return "document_files" }

// MigrateDocuments creates the documents tables and the vehicle identity
// columns on cars (chassis, engine, cylinders, weight, payload, colour, and the trailer's own).
// Idempotent: it runs on every boot. Importing the old per-column dates and
// images into these tables is documents.Backfill, run from main after this.
func MigrateDocuments(db *gorm.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS documents (
            id          bigserial   PRIMARY KEY,
            owner_kind  text        NOT NULL,
            owner_id    bigint      NOT NULL,
            type_key    text        NOT NULL,
            label       text        NOT NULL DEFAULT '',
            created_at  timestamptz NOT NULL DEFAULT now(),
            UNIQUE (owner_kind, owner_id, type_key, label)
        )`,
		`CREATE TABLE IF NOT EXISTS document_versions (
            id               bigserial   PRIMARY KEY,
            document_id      bigint      NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
            issue_date       date,
            expiry_date      date,
            note             text        NOT NULL DEFAULT '',
            source           text        NOT NULL DEFAULT 'upload'
                CHECK (source IN ('upload', 'profile', 'legacy')),
            created_by       bigint      NOT NULL DEFAULT 0,
            created_by_name  text        NOT NULL DEFAULT '',
            created_at       timestamptz NOT NULL DEFAULT now(),
            updated_by_name  text        NOT NULL DEFAULT '',
            updated_at       timestamptz,
            hidden_by_name   text        NOT NULL DEFAULT '',
            hidden_at        timestamptz
        )`,
		`CREATE INDEX IF NOT EXISTS document_versions_document_idx
            ON document_versions (document_id, id DESC)`,
		`CREATE TABLE IF NOT EXISTS document_files (
            id             bigserial   PRIMARY KEY,
            version_id     bigint      REFERENCES document_versions(id) ON DELETE CASCADE,
            slot           text        NOT NULL DEFAULT '',
            position       integer     NOT NULL DEFAULT 0,
            stored_name    text        NOT NULL,
            original_name  text        NOT NULL DEFAULT '',
            content_type   text        NOT NULL,
            size           bigint      NOT NULL,
            has_thumb      boolean     NOT NULL DEFAULT false,
            uploaded_by    bigint      NOT NULL DEFAULT 0,
            created_at     timestamptz NOT NULL DEFAULT now()
        )`,
		`CREATE INDEX IF NOT EXISTS document_files_version_idx
            ON document_files (version_id, position, id)`,
		// What the licence says about the vehicle itself. A trailer has its
		// own plate, chassis number, weight, payload and colour; it stays with its truck, so
		// these are columns on the car rather than a vehicle of its own.
		`ALTER TABLE cars
            ADD COLUMN IF NOT EXISTS chassis_no           text             NOT NULL DEFAULT '',
            ADD COLUMN IF NOT EXISTS engine_no            text             NOT NULL DEFAULT '',
            ADD COLUMN IF NOT EXISTS engine_cc            integer          NOT NULL DEFAULT 0,
            ADD COLUMN IF NOT EXISTS engine_cylinders     integer          NOT NULL DEFAULT 0,
            ADD COLUMN IF NOT EXISTS weight_tons          double precision NOT NULL DEFAULT 0,
            ADD COLUMN IF NOT EXISTS payload_tons         double precision NOT NULL DEFAULT 0,
            ADD COLUMN IF NOT EXISTS color                text             NOT NULL DEFAULT '',
            ADD COLUMN IF NOT EXISTS trailer_no_plate     text             NOT NULL DEFAULT '',
            ADD COLUMN IF NOT EXISTS trailer_chassis_no   text             NOT NULL DEFAULT '',
            ADD COLUMN IF NOT EXISTS trailer_weight_tons  double precision NOT NULL DEFAULT 0,
            ADD COLUMN IF NOT EXISTS trailer_payload_tons double precision NOT NULL DEFAULT 0,
            ADD COLUMN IF NOT EXISTS trailer_color        text             NOT NULL DEFAULT ''`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			return err
		}
	}
	return nil
}
