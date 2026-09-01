package Models

import "gorm.io/gorm"

// HydrateFeeMappingCoords fills the non-persisted Latitude/Longitude on fee
// mappings from the canonical drop_off_points table.
//
// One query for the whole batch, not one per row: the fee-mappings screen
// loads every mapping at once and a per-row lookup would be hundreds of
// round trips.
//
// A drop-off with no pin leaves the pair at (0, 0), which is exactly what the
// column used to hold and what the frontend's coordinate helpers already
// treat as "unset" — so the contract is unchanged.
func HydrateFeeMappingCoords(db *gorm.DB, mappings []FeeMapping) error {
	if len(mappings) == 0 {
		return nil
	}

	names := make([]string, 0, len(mappings))
	seen := make(map[string]bool, len(mappings))
	for _, m := range mappings {
		if m.DropOffPoint != "" && !seen[m.DropOffPoint] {
			seen[m.DropOffPoint] = true
			names = append(names, m.DropOffPoint)
		}
	}
	if len(names) == 0 {
		return nil
	}

	var points []DropOffPoint
	if err := db.Where("name IN ?", names).Find(&points).Error; err != nil {
		return err
	}

	byName := make(map[string]DropOffPoint, len(points))
	for _, p := range points {
		byName[p.Name] = p
	}

	for i := range mappings {
		p, ok := byName[mappings[i].DropOffPoint]
		if !ok || p.Latitude == nil || p.Longitude == nil {
			continue
		}
		mappings[i].Latitude = *p.Latitude
		mappings[i].Longitude = *p.Longitude
	}
	return nil
}

// HydrateFeeMappingCoord is the single-row form.
func HydrateFeeMappingCoord(db *gorm.DB, mapping *FeeMapping) error {
	if mapping == nil {
		return nil
	}
	batch := []FeeMapping{*mapping}
	if err := HydrateFeeMappingCoords(db, batch); err != nil {
		return err
	}
	mapping.Latitude = batch[0].Latitude
	mapping.Longitude = batch[0].Longitude
	return nil
}
