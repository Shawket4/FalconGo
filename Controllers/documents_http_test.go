package Controllers

import (
	"Falcon/Models"
	"Falcon/documents"
	"Falcon/testutil"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// documentsApp is the documents routes with the caller taken from a header,
// in place of the session Verify reads. The permission gates on the routes
// themselves are middleware.Verify's and are tested there.
func documentsApp(t *testing.T) *fiber.App {
	t.Helper()
	db := testutil.DocumentsSchema(t)
	prevDB, prevRoot := Models.DB, documents.Root
	Models.DB, documents.Root = db, t.TempDir()
	t.Cleanup(func() { Models.DB, documents.Root = prevDB, prevRoot })

	app := fiber.New(fiber.Config{BodyLimit: documents.BodyLimit})
	app.Use(func(c *fiber.Ctx) error {
		var id uint
		var permission int
		fmt.Sscanf(c.Get("X-Test-User"), "%d:%d", &id, &permission)
		c.Locals("user", &Models.User{Id: id, Name: fmt.Sprintf("user %d", id), Permission: permission})
		return c.Next()
	})
	app.Get("/types", GetDocumentTypes)
	app.Get("/overview/:ownerKind", GetDocumentsOverview)
	app.Post("/files", UploadDocumentFile)
	app.Get("/files/:id", GetDocumentFile)
	app.Put("/versions/:id", UpdateDocumentVersion)
	app.Delete("/versions/:id", HideDocumentVersion)
	app.Post("/versions/:id/restore", RestoreDocumentVersion)
	app.Get("/:ownerKind/:ownerId", GetOwnerDocuments)
	app.Post("/:ownerKind/:ownerId/versions", CreateDocumentVersion)
	return app
}

const (
	asManager      = "1:3"
	asOtherManager = "5:3"
	asViewer       = "2:1"
)

// call sends a JSON request (body nil for none) as a user and returns the
// response with its body read.
func call(t *testing.T, app *fiber.App, user, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	req.Header.Set("X-Test-User", user)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func upload(t *testing.T, app *fiber.App, user, name string, data []byte) (*http.Response, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("file", name)
	part.Write(data)
	w.Close()
	req := httptest.NewRequest("POST", "/files", &buf)
	req.Header.Set(fiber.HeaderContentType, w.FormDataContentType())
	req.Header.Set("X-Test-User", user)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 800, 500)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type ownerDocs struct {
	Documents []documents.DocumentView `json:"documents"`
}

func decodeDocs(t *testing.T, data []byte) []documents.DocumentView {
	t.Helper()
	var out ownerDocs
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return out.Documents
}

func TestDocumentsUploadSaveAndRead(t *testing.T) {
	app := documentsApp(t)
	driver := Models.Driver{Name: "Hassan"}
	Models.DB.Create(&driver)
	base := fmt.Sprintf("/driver/%d", driver.ID)

	resp, data := call(t, app, asViewer, "GET", "/types", nil)
	var catalogue struct {
		Types        []documents.Type `json:"types"`
		ContentTypes []string         `json:"content_types"`
		MaxFileBytes int              `json:"max_file_bytes"`
	}
	json.Unmarshal(data, &catalogue)
	if resp.StatusCode != 200 || len(catalogue.Types) != 12 || len(catalogue.ContentTypes) != 4 || catalogue.MaxFileBytes != documents.MaxFileBytes {
		t.Fatalf("catalogue = %d, %s", resp.StatusCode, data)
	}

	original := testJPEG(t)
	resp, file := upload(t, app, asManager, "licence.jpg", original)
	if resp.StatusCode != 200 || file["content_type"] != "image/jpeg" || file["has_thumb"] != true {
		t.Fatalf("upload = %d %v", resp.StatusCode, file)
	}
	if _, leaked := file["stored_name"]; leaked {
		t.Error("the upload response names the file on disk")
	}
	fileID := uint(file["id"].(float64))
	filePath := fmt.Sprintf("/files/%d", fileID)

	// Staged: its uploader and managers only.
	if resp, _ := call(t, app, asViewer, "GET", filePath, nil); resp.StatusCode != 403 {
		t.Errorf("a viewer opening someone's staged file = %d; want 403", resp.StatusCode)
	}
	if resp, _ := call(t, app, asManager, "GET", filePath, nil); resp.StatusCode != 200 {
		t.Errorf("the uploader opening the staged file = %d; want 200", resp.StatusCode)
	}

	resp, data = call(t, app, asManager, "POST", base+"/versions", map[string]any{
		"type_key": "driving_licence", "expiry_date": "2027-01-31",
		"files": []map[string]any{{"id": fileID, "slot": "front"}},
	})
	docs := decodeDocs(t, data)
	if resp.StatusCode != 200 || len(docs) != 1 || docs[0].TypeKey != "driving_licence" || len(docs[0].Versions[0].Files) != 1 {
		t.Fatalf("save = %d %s", resp.StatusCode, data)
	}
	version := docs[0].Versions[0]
	if version.CreatedByName != "user 1" || *version.ExpiryDate != "2027-01-31" {
		t.Errorf("version = %+v", version)
	}

	// Anyone signed in opens an ordinary document, original and thumbnail.
	resp, body := call(t, app, asViewer, "GET", filePath, nil)
	if resp.StatusCode != 200 || !bytes.Equal(body, original) {
		t.Fatalf("download = %d, %d bytes; want the %d uploaded", resp.StatusCode, len(body), len(original))
	}
	if got := resp.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "private, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	resp, thumb := call(t, app, asViewer, "GET", filePath+"?thumb=1", nil)
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(thumb)); resp.StatusCode != 200 || err != nil || cfg.Width != 480 {
		t.Errorf("thumbnail = %d, %v, %+v; want a 480px JPEG", resp.StatusCode, err, cfg)
	}

	// Refusals carry a code the dashboard translates.
	for name, tc := range map[string]struct {
		method, path string
		body         any
		status       int
		code         string
	}{
		"nothing to save":      {"POST", base + "/versions", map[string]any{"type_key": "drug_test"}, 400, "empty"},
		"an unknown type":      {"POST", base + "/versions", map[string]any{"type_key": "passport", "note": "x"}, 400, "unknown_type"},
		"a body that is not":   {"POST", base + "/versions", "not an object", 400, "invalid_body"},
		"an unknown owner":     {"POST", "/driver/999/versions", map[string]any{"type_key": "drug_test", "note": "x"}, 404, "not_found"},
		"an unknown kind":      {"GET", "/pilot/1", nil, 404, "not_found"},
		"an unknown version":   {"DELETE", "/versions/999", nil, 404, "not_found"},
		"an unknown file":      {"GET", "/files/999", nil, 404, "not_found"},
		"a bad overview kind":  {"GET", "/overview/pilot", nil, 400, "unknown_owner_kind"},
		"a file used twice":    {"POST", base + "/versions", map[string]any{"type_key": "drug_test", "files": []map[string]any{{"id": fileID, "slot": "page"}}}, 400, "file_in_use"},
		"an invalid edit date": {"PUT", fmt.Sprintf("/versions/%d", version.ID), map[string]any{"expiry_date": "31-01-2027"}, 400, "invalid_date"},
	} {
		resp, data := call(t, app, asManager, tc.method, tc.path, tc.body)
		var out map[string]any
		json.Unmarshal(data, &out)
		if resp.StatusCode != tc.status || out["code"] != tc.code {
			t.Errorf("%s = %d %s; want %d %s", name, resp.StatusCode, data, tc.status, tc.code)
		}
	}
	if resp, out := upload(t, app, asManager, "notes.txt", []byte("plain text")); resp.StatusCode != 400 || out["code"] != "unsupported_type" {
		t.Errorf("a text upload = %d %v; want 400 unsupported_type", resp.StatusCode, out)
	}
	if resp, out := upload(t, app, asManager, "huge.pdf", append([]byte("%PDF-1.4\n"), make([]byte, documents.MaxFileBytes)...)); resp.StatusCode != 413 || out["code"] != "file_too_large" {
		t.Errorf("an oversized upload = %d %v; want 413 file_too_large", resp.StatusCode, out)
	}

	// Hidden versions are a manager's to see; hiding answers with the
	// owner's documents, as every write does.
	resp, data = call(t, app, asOtherManager, "DELETE", fmt.Sprintf("/versions/%d", version.ID), nil)
	if docs := decodeDocs(t, data); resp.StatusCode != 200 || len(docs) != 1 || !docs[0].Versions[0].Hidden || docs[0].Versions[0].HiddenByName != "user 5" || docs[0].CurrentVersionID != nil {
		t.Fatalf("hide = %d %s", resp.StatusCode, data)
	}
	if _, data := call(t, app, asViewer, "GET", base, nil); len(decodeDocs(t, data)) != 0 {
		t.Errorf("a viewer still sees the hidden document: %s", data)
	}
	_, data = call(t, app, asViewer, "GET", "/overview/driver", nil)
	var overview struct {
		Documents []documents.OverviewRow `json:"documents"`
	}
	json.Unmarshal(data, &overview)
	if len(overview.Documents) != 0 {
		t.Errorf("the overview lists a hidden document: %s", data)
	}
	resp, data = call(t, app, asManager, "POST", fmt.Sprintf("/versions/%d/restore", version.ID), nil)
	if docs := decodeDocs(t, data); resp.StatusCode != 200 || docs[0].Versions[0].Hidden {
		t.Errorf("restore = %d %s", resp.StatusCode, data)
	}
	_, data = call(t, app, asViewer, "GET", "/overview/driver", nil)
	json.Unmarshal(data, &overview)
	if len(overview.Documents) != 1 || overview.Documents[0].FileCount != 1 || overview.Documents[0].OwnerID != driver.ID {
		t.Errorf("overview = %s", data)
	}
}

// A national ID's pictures are for managers and above.
func TestSensitiveDocumentFilesAreForManagers(t *testing.T) {
	app := documentsApp(t)
	driver := Models.Driver{Name: "Hassan"}
	Models.DB.Create(&driver)

	_, file := upload(t, app, asManager, "id.jpg", testJPEG(t))
	fileID := uint(file["id"].(float64))
	resp, data := call(t, app, asManager, "POST", fmt.Sprintf("/driver/%d/versions", driver.ID), map[string]any{
		"type_key": "national_id", "files": []map[string]any{{"id": fileID, "slot": "front"}},
	})
	if resp.StatusCode != 200 {
		t.Fatalf("save = %d %s", resp.StatusCode, data)
	}
	for _, path := range []string{fmt.Sprintf("/files/%d", fileID), fmt.Sprintf("/files/%d?thumb=1", fileID)} {
		resp, data := call(t, app, asViewer, "GET", path, nil)
		var out map[string]any
		json.Unmarshal(data, &out)
		if resp.StatusCode != 403 || out["code"] != "forbidden" {
			t.Errorf("a viewer opening %s = %d %s; want 403 forbidden", path, resp.StatusCode, data)
		}
		if resp, _ := call(t, app, asManager, "GET", path, nil); resp.StatusCode != 200 {
			t.Errorf("a manager opening %s = %d; want 200", path, resp.StatusCode)
		}
	}
	// The viewer still sees that the document is there, marked restricted.
	_, data = call(t, app, asViewer, "GET", fmt.Sprintf("/driver/%d", driver.ID), nil)
	if docs := decodeDocs(t, data); len(docs) != 1 || !docs[0].Restricted {
		t.Errorf("a viewer's list = %s; want the document, restricted", data)
	}
}
