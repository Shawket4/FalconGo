package PetroApp

import (
	"Falcon/Models"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"
)

type StationWithDistance struct {
	Station      Models.PetroAppStation `json:"station"`
	Distance     float64                `json:"distance"`      // Straight-line distance
	RoadDistance float64                `json:"road_distance"` // Actual road distance in meters
	Duration     float64                `json:"duration"`      // Travel time in seconds
}

// OSRM API response structures
type OSRMResponse struct {
	Code   string      `json:"code"`
	Routes []OSRMRoute `json:"routes"`
}

type OSRMRoute struct {
	Distance float64 `json:"distance"` // in meters
	Duration float64 `json:"duration"` // in seconds
}

func GetClosestStations(c *fiber.Ctx) error {
	var input struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	}
	if err := c.BodyParser(&input); err != nil {
		log.Println(err)
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	var stations []Models.PetroAppStation
	if err := Models.DB.Model(&Models.PetroAppStation{}).Find(&stations).Error; err != nil {
		log.Println(err)
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Step 1: Calculate straight-line distances and get top 20 for OSRM processing
	// We get more than 10 because road distances might change the ranking
	output := make([]StationWithDistance, 0, len(stations))
	for _, station := range stations {
		distance := CalculateDistance(input.Lat, input.Lng, station.Lat, station.Lng)
		stationWithDistance := StationWithDistance{
			Station:  station,
			Distance: distance,
		}
		output = append(output, stationWithDistance)
	}

	// Sort by straight-line distance
	sort.Slice(output, func(i, j int) bool {
		return output[i].Distance < output[j].Distance
	})

	// Get top 20 stations for OSRM processing (to account for road distance variations)
	topStations := 20
	if len(output) > topStations {
		output = output[:topStations]
	}

	// Step 2: Get road distances and durations from OSRM for top candidates
	if err := enrichWithOSRMData(&output, input.Lat, input.Lng); err != nil {
		log.Printf("OSRM error: %v", err)
		// Fallback to straight-line distance sorting if OSRM fails
		if len(output) > 10 {
			output = output[:10]
		}
		return c.Status(http.StatusOK).JSON(fiber.Map{
			"stations": output,
			"total":    len(output),
			"warning":  "Road distances unavailable, showing straight-line distances only",
		})
	}

	// Step 3: Sort by road distance and get top 10
	sort.Slice(output, func(i, j int) bool {
		return output[i].RoadDistance < output[j].RoadDistance
	})

	// Get final top 10
	if len(output) > 10 {
		output = output[:10]
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"stations": output,
		"total":    len(output),
	})
}

func enrichWithOSRMData(stations *[]StationWithDistance, originLat, originLng float64) error {
	if len(*stations) == 0 {
		return nil
	}

	// Build coordinates string for OSRM
	// Format: "lng1,lat1;lng2,lat2;lng3,lat3"

	for i, station := range *stations {
		(*stations)[i].RoadDistance, (*stations)[i].Duration, _ = getOSRMRouteData(originLat, originLng, station.Station.Lat, station.Station.Lng)
	}

	return nil
}

// Alternative approach using OSRM route service (if you prefer individual calls)
func getOSRMRouteData(originLat, originLng, destLat, destLng float64) (float64, float64, error) {
	url := fmt.Sprintf("http://localhost:5000/route/v1/driving/%.6f,%.6f;%.6f,%.6f?overview=false",
		originLng, originLat, destLng, destLat)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("OSRM returned status %d", resp.StatusCode)
	}

	var osrmResp OSRMResponse
	if err := json.NewDecoder(resp.Body).Decode(&osrmResp); err != nil {
		return 0, 0, err
	}

	if osrmResp.Code != "Ok" || len(osrmResp.Routes) == 0 {
		return 0, 0, fmt.Errorf("no route found")
	}

	route := osrmResp.Routes[0]
	return route.Distance / 1000, route.Duration / 60, nil
}
