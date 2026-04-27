package MultiRouteOptimizer

import (
	"fmt"
	"math"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/exp/rand"
)

func OptimalRouteHandler(c *fiber.Ctx) error {
	// Parse request
	var req RouteRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body: "+err.Error())
	}

	// Validate request
	if req.Start == (Point{}) || req.End == (Point{}) {
		return fiber.NewError(fiber.StatusBadRequest, "Start and end points are required")
	}

	// Set defaults
	if req.Optimization == "" {
		req.Optimization = "distance"
	}
	if req.TravelMode == "" {
		req.TravelMode = "driving"
	}
	if req.Algorithm == "" {
		req.Algorithm = "2opt" // Default to nearest neighbor with 2-opt
	}

	// Create a list of all points including start and end
	allPoints := append([]Point{req.Start}, append(req.Waypoints, req.End)...)
	n := len(allPoints)

	// Create distance matrix
	distMatrix := make([][]float64, n)
	for i := range distMatrix {
		distMatrix[i] = make([]float64, n)
		for j := range distMatrix[i] {
			distMatrix[i][j] = haversineDistance(allPoints[i], allPoints[j])
		}
	}

	// Choose algorithm based on request
	var route []int
	var algorithmName string

	switch req.Algorithm {
	case "nearest":
		route = nearestNeighborTSP(distMatrix, 0, n-1)
		algorithmName = "Nearest Neighbor"
	case "simulated":
		route = simulatedAnnealing(distMatrix, 0, n-1)
		algorithmName = "Simulated Annealing"
	case "genetic":
		route = geneticAlgorithm(distMatrix, 0, n-1, 100, 1000)
		algorithmName = "Genetic Algorithm"
	default: // "2opt" or any other value
		route = nearestNeighborTSP(distMatrix, 0, n-1)
		route = twoOptImprovement(route, distMatrix)
		algorithmName = "Nearest Neighbor with 2-opt improvement"
	}

	// Calculate total distance
	totalDistance := 0.0
	for i := 0; i < len(route)-1; i++ {
		totalDistance += distMatrix[route[i]][route[i+1]]
	}

	// Extract the waypoints (excluding start and end)
	var resultRoute []Point
	for i := 1; i < len(route)-1; i++ {
		resultRoute = append(resultRoute, allPoints[route[i]])
	}

	// Calculate estimated duration based on average speed for the travel mode
	speed := AverageSpeeds[req.TravelMode]
	if speed == 0 {
		speed = AverageSpeeds["driving"] // Default to driving if travel mode not found
	}

	// Convert distance (km) to duration (seconds) based on average speed
	estimatedDuration := (totalDistance / speed) * 3600 // Convert hours to seconds

	// Generate Google Maps URL
	googleMapsURL := generateGoogleMapsURL(req.Start, req.End, resultRoute, req.TravelMode)

	// Create response
	response := RouteResponse{
		OptimalRoute:      append([]Point{req.Start}, append(resultRoute, req.End)...),
		TotalDistance:     math.Round(totalDistance*100) / 100, // Round to 2 decimal places
		EstimatedDuration: math.Round(estimatedDuration*100) / 100,
		GoogleMapsURL:     googleMapsURL,
		Algorithm:         algorithmName,
	}

	// Return response
	return c.JSON(response)
}

// haversineDistance calculates the great-circle distance between two points on Earth
func haversineDistance(p1, p2 Point) float64 {
	// Convert latitude and longitude from degrees to radians
	lat1 := p1.Lat * math.Pi / 180
	lng1 := p1.Lng * math.Pi / 180
	lat2 := p2.Lat * math.Pi / 180
	lng2 := p2.Lng * math.Pi / 180

	// Haversine formula
	dlat := lat2 - lat1
	dlng := lng2 - lng1
	a := math.Pow(math.Sin(dlat/2), 2) + math.Cos(lat1)*math.Cos(lat2)*math.Pow(math.Sin(dlng/2), 2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	distance := earthRadius * c

	return distance
}

// nearestNeighborTSP implements the nearest neighbor algorithm for TSP
// with fixed start and end points
func nearestNeighborTSP(distMatrix [][]float64, startIdx, endIdx int) []int {
	n := len(distMatrix)
	route := make([]int, 0, n)
	visited := make([]bool, n)

	// Add the start point
	route = append(route, startIdx)
	visited[startIdx] = true
	visited[endIdx] = true // Mark end as temporarily visited

	current := startIdx
	remaining := n - 2 // Minus start and end

	// Find nearest neighbors
	for remaining > 0 {
		nearest := -1
		minDist := math.Inf(1)

		for j := 0; j < n; j++ {
			if !visited[j] && distMatrix[current][j] < minDist {
				minDist = distMatrix[current][j]
				nearest = j
			}
		}

		if nearest != -1 {
			route = append(route, nearest)
			visited[nearest] = true
			current = nearest
			remaining--
		} else {
			break
		}
	}

	// Add the end point
	visited[endIdx] = false // Unmark end
	route = append(route, endIdx)

	return route
}

// twoOptImprovement implements the 2-opt improvement algorithm
func twoOptImprovement(route []int, distMatrix [][]float64) []int {
	n := len(route)
	improvement := true

	for improvement {
		improvement = false

		for i := 0; i < n-3; i++ {
			for j := i + 2; j < n-1; j++ {
				// Calculate current distance
				currentDist := distMatrix[route[i]][route[i+1]] + distMatrix[route[j]][route[j+1]]

				// Calculate new distance if we swap
				newDist := distMatrix[route[i]][route[j]] + distMatrix[route[i+1]][route[j+1]]

				// If new distance is shorter, swap
				if newDist < currentDist {
					// Reverse the sub-route between i+1 and j
					for k, l := i+1, j; k < l; k, l = k+1, l-1 {
						route[k], route[l] = route[l], route[k]
					}
					improvement = true
				}
			}
		}
	}

	return route
}

// simulatedAnnealing implements the simulated annealing algorithm for TSP
func simulatedAnnealing(distMatrix [][]float64, startIdx, endIdx int) []int {
	n := len(distMatrix)
	_ = n
	// Initialize with nearest neighbor solution
	route := nearestNeighborTSP(distMatrix, startIdx, endIdx)

	// Initial route cost
	bestRoute := make([]int, len(route))
	copy(bestRoute, route)
	bestCost := routeCost(route, distMatrix)
	currentCost := bestCost

	// Simulated annealing parameters
	temperature := 100.0
	coolingRate := 0.99
	minTemperature := 0.01

	// Main simulated annealing loop
	for temperature > minTemperature {
		// Create new solution by swapping two cities (excluding start and end)
		newRoute := make([]int, len(route))
		copy(newRoute, route)

		// Get two random indices to swap (excluding start and end)
		i := rand.Intn(len(route)-2) + 1 // +1 to skip start
		j := rand.Intn(len(route)-2) + 1
		for i == j {
			j = rand.Intn(len(route)-2) + 1
		}

		// Swap
		newRoute[i], newRoute[j] = newRoute[j], newRoute[i]

		// Calculate new cost
		newCost := routeCost(newRoute, distMatrix)

		// Decide if we should accept the new solution
		if acceptNewSolution(currentCost, newCost, temperature) {
			route = newRoute
			currentCost = newCost

			// Update best solution if new solution is better
			if newCost < bestCost {
				copy(bestRoute, route)
				bestCost = newCost
			}
		}

		// Cool down
		temperature *= coolingRate
	}

	return bestRoute
}

// acceptNewSolution decides whether to accept a new solution in simulated annealing
func acceptNewSolution(currentCost, newCost, temperature float64) bool {
	// If new solution is better, accept it
	if newCost < currentCost {
		return true
	}

	// Otherwise, accept with a probability that depends on the temperature
	// and how much worse the new solution is
	delta := newCost - currentCost
	probability := math.Exp(-delta / temperature)

	return rand.Float64() < probability
}

// routeCost calculates the total cost of a route
func routeCost(route []int, distMatrix [][]float64) float64 {
	cost := 0.0
	for i := 0; i < len(route)-1; i++ {
		cost += distMatrix[route[i]][route[i+1]]
	}
	return cost
}

// geneticAlgorithm implements a genetic algorithm for TSP
func geneticAlgorithm(distMatrix [][]float64, startIdx, endIdx int,
	populationSize, generations int) []int {
	n := len(distMatrix)

	// For small problems, just use nearest neighbor with 2-opt
	if n <= 4 {
		route := nearestNeighborTSP(distMatrix, startIdx, endIdx)
		return twoOptImprovement(route, distMatrix)
	}

	// Initialize population
	population := initializePopulation(distMatrix, startIdx, endIdx, populationSize)

	// Main genetic algorithm loop
	for gen := 0; gen < generations; gen++ {
		// Evaluate fitness
		fitness := make([]float64, populationSize)
		totalFitness := 0.0

		for i := 0; i < populationSize; i++ {
			cost := routeCost(population[i], distMatrix)
			fitness[i] = 1.0 / cost // Invert cost to get fitness (higher is better)
			totalFitness += fitness[i]
		}

		// Create new population
		newPopulation := make([][]int, populationSize)

		// Elitism: keep the best solution
		bestIdx := 0
		for i := 1; i < populationSize; i++ {
			if fitness[i] > fitness[bestIdx] {
				bestIdx = i
			}
		}
		newPopulation[0] = make([]int, len(population[bestIdx]))
		copy(newPopulation[0], population[bestIdx])

		// Apply 2-opt to the best solution
		newPopulation[0] = twoOptImprovement(newPopulation[0], distMatrix)

		// Create rest of new population
		for i := 1; i < populationSize; i++ {
			// Selection
			parent1 := selectParent(population, fitness, totalFitness)
			parent2 := selectParent(population, fitness, totalFitness)

			// Crossover
			child := orderCrossover(parent1, parent2, startIdx, endIdx)

			// Mutation
			if rand.Float64() < 0.2 { // 20% mutation rate
				mutate(child, startIdx, endIdx)
			}

			newPopulation[i] = child
		}

		// Replace old population
		population = newPopulation
	}

	// Find best solution
	bestIdx := 0
	bestFitness := 1.0 / routeCost(population[0], distMatrix)

	for i := 1; i < populationSize; i++ {
		fitness := 1.0 / routeCost(population[i], distMatrix)
		if fitness > bestFitness {
			bestFitness = fitness
			bestIdx = i
		}
	}

	// Apply 2-opt to the final solution for additional improvement
	return twoOptImprovement(population[bestIdx], distMatrix)
}

// initializePopulation creates an initial population for the genetic algorithm
func initializePopulation(distMatrix [][]float64, startIdx, endIdx int, populationSize int) [][]int {
	n := len(distMatrix)
	population := make([][]int, populationSize)

	// Create intermediate points list (excluding start and end)
	intermediate := make([]int, 0, n-2)
	for i := 0; i < n; i++ {
		if i != startIdx && i != endIdx {
			intermediate = append(intermediate, i)
		}
	}

	// Create random routes
	for i := 0; i < populationSize; i++ {
		// Shuffle intermediate points
		shuffled := make([]int, len(intermediate))
		copy(shuffled, intermediate)
		rand.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})

		// Create complete route
		route := make([]int, 0, n)
		route = append(route, startIdx)
		route = append(route, shuffled...)
		route = append(route, endIdx)

		population[i] = route
	}

	return population
}

// selectParent selects a parent for crossover using roulette wheel selection
func selectParent(population [][]int, fitness []float64, totalFitness float64) []int {
	// Roulette wheel selection
	value := rand.Float64() * totalFitness
	sum := 0.0

	for i := 0; i < len(population); i++ {
		sum += fitness[i]
		if sum >= value {
			selected := make([]int, len(population[i]))
			copy(selected, population[i])
			return selected
		}
	}

	// Fallback (should rarely happen due to floating point precision)
	selected := make([]int, len(population[0]))
	copy(selected, population[0])
	return selected
}

// orderCrossover implements ordered crossover (OX) for TSP
func orderCrossover(parent1, parent2 []int, startIdx, endIdx int) []int {
	n := len(parent1)
	child := make([]int, n)

	// Initialize with -1 (unassigned)
	for i := range child {
		child[i] = -1
	}

	// Always keep start and end fixed
	child[0] = startIdx
	child[n-1] = endIdx

	// Select a random segment from parent1
	segmentStart := rand.Intn(n-3) + 1 // +1 to skip start
	segmentEnd := segmentStart + rand.Intn(n-segmentStart-1)

	// Copy segment from parent1 to child
	for i := segmentStart; i <= segmentEnd; i++ {
		child[i] = parent1[i]
	}

	// Fill remaining positions with values from parent2 in order
	currentPos := 1            // Start after the fixed start point
	for i := 1; i < n-1; i++ { // Skip start and end points in parent2
		// Skip if already in the child's segment
		if contains(child, parent2[i]) {
			continue
		}

		// Find next available position
		for currentPos < n-1 && child[currentPos] != -1 {
			currentPos++
		}

		// If we found a position, fill it
		if currentPos < n-1 {
			child[currentPos] = parent2[i]
		}
	}

	return child
}

// contains checks if value is in array
func contains(arr []int, value int) bool {
	for _, v := range arr {
		if v == value {
			return true
		}
	}
	return false
}

// mutate performs a swap mutation on the route (excluding start and end points)
func mutate(route []int, startIdx, endIdx int) {
	n := len(route)

	// Select two random positions to swap (excluding start and end)
	i := rand.Intn(n-2) + 1 // +1 to skip start
	j := rand.Intn(n-2) + 1
	for i == j {
		j = rand.Intn(n-2) + 1
	}

	// Swap
	route[i], route[j] = route[j], route[i]
}

// generateGoogleMapsURL creates a Google Maps URL for the route
func generateGoogleMapsURL(start, end Point, waypoints []Point, mode string) string {
	baseURL := "https://www.google.com/maps/dir/?api=1"

	// Add origin
	params := url.Values{}
	params.Add("origin", fmt.Sprintf("%.6f,%.6f", start.Lat, start.Lng))

	// Add destination
	params.Add("destination", fmt.Sprintf("%.6f,%.6f", end.Lat, end.Lng))

	// Add waypoints if any
	if len(waypoints) > 0 {
		var waypointStrings []string
		for _, wp := range waypoints {
			waypointStrings = append(waypointStrings, fmt.Sprintf("%.6f,%.6f", wp.Lat, wp.Lng))
		}
		params.Add("waypoints", strings.Join(waypointStrings, "|"))
	}

	// Add travel mode
	params.Add("travelmode", mode)

	return baseURL + "&" + params.Encode()
}
