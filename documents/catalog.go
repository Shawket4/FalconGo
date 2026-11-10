// Package documents is the papers and photos a car or a driver carries: one
// model, one set of operations and one file store for every owner and every
// kind of paper. See Models/documents.go for the tables.
//
// Adding a kind of paper is one entry in the catalogue below (and its name in
// the dashboard's translation files: this package only ever sends keys).
// Adding a kind of owner is one entry in owners.
package documents

// Owner kinds.
const (
	OwnerCar    = "car"
	OwnerDriver = "driver"
)

// Layouts: how a type's files are arranged, for the screen that shows them.
const (
	LayoutSides = "sides" // a front and a back
	LayoutPages = "pages" // one or more pages, or a PDF
	LayoutPhoto = "photo" // a single picture
	LayoutAlbum = "album" // a set of pictures
)

// Slot is one place a file goes in a version, and how many files it takes.
type Slot struct {
	Key string `json:"key"`
	Max int    `json:"max"`
}

// Type is one kind of paper. Every type has an issue date and an expiry date
// on each version, and nothing on any of them is required.
type Type struct {
	Key       string `json:"key"`
	OwnerKind string `json:"owner_kind"`
	Layout    string `json:"layout"`
	Slots     []Slot `json:"slots"`
	// An owner holds several, told apart by a label (a site pass per company).
	Multiple bool `json:"multiple"`
	// Files of a sensitive type are served to managers and above only.
	Sensitive bool `json:"sensitive"`
	// Pictures only; every other type also takes a PDF.
	ImagesOnly bool `json:"images_only"`
	// The car types this paper applies to; empty means every car.
	CarTypes []string `json:"car_types,omitempty"`

	// The owner-table column that mirrors the current version's expiry date.
	// These columns predate this package and are still what the fleet
	// dashboard's SQL, the maintenance cache and the Excel export read, so
	// they are kept as a projection: this package is the only thing that
	// writes them (mirror in legacy.go).
	mirrorColumn string
	// Where this type's images lived before this package, for the import.
	legacy []legacyFile
}

// legacyFile is one old image column and the uploads directory it named.
type legacyFile struct {
	slot, column, dir string
}

var (
	sides = []Slot{{Key: "front", Max: 1}, {Key: "back", Max: 1}}
	pages = []Slot{{Key: "page", Max: 8}}
)

// catalogue is every type, in the order the dashboard lists them.
var catalogue = []Type{
	// ── Drivers ─────────────────────────────────────────────────────────
	{
		Key: "national_id", OwnerKind: OwnerDriver, Layout: LayoutSides, Slots: sides,
		Sensitive:    true,
		mirrorColumn: "id_license_expiration_date",
		legacy: []legacyFile{
			{"front", "id_license_image_name", "driver-id-licenses"},
			{"back", "id_license_image_name_back", "driver-id-licenses-back"},
		},
	},
	{
		Key: "driving_licence", OwnerKind: OwnerDriver, Layout: LayoutSides, Slots: sides,
		mirrorColumn: "driver_license_expiration_date",
		legacy: []legacyFile{
			{"front", "driver_license_image_name", "driver-licenses"},
			{"back", "driver_license_image_name_back", "driver-licenses-back"},
		},
	},
	{
		Key: "safety_certificate", OwnerKind: OwnerDriver, Layout: LayoutPages, Slots: pages,
		mirrorColumn: "safety_license_expiration_date",
		legacy:       []legacyFile{{"page", "safety_license_image_name", "safety-licenses"}},
	},
	{
		Key: "drug_test", OwnerKind: OwnerDriver, Layout: LayoutPages, Slots: pages,
		mirrorColumn: "drug_test_expiration_date",
		legacy:       []legacyFile{{"page", "drug_test_image_name", "drug-tests"}},
	},
	{Key: "criminal_record", OwnerKind: OwnerDriver, Layout: LayoutPages, Slots: pages},
	{Key: "trust_receipt", OwnerKind: OwnerDriver, Layout: LayoutPages, Slots: pages},
	{
		Key: "site_access_pass", OwnerKind: OwnerDriver, Layout: LayoutSides, Slots: sides,
		Multiple: true,
	},
	{
		Key: "driver_photo", OwnerKind: OwnerDriver, Layout: LayoutPhoto,
		Slots: []Slot{{Key: "photo", Max: 1}}, ImagesOnly: true,
	},

	// ── Cars ────────────────────────────────────────────────────────────
	{
		Key: "vehicle_licence", OwnerKind: OwnerCar, Layout: LayoutSides, Slots: sides,
		mirrorColumn: "license_expiration_date",
		legacy: []legacyFile{
			{"front", "car_license_image_name", "car-licenses"},
			{"back", "car_license_image_name_back", "car-licenses-back"},
		},
	},
	{
		Key: "calibration_certificate", OwnerKind: OwnerCar, Layout: LayoutSides, Slots: sides,
		mirrorColumn: "calibration_expiration_date",
		legacy: []legacyFile{
			{"front", "calibration_license_image_name", "calibration-licenses"},
			{"back", "calibration_license_image_name_back", "calibration-licenses-back"},
		},
	},
	{
		// Only tankers carry one: the rule the cars screen and the Excel
		// export have always applied (services.tankLicenseApplies).
		Key: "tank_licence", OwnerKind: OwnerCar, Layout: LayoutSides, Slots: sides,
		CarTypes:     []string{"Trailer", "Truck"},
		mirrorColumn: "tank_license_expiration_date",
		legacy: []legacyFile{
			{"front", "tank_license_image_name", "tank-licenses"},
			{"back", "tank_license_image_name_back", "tank-licenses-back"},
		},
	},
	{
		Key: "vehicle_photos", OwnerKind: OwnerCar, Layout: LayoutAlbum,
		Slots: []Slot{{Key: "photo", Max: 30}}, ImagesOnly: true,
	},
}

// owner is the table behind an owner kind.
type owner struct {
	table string
}

var owners = map[string]owner{
	OwnerCar:    {table: "cars"},
	OwnerDriver: {table: "drivers"},
}

// Types is the catalogue, in display order.
func Types() []Type { return catalogue }

// TypesFor is the catalogue for one owner kind.
func TypesFor(ownerKind string) []Type {
	out := []Type{}
	for _, t := range catalogue {
		if t.OwnerKind == ownerKind {
			out = append(out, t)
		}
	}
	return out
}

// TypeByKey finds a type.
func TypeByKey(key string) (Type, bool) {
	for _, t := range catalogue {
		if t.Key == key {
			return t, true
		}
	}
	return Type{}, false
}

// ValidOwnerKind reports whether kind is one this package knows.
func ValidOwnerKind(kind string) bool {
	_, ok := owners[kind]
	return ok
}

func (t Type) slot(key string) (Slot, bool) {
	for _, s := range t.Slots {
		if s.Key == key {
			return s, true
		}
	}
	return Slot{}, false
}

func (t Type) appliesToCarType(carType string) bool {
	if len(t.CarTypes) == 0 {
		return true
	}
	for _, ct := range t.CarTypes {
		if ct == carType {
			return true
		}
	}
	return false
}
