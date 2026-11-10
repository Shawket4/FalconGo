package documents

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"Falcon/Models"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // decode WebP uploads for their thumbnails
	"gorm.io/gorm"
)

// Root is where the files live. It is under ./uploads, the one directory a
// deploy carries over.
var Root = "./uploads/documents"

const (
	// MaxFileBytes is the largest file taken. The dashboard shrinks pictures
	// before sending them, so this is headroom for PDFs.
	MaxFileBytes = 12 << 20
	// BodyLimit is what the server has to accept for one such file in a
	// multipart body.
	BodyLimit = MaxFileBytes + (1 << 20)

	thumbEdge = 480
	// Past this many pixels a picture is stored without a thumbnail rather
	// than decoded: a decode costs four bytes a pixel.
	maxThumbPixels = 50_000_000
	// A staged file nobody saved is removed after this long.
	stagedTTL = 24 * time.Hour
)

// accepted is the content types taken, by what the bytes are (never by the
// name or the type the browser claims), and the extension each is stored with.
var accepted = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/webp":      ".webp",
	"application/pdf": ".pdf",
}

// ContentTypes is the accepted content types, for the catalogue response.
func ContentTypes() []string {
	return []string{"image/jpeg", "image/png", "image/webp", "application/pdf"}
}

func isImage(contentType string) bool { return strings.HasPrefix(contentType, "image/") }

// Stage stores an uploaded file that no version holds yet. The save that
// follows attaches it by id; a staged file nobody saves is purged.
func Stage(db *gorm.DB, originalName string, data []byte, by Actor) (Models.DocumentFile, error) {
	purgeStagedSometimes(db)
	file, err := store(originalName, data)
	if err != nil {
		return file, err
	}
	file.UploadedBy = by.ID
	if err := db.Create(&file).Error; err != nil {
		RemoveStored([]string{file.StoredName})
		return file, err
	}
	return file, nil
}

// store writes data under Root, with a thumbnail when it is a picture, and
// returns the row describing it (not yet inserted).
func store(originalName string, data []byte) (Models.DocumentFile, error) {
	var file Models.DocumentFile
	if len(data) == 0 {
		return file, refuse("empty_file", "the file is empty")
	}
	if len(data) > MaxFileBytes {
		return file, refuse("file_too_large", "the file is too large")
	}
	contentType := strings.SplitN(http.DetectContentType(data), ";", 2)[0]
	ext, ok := accepted[contentType]
	if !ok {
		return file, refuse("unsupported_type", "only JPEG, PNG, WebP and PDF files are accepted")
	}

	var thumb image.Image
	if isImage(contentType) {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return file, refuse("unreadable_image", "the picture could not be read")
		}
		if cfg.Width*cfg.Height <= maxThumbPixels {
			img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
			if err != nil {
				return file, refuse("unreadable_image", "the picture could not be read")
			}
			thumb = imaging.Fit(img, thumbEdge, thumbEdge, imaging.Lanczos)
		}
	}

	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return file, err
	}
	// Stored under a random name: the original name is often a person's, and
	// is kept only in the row.
	storedName := time.Now().UTC().Format("2006/01") + "/" + hex.EncodeToString(random) + ext
	path := Path(storedName, false)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return file, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return file, err
	}
	if thumb != nil {
		if err := imaging.Save(thumb, Path(storedName, true), imaging.JPEGQuality(80)); err != nil {
			os.Remove(path)
			return file, err
		}
	}

	name := filepath.Base(strings.ReplaceAll(originalName, "\\", "/"))
	if len(name) > 200 {
		name = name[len(name)-200:]
	}
	return Models.DocumentFile{
		StoredName:   storedName,
		OriginalName: strings.ToValidUTF8(name, ""),
		ContentType:  contentType,
		Size:         int64(len(data)),
		HasThumb:     thumb != nil,
	}, nil
}

// thumbSuffix names a file's thumbnail beside it. Stored names end in the
// file's own extension, so this cannot collide with one.
const thumbSuffix = ".thumb.jpg"

// Path is where a stored file, or its thumbnail, is on disk.
func Path(storedName string, thumb bool) string {
	p := filepath.Join(Root, filepath.FromSlash(storedName))
	if thumb {
		p += thumbSuffix
	}
	return p
}

// RemoveStored deletes files and their thumbnails from disk. Call it after
// the transaction that dropped their rows has committed.
func RemoveStored(storedNames []string) {
	for _, name := range storedNames {
		os.Remove(Path(name, false))
		os.Remove(Path(name, true))
	}
}

// FileAccess is a file and what decides who may open it.
type FileAccess struct {
	File Models.DocumentFile
	// Its document's type is sensitive: managers and above only.
	Sensitive bool
	// Not attached to a version yet: its uploader and managers only.
	Staged bool
}

// FindFile loads a file for serving.
func FindFile(db *gorm.DB, fileID uint) (FileAccess, error) {
	var access FileAccess
	if err := db.First(&access.File, fileID).Error; err != nil {
		return access, notFound(err)
	}
	if access.File.VersionID == nil {
		access.Staged = true
		return access, nil
	}
	var typeKey string
	if err := db.Raw(`
        SELECT d.type_key FROM documents d
        JOIN document_versions v ON v.document_id = d.id
        WHERE v.id = ?`, *access.File.VersionID).Scan(&typeKey).Error; err != nil {
		return access, err
	}
	typ, _ := TypeByKey(typeKey)
	access.Sensitive = typ.Sensitive
	return access, nil
}

// PurgeStaged removes the staged files older than the TTL: uploads whose
// form was closed without saving. It returns how many went.
func PurgeStaged(db *gorm.DB) (int, error) {
	var stale []Models.DocumentFile
	if err := db.Where("version_id IS NULL AND created_at < ?", time.Now().Add(-stagedTTL)).
		Find(&stale).Error; err != nil {
		return 0, err
	}
	for _, f := range stale {
		if err := db.Delete(&Models.DocumentFile{}, f.ID).Error; err != nil {
			return 0, err
		}
		RemoveStored([]string{f.StoredName})
	}
	return len(stale), nil
}

var lastPurge atomic.Int64

// purgeStagedSometimes runs PurgeStaged at most once an hour, off the back of
// an upload, so the store cleans itself without a worker of its own.
func purgeStagedSometimes(db *gorm.DB) {
	now := time.Now().Unix()
	last := lastPurge.Load()
	if now-last < 3600 || !lastPurge.CompareAndSwap(last, now) {
		return
	}
	go func() {
		defer func() { _ = recover() }()
		if _, err := PurgeStaged(db); err != nil {
			fmt.Fprintf(os.Stderr, "documents: purge staged files: %v\n", err)
		}
	}()
}
