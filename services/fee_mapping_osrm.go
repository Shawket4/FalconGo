package services

import (
	"log"

	"Falcon/Models"

	"gorm.io/gorm"
)

// enrichSlots caps concurrent enrichment goroutines, for the same reason the
// audit hook caps its own: when the proxy is down every call burns its full
// timeout, and a bulk import must not be able to pile up hundreds of them.
var enrichSlots = make(chan struct{}, 4)

// EnrichFeeMappingOSRM recomputes one mapping's OSRM distance and duration.
//
// Fire-and-forget: creating or updating a fee mapping must not fail, or block,
// because the routing service is slow or down. A mapping that misses its
// enrichment is picked up by the bulk enrich endpoint.
//
// Coordinates are read from the canonical terminals and drop_off_points tables,
// NOT from the copies cached on fee_mappings. Those copies exist only because
// the enrichment used to read them, and sourcing from the canonical rows here
// means a pin correction in the locations screen is reflected the next time
// this runs — where the cached copy would have gone stale silently.
func EnrichFeeMappingOSRM(db *gorm.DB, mappingID uint) {
	select {
	case enrichSlots <- struct{}{}:
	default:
		log.Printf("Warning: fee mapping OSRM enrich: saturated, dropping id=%d (bulk enrich will cover)", mappingID)
		return
	}

	go func() {
		defer func() {
			<-enrichSlots
			if r := recover(); r != nil {
				log.Printf("Warning: fee mapping OSRM enrich: recovered panic: %v", r)
			}
		}()

		var m Models.FeeMapping
		if err := db.First(&m, mappingID).Error; err != nil {
			log.Printf("Warning: fee mapping OSRM enrich: load id=%d: %v", mappingID, err)
			return
		}

		var terminal Models.Terminal
		if err := db.Where("name = ?", m.Terminal).First(&terminal).Error; err != nil {
			// Not an error worth shouting about: an unpinned terminal simply
			// cannot be routed yet.
			return
		}
		if terminal.Latitude == 0 || terminal.Longitude == 0 {
			return
		}

		var point Models.DropOffPoint
		if err := db.Where("name = ?", m.DropOffPoint).First(&point).Error; err != nil {
			return
		}
		if point.Latitude == nil || point.Longitude == nil {
			return
		}

		distanceKm, durationMin, err := Route(
			terminal.Latitude, terminal.Longitude,
			*point.Latitude, *point.Longitude,
			true,
		)
		if err != nil {
			log.Printf("Warning: fee mapping OSRM enrich: id=%d: %v", mappingID, err)
			return
		}

		if err := db.Model(&Models.FeeMapping{}).
			Where("id = ?", mappingID).
			Updates(map[string]any{
				"osrm_distance": distanceKm,
				"osrm_duration": durationMin,
			}).Error; err != nil {
			log.Printf("Warning: fee mapping OSRM enrich: save id=%d: %v", mappingID, err)
		}
	}()
}
