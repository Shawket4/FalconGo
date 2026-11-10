package Controllers

import (
	"Falcon/Models"
	"Falcon/Observability"
	"Falcon/documents"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// The documents API: the papers and photos of cars and drivers, with their
// history. Everyone signed in reads; managers (permission 3) and above write,
// see hidden versions and open the files of sensitive types. Routes:
// FiberConfig/Routes.go, under /api/documents.

// documentsManager is the permission that writes and sees everything.
const documentsManager = 3

func documentsActor(c *fiber.Ctx) documents.Actor {
	user := RequestUser(c)
	return documents.Actor{ID: user.Id, Name: user.Name}
}

func isDocumentsManager(c *fiber.Ctx) bool {
	return RequestUser(c).Permission >= documentsManager
}

// documentsError answers a failed documents call: a refusal with its code for
// the dashboard to translate, a miss as 404, anything else as a logged 500.
func documentsError(c *fiber.Ctx, what string, err error) error {
	var verr *documents.ValidationError
	switch {
	case errors.As(err, &verr):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": verr.Msg, "code": verr.Code})
	case errors.Is(err, documents.ErrNotFound):
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not found", "code": "not_found"})
	default:
		log.Printf("documents: %s: %v", what, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "could not " + what})
	}
}

func uintParam(c *fiber.Ctx, name string) (uint, bool) {
	n, err := strconv.ParseUint(c.Params(name), 10, 32)
	return uint(n), err == nil && n > 0
}

// documentsOwner reads :ownerKind and :ownerId.
func documentsOwner(c *fiber.Ctx) (string, uint, bool) {
	kind := c.Params("ownerKind")
	id, ok := uintParam(c, "ownerId")
	return kind, id, ok && documents.ValidOwnerKind(kind)
}

// ownerDocuments answers with everything the owner holds, as the caller may
// see it: the response of the read and of every write, so the dashboard
// replaces what it shows instead of patching it.
func ownerDocuments(c *fiber.Ctx, ownerKind string, ownerID uint) error {
	docs, err := documents.ListOwner(Models.DB, ownerKind, ownerID, isDocumentsManager(c))
	if err != nil {
		return documentsError(c, "load the documents", err)
	}
	return c.JSON(fiber.Map{"documents": docs})
}

// GetDocumentTypes is GET /api/documents/types: the catalogue and the upload
// limits.
func GetDocumentTypes(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"types":          documents.Types(),
		"content_types":  documents.ContentTypes(),
		"max_file_bytes": documents.MaxFileBytes,
	})
}

// GetOwnerDocuments is GET /api/documents/:ownerKind/:ownerId.
func GetOwnerDocuments(c *fiber.Ctx) error {
	kind, id, ok := documentsOwner(c)
	if !ok {
		return documentsError(c, "", documents.ErrNotFound)
	}
	return ownerDocuments(c, kind, id)
}

// GetDocumentsOverview is GET /api/documents/overview/:ownerKind: the current
// version of every document of every car, or of every driver.
func GetDocumentsOverview(c *fiber.Ctx) error {
	rows, err := documents.Overview(Models.DB, c.Params("ownerKind"))
	if err != nil {
		return documentsError(c, "load the documents overview", err)
	}
	return c.JSON(fiber.Map{"documents": rows})
}

// CreateDocumentVersion is POST /api/documents/:ownerKind/:ownerId/versions:
// a document's first version, or a renewal.
func CreateDocumentVersion(c *fiber.Ctx) error {
	kind, id, ok := documentsOwner(c)
	if !ok {
		return documentsError(c, "", documents.ErrNotFound)
	}
	var in documents.VersionInput
	if err := json.Unmarshal(c.Body(), &in); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body", "code": "invalid_body"})
	}
	err := Models.DB.Transaction(func(tx *gorm.DB) error {
		_, err := documents.CreateVersion(tx, kind, id, in, documentsActor(c))
		return err
	})
	if err != nil {
		return documentsError(c, "save the document", err)
	}
	return ownerDocuments(c, kind, id)
}

// UpdateDocumentVersion is PUT /api/documents/versions/:id: a correction.
func UpdateDocumentVersion(c *fiber.Ctx) error {
	versionID, ok := uintParam(c, "id")
	if !ok {
		return documentsError(c, "", documents.ErrNotFound)
	}
	var in documents.VersionInput
	if err := json.Unmarshal(c.Body(), &in); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body", "code": "invalid_body"})
	}
	var doc Models.Document
	var dropped []string
	err := Models.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		doc, dropped, err = documents.EditVersion(tx, versionID, in, documentsActor(c))
		return err
	})
	if err != nil {
		return documentsError(c, "save the document", err)
	}
	documents.RemoveStored(dropped)
	return ownerDocuments(c, doc.OwnerKind, doc.OwnerID)
}

// HideDocumentVersion is DELETE /api/documents/versions/:id. Nothing is
// deleted: the version leaves the screen and stays in the managers' history.
func HideDocumentVersion(c *fiber.Ctx) error {
	return setDocumentVersionHidden(c, documents.HideVersion, "remove the document")
}

// RestoreDocumentVersion is POST /api/documents/versions/:id/restore.
func RestoreDocumentVersion(c *fiber.Ctx) error {
	return setDocumentVersionHidden(c, documents.RestoreVersion, "restore the document")
}

func setDocumentVersionHidden(
	c *fiber.Ctx,
	change func(*gorm.DB, uint, documents.Actor) (Models.Document, error),
	what string,
) error {
	versionID, ok := uintParam(c, "id")
	if !ok {
		return documentsError(c, "", documents.ErrNotFound)
	}
	var doc Models.Document
	err := Models.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		doc, err = change(tx, versionID, documentsActor(c))
		return err
	})
	if err != nil {
		return documentsError(c, what, err)
	}
	return ownerDocuments(c, doc.OwnerKind, doc.OwnerID)
}

// UploadDocumentFile is POST /api/documents/files: one file, multipart field
// "file". It is stored staged; the version save that follows attaches it.
func UploadDocumentFile(c *fiber.Ctx) error {
	header, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no file was sent", "code": "empty_file"})
	}
	if header.Size > documents.MaxFileBytes {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"error": "the file is too large", "code": "file_too_large"})
	}
	src, err := header.Open()
	if err != nil {
		return documentsError(c, "read the upload", err)
	}
	defer src.Close()
	data, err := io.ReadAll(io.LimitReader(src, documents.MaxFileBytes+1))
	if err != nil {
		return documentsError(c, "read the upload", err)
	}
	file, err := documents.Stage(Models.DB, header.Filename, data, documentsActor(c))
	if err != nil {
		var verr *documents.ValidationError
		if !errors.As(err, &verr) {
			// A full disk or a missing directory, not a bad file.
			Observability.ReportFailedUpload("document", err)
		}
		return documentsError(c, "store the file", err)
	}
	return c.JSON(file)
}

// GetDocumentFile is GET /api/documents/files/:id, and ?thumb=1 for the
// thumbnail of a picture. A file never changes once stored, so it is cached
// for good, privately.
func GetDocumentFile(c *fiber.Ctx) error {
	fileID, ok := uintParam(c, "id")
	if !ok {
		return documentsError(c, "", documents.ErrNotFound)
	}
	access, err := documents.FindFile(Models.DB, fileID)
	if err != nil {
		return documentsError(c, "load the file", err)
	}
	user := RequestUser(c)
	manager := user.Permission >= documentsManager
	if (access.Sensitive && !manager) || (access.Staged && !manager && access.File.UploadedBy != user.Id) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "you may not open this file", "code": "forbidden"})
	}
	thumb := c.QueryBool("thumb") && access.File.HasThumb
	contentType := access.File.ContentType
	if thumb {
		contentType = "image/jpeg"
	}
	c.Set(fiber.HeaderContentType, contentType)
	c.Set(fiber.HeaderCacheControl, "private, max-age=31536000, immutable")
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	c.Set(fiber.HeaderContentDisposition, "inline")
	return c.SendFile(documents.Path(access.File.StoredName, thumb), false)
}
