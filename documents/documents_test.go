package documents

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"Falcon/Models"
	"Falcon/testutil"

	"gorm.io/gorm"
)

var manager = Actor{ID: 9, Name: "Mona"}

// setup is a fresh schema with the file store in a temporary directory.
func setup(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.DocumentsSchema(t)
	prevRoot, prevLegacy := Root, legacyUploads
	Root, legacyUploads = t.TempDir(), t.TempDir()
	t.Cleanup(func() { Root, legacyUploads = prevRoot, prevLegacy })
	return db
}

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 900, 600))
	for x := 0; x < 900; x++ {
		for y := 0; y < 600; y++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var pdfBytes = []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n")

func stage(t *testing.T, db *gorm.DB, name string, data []byte) uint {
	t.Helper()
	file, err := Stage(db, name, data, manager)
	if err != nil {
		t.Fatalf("stage %s: %v", name, err)
	}
	return file.ID
}

func newCar(t *testing.T, db *gorm.DB, carType string) uint {
	t.Helper()
	car := Models.Car{CarNoPlate: "ABC 123", CarType: carType}
	if err := db.Create(&car).Error; err != nil {
		t.Fatal(err)
	}
	return car.ID
}

func newDriver(t *testing.T, db *gorm.DB) uint {
	t.Helper()
	driver := Models.Driver{Name: "Hassan"}
	if err := db.Create(&driver).Error; err != nil {
		t.Fatal(err)
	}
	return driver.ID
}

func create(t *testing.T, db *gorm.DB, kind string, owner uint, in VersionInput) (Models.Document, error) {
	t.Helper()
	var doc Models.Document
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		doc, err = CreateVersion(tx, kind, owner, in, manager)
		return err
	})
	return doc, err
}

func mustCreate(t *testing.T, db *gorm.DB, kind string, owner uint, in VersionInput) Models.Document {
	t.Helper()
	doc, err := create(t, db, kind, owner, in)
	if err != nil {
		t.Fatalf("create %s: %v", in.TypeKey, err)
	}
	return doc
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != code {
		t.Fatalf("error = %v; want a refusal with code %q", err, code)
	}
}

func column(t *testing.T, db *gorm.DB, table, col string, id uint) string {
	t.Helper()
	var v string
	if err := db.Raw("SELECT COALESCE("+col+", '') FROM "+table+" WHERE id = ?", id).Scan(&v).Error; err != nil {
		t.Fatal(err)
	}
	return v
}

func only(t *testing.T, db *gorm.DB, kind string, owner uint, asManager bool) DocumentView {
	t.Helper()
	docs, err := ListOwner(db, kind, owner, asManager)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("documents = %d; want 1", len(docs))
	}
	return docs[0]
}

func refs(pairs ...any) *[]FileRef {
	out := []FileRef{}
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, FileRef{ID: pairs[i].(uint), Slot: pairs[i+1].(string)})
	}
	return &out
}

func TestCatalogueIsWellFormedAndItsColumnsExist(t *testing.T) {
	db := setup(t)
	seen := map[string]bool{}
	for _, typ := range Types() {
		if seen[typ.Key] {
			t.Errorf("%s: listed twice", typ.Key)
		}
		seen[typ.Key] = true
		if !ValidOwnerKind(typ.OwnerKind) {
			t.Errorf("%s: unknown owner kind %q", typ.Key, typ.OwnerKind)
		}
		if len(typ.Slots) == 0 {
			t.Errorf("%s: takes no files", typ.Key)
		}
		columns := []string{}
		if typ.mirrorColumn != "" {
			columns = append(columns, typ.mirrorColumn)
		}
		for _, lf := range typ.legacy {
			columns = append(columns, lf.column)
			if _, ok := typ.slot(lf.slot); !ok {
				t.Errorf("%s: legacy column %s names slot %q, which the type does not have", typ.Key, lf.column, lf.slot)
			}
		}
		for _, col := range columns {
			var n int64
			db.Raw(`SELECT count(*) FROM information_schema.columns
                    WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
				owners[typ.OwnerKind].table, col).Scan(&n)
			if n != 1 {
				t.Errorf("%s: %s.%s does not exist", typ.Key, owners[typ.OwnerKind].table, col)
			}
		}
	}
	if len(TypesFor(OwnerDriver)) != 8 || len(TypesFor(OwnerCar)) != 4 {
		t.Errorf("catalogue = %d driver and %d car types; want 8 and 4",
			len(TypesFor(OwnerDriver)), len(TypesFor(OwnerCar)))
	}
}

// The whole life of one document: first version, renewal, correction, a
// version hidden and brought back; the old expiry column follows throughout.
func TestVersionLifecycleAndTheMirroredColumn(t *testing.T) {
	db := setup(t)
	car := newCar(t, db, "Trailer")
	mirrored := func() string { return column(t, db, "cars", "license_expiration_date", car) }

	front, back := stage(t, db, "front.jpg", jpegBytes(t)), stage(t, db, "back.jpg", jpegBytes(t))
	doc := mustCreate(t, db, OwnerCar, car, VersionInput{
		TypeKey: "vehicle_licence", IssueDate: "2025-01-10", ExpiryDate: "2026-01-10",
		Files: refs(front, "front", back, "back"),
	})
	if got := mirrored(); got != "2026-01-10" {
		t.Fatalf("mirrored column = %q; want 2026-01-10", got)
	}
	first := only(t, db, OwnerCar, car, false)
	if len(first.Versions) != 1 || len(first.Versions[0].Files) != 2 || first.Versions[0].CreatedByName != "Mona" {
		t.Fatalf("first version = %+v", first.Versions)
	}
	if f := first.Versions[0].Files; f[0].Slot != "front" || f[1].Slot != "back" || !f[0].HasThumb {
		t.Errorf("files = %+v; want front then back, with thumbnails", f)
	}

	// A renewal is a second version of the same document.
	renewed := mustCreate(t, db, OwnerCar, car, VersionInput{DocumentID: doc.ID, ExpiryDate: "2027-01-10"})
	if renewed.ID != doc.ID {
		t.Fatalf("renewal made document %d; want %d", renewed.ID, doc.ID)
	}
	view := only(t, db, OwnerCar, car, false)
	if len(view.Versions) != 2 || *view.CurrentVersionID != view.Versions[0].ID || *view.Versions[0].ExpiryDate != "2027-01-10" {
		t.Fatalf("after renewal = %+v", view)
	}
	if got := mirrored(); got != "2027-01-10" {
		t.Errorf("mirrored column = %q; want the renewal's 2027-01-10", got)
	}
	// Naming the type again is also a renewal: an owner holds one licence.
	if again := mustCreate(t, db, OwnerCar, car, VersionInput{TypeKey: "vehicle_licence", Note: "third"}); again.ID != doc.ID {
		t.Errorf("a second vehicle licence became document %d; want a version of %d", again.ID, doc.ID)
	}
	if got := mirrored(); got != "" {
		t.Errorf("mirrored column = %q; the current version has no expiry date", got)
	}

	// A correction edits in place and leaves the files alone when none are sent.
	current := *only(t, db, OwnerCar, car, false).CurrentVersionID
	err := db.Transaction(func(tx *gorm.DB) error {
		_, _, err := EditVersion(tx, current, VersionInput{ExpiryDate: "2028-02-02", Note: "third"}, Actor{Name: "Omar"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	view = only(t, db, OwnerCar, car, false)
	if len(view.Versions) != 3 || view.Versions[0].UpdatedByName != "Omar" || mirrored() != "2028-02-02" {
		t.Fatalf("after edit = %+v, column %q", view.Versions[0], mirrored())
	}

	// Hiding the current version makes the one before it current.
	if err := db.Transaction(func(tx *gorm.DB) error { _, err := HideVersion(tx, current, manager); return err }); err != nil {
		t.Fatal(err)
	}
	if got := mirrored(); got != "2027-01-10" {
		t.Errorf("mirrored column = %q; want the previous version's 2027-01-10", got)
	}
	if v := only(t, db, OwnerCar, car, false); len(v.Versions) != 2 || *v.CurrentVersionID == current {
		t.Errorf("a viewer still sees the hidden version: %+v", v)
	}
	if v := only(t, db, OwnerCar, car, true); len(v.Versions) != 3 || !v.Versions[0].Hidden || v.Versions[0].HiddenByName != "Mona" || *v.CurrentVersionID != v.Versions[1].ID {
		t.Errorf("a manager's history = %+v", v)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { _, err := RestoreVersion(tx, current, manager); return err }); err != nil {
		t.Fatal(err)
	}
	if got := mirrored(); got != "2028-02-02" {
		t.Errorf("mirrored column = %q after restore; want 2028-02-02", got)
	}

	rows, err := Overview(db, OwnerCar)
	if err != nil || len(rows) != 1 || rows[0].VersionID != current || rows[0].OwnerID != car || *rows[0].ExpiryDate != "2028-02-02" {
		t.Errorf("overview = %+v, %v", rows, err)
	}
}

func TestEditReplacesFilesAndReportsTheDropped(t *testing.T) {
	db := setup(t)
	driver := newDriver(t, db)
	a, b := stage(t, db, "a.jpg", jpegBytes(t)), stage(t, db, "b.pdf", pdfBytes)
	mustCreate(t, db, OwnerDriver, driver, VersionInput{TypeKey: "criminal_record", Files: refs(a, "page", b, "page")})
	version := only(t, db, OwnerDriver, driver, false).Versions[0]
	if version.Files[1].ContentType != "application/pdf" || version.Files[1].HasThumb {
		t.Fatalf("pdf stored as %+v", version.Files[1])
	}

	c := stage(t, db, "c.jpg", jpegBytes(t))
	var dropped []string
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		_, dropped, err = EditVersion(tx, version.ID, VersionInput{Files: refs(c, "page", b, "page")}, manager)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dropped) != 1 {
		t.Fatalf("dropped = %v; want the one file left out", dropped)
	}
	if _, err := os.Stat(Path(dropped[0], false)); err != nil {
		t.Errorf("the dropped file was removed before the caller did it: %v", err)
	}
	RemoveStored(dropped)
	if _, err := os.Stat(Path(dropped[0], true)); !os.IsNotExist(err) {
		t.Errorf("thumbnail still on disk: %v", err)
	}
	after := only(t, db, OwnerDriver, driver, false).Versions[0].Files
	if len(after) != 2 || after[0].ID != c || after[1].ID != b {
		t.Errorf("files after edit = %+v; want c then b", after)
	}
}

func TestNothingIsRequiredButSomethingIs(t *testing.T) {
	db := setup(t)
	driver, tanker, tractor := newDriver(t, db), newCar(t, db, "Trailer"), newCar(t, db, "No Trailer")

	_, err := create(t, db, OwnerDriver, driver, VersionInput{TypeKey: "drug_test"})
	wantCode(t, err, "empty")
	for name, in := range map[string]VersionInput{
		"a date alone": {TypeKey: "drug_test", ExpiryDate: "2026-05-01"},
		"a note alone": {TypeKey: "safety_certificate", Note: "at the office"},
		"a file alone": {TypeKey: "trust_receipt", Files: refs(stage(t, db, "r.pdf", pdfBytes), "page")},
	} {
		if _, err := create(t, db, OwnerDriver, driver, in); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	photo, pdf := stage(t, db, "p.jpg", jpegBytes(t)), stage(t, db, "p.pdf", pdfBytes)
	used := stage(t, db, "used.jpg", jpegBytes(t))
	mustCreate(t, db, OwnerCar, tanker, VersionInput{TypeKey: "vehicle_photos", Files: refs(used, "photo")})
	for name, tc := range map[string]struct {
		kind  string
		owner uint
		in    VersionInput
		code  string
	}{
		"a date that is not one":      {OwnerDriver, driver, VersionInput{TypeKey: "drug_test", ExpiryDate: "01/05/2026"}, "invalid_date"},
		"expiry before issue":         {OwnerDriver, driver, VersionInput{TypeKey: "drug_test", IssueDate: "2026-05-02", ExpiryDate: "2026-05-01"}, "expiry_before_issue"},
		"a car's type on a driver":    {OwnerDriver, driver, VersionInput{TypeKey: "vehicle_licence", Note: "x"}, "unknown_type"},
		"a type that does not exist":  {OwnerCar, tanker, VersionInput{TypeKey: "passport", Note: "x"}, "unknown_type"},
		"a tank licence on a tractor": {OwnerCar, tractor, VersionInput{TypeKey: "tank_licence", Note: "x"}, "not_applicable"},
		"a slot the type lacks":       {OwnerDriver, driver, VersionInput{TypeKey: "driver_photo", Files: refs(photo, "front")}, "unknown_slot"},
		"two portraits":               {OwnerDriver, driver, VersionInput{TypeKey: "driver_photo", Files: refs(photo, "photo", pdf, "photo")}, "too_many_files"},
		"a pdf as a photo":            {OwnerDriver, driver, VersionInput{TypeKey: "driver_photo", Files: refs(pdf, "photo")}, "images_only"},
		"the same file twice":         {OwnerDriver, driver, VersionInput{TypeKey: "criminal_record", Files: refs(photo, "page", photo, "page")}, "duplicate_file"},
		"another document's file":     {OwnerDriver, driver, VersionInput{TypeKey: "criminal_record", Files: refs(used, "page")}, "file_in_use"},
	} {
		_, err := create(t, db, tc.kind, tc.owner, tc.in)
		t.Run(name, func(t *testing.T) { wantCode(t, err, tc.code) })
	}
	if _, err := create(t, db, OwnerCar, 9999, VersionInput{TypeKey: "vehicle_photos", Note: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a car that does not exist: %v; want ErrNotFound", err)
	}
	// A refused save leaves nothing behind.
	var n int64
	db.Model(&Models.Document{}).Where("type_key IN ?", []string{"driver_photo", "criminal_record", "tank_licence"}).Count(&n)
	if n != 0 {
		t.Errorf("%d documents were left by refused saves", n)
	}
	if _, err := create(t, db, OwnerCar, tanker, VersionInput{TypeKey: "tank_licence", Note: "x"}); err != nil {
		t.Errorf("a tank licence on a tanker: %v", err)
	}
}

func TestLabelledDocumentsAreSeparateAndTheirNamesUnique(t *testing.T) {
	db := setup(t)
	driver := newDriver(t, db)
	shell := mustCreate(t, db, OwnerDriver, driver, VersionInput{TypeKey: "site_access_pass", Label: "  Shell   Egypt ", ExpiryDate: "2026-03-01"})
	total := mustCreate(t, db, OwnerDriver, driver, VersionInput{TypeKey: "site_access_pass", Label: "Total", ExpiryDate: "2026-04-01"})
	if shell.ID == total.ID || shell.Label != "Shell Egypt" {
		t.Fatalf("passes = %+v and %+v; want two documents, labels tidied", shell, total)
	}
	_, err := create(t, db, OwnerDriver, driver, VersionInput{TypeKey: "site_access_pass", Label: "Total", Note: "again"})
	wantCode(t, err, "label_taken")
	// A renewal names the document, so its label is not a clash.
	mustCreate(t, db, OwnerDriver, driver, VersionInput{DocumentID: total.ID, ExpiryDate: "2027-04-01"})

	docs, _ := ListOwner(db, OwnerDriver, driver, false)
	if len(docs) != 2 || docs[0].Label != "Shell Egypt" || len(docs[1].Versions) != 2 {
		t.Fatalf("passes = %+v", docs)
	}
	shellVersion := docs[0].Versions[0].ID
	err = db.Transaction(func(tx *gorm.DB) error {
		_, _, err := EditVersion(tx, shellVersion, VersionInput{Label: "Total", ExpiryDate: "2026-03-01"}, manager)
		return err
	})
	wantCode(t, err, "label_taken")
	err = db.Transaction(func(tx *gorm.DB) error {
		_, _, err := EditVersion(tx, shellVersion, VersionInput{Label: "Shell", ExpiryDate: "2026-03-01"}, manager)
		return err
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}

	// Hiding everything under a label frees it.
	if err := db.Transaction(func(tx *gorm.DB) error { _, err := HideVersion(tx, shellVersion, manager); return err }); err != nil {
		t.Fatal(err)
	}
	if docs, _ := ListOwner(db, OwnerDriver, driver, false); len(docs) != 1 {
		t.Errorf("a viewer sees %d passes; want only the one with a visible version", len(docs))
	}
	if again := mustCreate(t, db, OwnerDriver, driver, VersionInput{TypeKey: "site_access_pass", Label: "Shell", Note: "new card"}); again.ID != shell.ID {
		t.Errorf("the freed label made document %d; want %d reused", again.ID, shell.ID)
	}
	// Another driver's document is not this one's to renew.
	other := newDriver(t, db)
	if _, err := create(t, db, OwnerDriver, other, VersionInput{DocumentID: total.ID, Note: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("renewing another driver's pass: %v; want ErrNotFound", err)
	}
}

func TestSensitiveAndStagedFiles(t *testing.T) {
	db := setup(t)
	driver := newDriver(t, db)
	front := stage(t, db, "C:\\Users\\me\\id front.jpg", jpegBytes(t))

	access, err := FindFile(db, front)
	if err != nil || !access.Staged || access.Sensitive || access.File.UploadedBy != manager.ID || access.File.OriginalName != "id front.jpg" {
		t.Fatalf("staged file = %+v, %v", access, err)
	}
	mustCreate(t, db, OwnerDriver, driver, VersionInput{TypeKey: "national_id", Files: refs(front, "front")})
	if access, _ = FindFile(db, front); access.Staged || !access.Sensitive {
		t.Errorf("an attached national ID = %+v; want sensitive, not staged", access)
	}
	if v := only(t, db, OwnerDriver, driver, false); !v.Restricted {
		t.Error("a viewer's national ID is not marked restricted")
	}
	if v := only(t, db, OwnerDriver, driver, true); v.Restricted {
		t.Error("a manager's national ID is marked restricted")
	}
	if _, err := FindFile(db, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing file: %v", err)
	}

	for name, data := range map[string][]byte{"text": []byte("hello there"), "html": []byte("<html><script>1</script></html>")} {
		_, err := Stage(db, name+".jpg", data, manager)
		wantCode(t, err, "unsupported_type")
	}
	_, err = Stage(db, "empty.jpg", nil, manager)
	wantCode(t, err, "empty_file")
	_, err = Stage(db, "broken.jpg", jpegBytes(t)[:200], manager)
	wantCode(t, err, "unreadable_image")

	// A staged file nobody saved goes after a day; an attached one never does.
	stale := stage(t, db, "stale.jpg", jpegBytes(t))
	fresh := stage(t, db, "fresh.jpg", jpegBytes(t))
	old := time.Now().Add(-25 * time.Hour)
	db.Model(&Models.DocumentFile{}).Where("id IN ?", []uint{stale, front}).Update("created_at", old)
	staleAccess, _ := FindFile(db, stale)
	if n, err := PurgeStaged(db); err != nil || n != 1 {
		t.Fatalf("purged %d, %v; want 1", n, err)
	}
	if _, err := os.Stat(Path(staleAccess.File.StoredName, false)); !os.IsNotExist(err) {
		t.Errorf("the purged file is still on disk: %v", err)
	}
	for _, id := range []uint{fresh, front} {
		if _, err := FindFile(db, id); err != nil {
			t.Errorf("file %d was purged: %v", id, err)
		}
	}
}

// The profile endpoints still take the old expiry dates; what they write is
// taken into the documents.
func TestAdoptLegacyDates(t *testing.T) {
	db := setup(t)
	driver := newDriver(t, db)
	write := func(value string) {
		t.Helper()
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("UPDATE drivers SET drug_test_expiration_date = ? WHERE id = ?", value, driver).Error; err != nil {
				return err
			}
			return AdoptLegacyDates(tx, OwnerDriver, driver, Actor{Name: "Advisor"})
		})
		if err != nil {
			t.Fatalf("adopt %q: %v", value, err)
		}
	}
	stored := func() string { return column(t, db, "drivers", "drug_test_expiration_date", driver) }

	write("")
	if docs, _ := ListOwner(db, OwnerDriver, driver, true); len(docs) != 0 {
		t.Fatalf("no date made %d documents", len(docs))
	}
	write("2026-06-30T00:00:00Z")
	v := only(t, db, OwnerDriver, driver, false)
	if v.TypeKey != "drug_test" || v.Versions[0].Source != "profile" || *v.Versions[0].ExpiryDate != "2026-06-30" || stored() != "2026-06-30" {
		t.Fatalf("adopted = %+v, column %q", v, stored())
	}
	// A changed date corrects the current version; it is not a renewal.
	write("2026-07-15")
	v = only(t, db, OwnerDriver, driver, false)
	if len(v.Versions) != 1 || *v.Versions[0].ExpiryDate != "2026-07-15" || v.Versions[0].UpdatedByName != "Advisor" {
		t.Fatalf("after a changed date = %+v", v.Versions)
	}
	// The same date again changes nothing.
	write("2026-07-15")
	if v = only(t, db, OwnerDriver, driver, false); len(v.Versions) != 1 {
		t.Errorf("an unchanged date made a version")
	}
	// Old text that was never a date is left alone, and blocks nothing.
	write("soon")
	if v = only(t, db, OwnerDriver, driver, false); *v.Versions[0].ExpiryDate != "2026-07-15" || stored() != "soon" {
		t.Errorf("garbage text changed the document or was rewritten: %+v, %q", v.Versions[0], stored())
	}
	write("")
	if v = only(t, db, OwnerDriver, driver, false); v.Versions[0].ExpiryDate != nil || stored() != "" {
		t.Errorf("an emptied date left %v, column %q", v.Versions[0].ExpiryDate, stored())
	}
	if CheckLegacyDate("soon") == nil || CheckLegacyDate("") != nil || CheckLegacyDate("2026-01-02") != nil {
		t.Error("CheckLegacyDate: want only non-dates refused")
	}
}

func TestBackfillImportsTheOldColumnsOnce(t *testing.T) {
	db := setup(t)
	dir := filepath.Join(legacyUploads, "car-licenses")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "170_front.jpg"), jpegBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
	withBoth := Models.Car{CarNoPlate: "A", LicenseExpirationDate: "2026-09-01", CarLicenseImageName: "170_front.jpg",
		CarLicenseImageNameBack: "gone.jpg", CalibrationExpirationDate: "2026-10-01"}
	garbage := Models.Car{CarNoPlate: "B", LicenseExpirationDate: "n/a", CarLicenseImageName: "../../etc/passwd"}
	recorded := Models.Car{CarNoPlate: "C", LicenseExpirationDate: "2020-01-01"}
	driver := Models.Driver{Name: "D", SafetyLicenseExpirationDate: "2026-12-31 00:00:00"}
	for _, row := range []any{&withBoth, &garbage, &recorded, &driver} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// C already has a licence document: the import never overrides it.
	mustCreate(t, db, OwnerCar, recorded.ID, VersionInput{TypeKey: "vehicle_licence", ExpiryDate: "2030-01-01"})

	created, err := Backfill(db)
	if err != nil || created != 3 {
		t.Fatalf("backfill created %d, %v; want 3 (A's licence and calibration, D's safety certificate)", created, err)
	}
	docs, _ := ListOwner(db, OwnerCar, withBoth.ID, false)
	if len(docs) != 2 || docs[0].TypeKey != "vehicle_licence" || docs[1].TypeKey != "calibration_certificate" {
		t.Fatalf("A's documents = %+v", docs)
	}
	licence := docs[0].Versions[0]
	if licence.Source != "legacy" || *licence.ExpiryDate != "2026-09-01" || len(licence.Files) != 1 ||
		licence.Files[0].Slot != "front" || !licence.Files[0].HasThumb {
		t.Errorf("A's licence = %+v", licence)
	}
	if docs, _ := ListOwner(db, OwnerCar, garbage.ID, true); len(docs) != 0 {
		t.Errorf("B (no date, no readable image) got %d documents", len(docs))
	}
	if v := only(t, db, OwnerCar, recorded.ID, false); len(v.Versions) != 1 || *v.Versions[0].ExpiryDate != "2030-01-01" {
		t.Errorf("C's recorded licence was touched: %+v", v.Versions)
	}
	if v := only(t, db, OwnerDriver, driver.ID, false); *v.Versions[0].ExpiryDate != "2026-12-31" {
		t.Errorf("D's safety certificate = %+v", v.Versions[0])
	}
	if again, err := Backfill(db); err != nil || again != 0 {
		t.Errorf("a second backfill created %d, %v; want 0", again, err)
	}
}
