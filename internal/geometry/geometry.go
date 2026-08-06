package geometry

import (
	"math"
	"sort"

	"github.com/greenroute/greenroute/internal/domain"
)

const earthRadiusMeters = 6_371_008.8

// MaxSampledPoints is a hard CPU/memory bound for pairwise geometry
// comparisons. At 128 points, a pairwise similarity pass remains bounded to
// tens of thousands of distance checks, independent of provider polyline
// length. This is intentionally a safety limit, not provider geometry storage.
const MaxSampledPoints = 128

const maxSimplifyInputPoints = 512

func DistanceMeters(a, b domain.GeoPoint) float64 {
	lat1, lat2 := radians(a.Latitude), radians(b.Latitude)
	dLat := lat2 - lat1
	dLon := radians(b.Longitude - a.Longitude)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	h = math.Max(0, math.Min(1, h))
	return earthRadiusMeters * 2 * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
}

func PolylineLength(points []domain.GeoPoint) float64 {
	var result float64
	for i := 1; i < len(points); i++ {
		result += DistanceMeters(points[i-1], points[i])
	}
	return result
}

// HausdorffDistance returns a symmetric, point-sampled Hausdorff distance. Both
// paths are densified first so a sparse provider geometry cannot appear similar
// merely because its vertices happen to align.
func HausdorffDistance(a, b []domain.GeoPoint, sampleEveryMeters float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return math.Inf(1)
	}
	if sampleEveryMeters <= 0 {
		sampleEveryMeters = 100
	}
	a = Densify(a, sampleEveryMeters)
	b = Densify(b, sampleEveryMeters)
	return math.Max(directedHausdorff(a, b), directedHausdorff(b, a))
}

func directedHausdorff(a, b []domain.GeoPoint) float64 {
	var maximum float64
	for _, point := range a {
		minimum := math.Inf(1)
		for _, candidate := range b {
			minimum = math.Min(minimum, DistanceMeters(point, candidate))
		}
		maximum = math.Max(maximum, minimum)
	}
	return maximum
}

func Densify(points []domain.GeoPoint, everyMeters float64) []domain.GeoPoint {
	if len(points) < 2 || everyMeters <= 0 {
		return append([]domain.GeoPoint(nil), points...)
	}
	lengths := make([]float64, len(points)-1)
	total := 0.0
	for index := 1; index < len(points); index++ {
		lengths[index-1] = DistanceMeters(points[index-1], points[index])
		total += lengths[index-1]
	}
	if total <= 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		return []domain.GeoPoint{points[0], points[len(points)-1]}
	}
	sampleCount := int(math.Ceil(total/everyMeters)) + 1
	if sampleCount < 2 {
		sampleCount = 2
	}
	if sampleCount > MaxSampledPoints {
		sampleCount = MaxSampledPoints
	}
	result := make([]domain.GeoPoint, 0, sampleCount)
	result = append(result, points[0])
	interval := total / float64(sampleCount-1)
	segmentIndex, traversed := 0, 0.0
	for sample := 1; sample < sampleCount-1; sample++ {
		target := float64(sample) * interval
		for segmentIndex < len(lengths)-1 && traversed+lengths[segmentIndex] < target {
			traversed += lengths[segmentIndex]
			segmentIndex++
		}
		segmentLength := lengths[segmentIndex]
		if segmentLength <= 0 {
			result = append(result, points[segmentIndex+1])
			continue
		}
		t := clamp01((target - traversed) / segmentLength)
		result = append(result, interpolate(points[segmentIndex], points[segmentIndex+1], t))
	}
	result = append(result, points[len(points)-1])
	return result
}

func OverlapRatio(a, b []domain.GeoPoint, corridorMeters, sampleEveryMeters float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	if corridorMeters <= 0 {
		corridorMeters = 250
	}
	a = Densify(a, sampleEveryMeters)
	b = Densify(b, sampleEveryMeters)
	return math.Min(directedOverlap(a, b, corridorMeters), directedOverlap(b, a, corridorMeters))
}

func directedOverlap(a, b []domain.GeoPoint, corridor float64) float64 {
	matched := 0
	for _, point := range a {
		for _, candidate := range b {
			if DistanceMeters(point, candidate) <= corridor {
				matched++
				break
			}
		}
	}
	return float64(matched) / float64(len(a))
}

func Similarity(a, b []domain.GeoPoint, maxHausdorffMeters float64) float64 {
	if maxHausdorffMeters <= 0 {
		maxHausdorffMeters = 750
	}
	sampledA, sampledB := Densify(a, 100), Densify(b, 100)
	hausdorff := math.Max(directedHausdorff(sampledA, sampledB), directedHausdorff(sampledB, sampledA))
	distanceScore := clamp01(1 - hausdorff/maxHausdorffMeters)
	overlap := math.Min(directedOverlap(sampledA, sampledB, 250), directedOverlap(sampledB, sampledA, 250))
	lengthA, lengthB := PolylineLength(a), PolylineLength(b)
	lengthScore := 0.0
	if math.Max(lengthA, lengthB) > 0 {
		lengthScore = math.Min(lengthA, lengthB) / math.Max(lengthA, lengthB)
	}
	return clamp01(0.45*overlap + 0.35*distanceScore + 0.20*lengthScore)
}

type DedupeConfig struct {
	MinimumOverlap          float64
	MaximumHausdorffMeters  float64
	MaximumLengthDifference float64
	SampleEveryMeters       float64
}

func DefaultDedupeConfig() DedupeConfig {
	return DedupeConfig{MinimumOverlap: 0.82, MaximumHausdorffMeters: 500, MaximumLengthDifference: 0.08, SampleEveryMeters: 100}
}

func IsDuplicate(a, b domain.RouteCandidate, config DedupeConfig) bool {
	maxLength := math.Max(float64(a.DistanceMeters), float64(b.DistanceMeters))
	if maxLength <= 0 || math.Abs(float64(a.DistanceMeters-b.DistanceMeters))/maxLength > config.MaximumLengthDifference {
		return false
	}
	if boundingBoxesSeparated(a.Geometry, b.Geometry, config.MaximumHausdorffMeters) {
		return false
	}
	if OverlapRatio(a.Geometry, b.Geometry, 250, config.SampleEveryMeters) < config.MinimumOverlap {
		return false
	}
	return HausdorffDistance(a.Geometry, b.Geometry, config.SampleEveryMeters) <= config.MaximumHausdorffMeters
}

// Deduplicate deterministically retains the fastest candidate in each geometry
// cluster and returns candidates ordered by duration then stable candidate ID.
func Deduplicate(candidates []domain.RouteCandidate, config DedupeConfig) ([]domain.RouteCandidate, int) {
	ordered := append([]domain.RouteCandidate(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].LiveDurationSeconds != ordered[j].LiveDurationSeconds {
			return ordered[i].LiveDurationSeconds < ordered[j].LiveDurationSeconds
		}
		return ordered[i].CandidateID < ordered[j].CandidateID
	})
	result := make([]domain.RouteCandidate, 0, len(ordered))
	dropped := 0
	for _, candidate := range ordered {
		duplicate := false
		for _, existing := range result {
			if IsDuplicate(candidate, existing, config) {
				duplicate = true
				dropped++
				break
			}
		}
		if !duplicate {
			result = append(result, candidate)
		}
	}
	return result, dropped
}

// Simplify applies Douglas-Peucker while always retaining endpoints.
func Simplify(points []domain.GeoPoint, toleranceMeters float64) []domain.GeoPoint {
	if len(points) <= 2 || toleranceMeters <= 0 {
		return append([]domain.GeoPoint(nil), points...)
	}
	if len(points) > maxSimplifyInputPoints {
		points = decimate(points, maxSimplifyInputPoints)
	}
	keep := make([]bool, len(points))
	keep[0], keep[len(points)-1] = true, true
	type interval struct{ first, last int }
	stack := []interval{{0, len(points) - 1}}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		index, maximum := -1, 0.0
		for pointIndex := current.first + 1; pointIndex < current.last; pointIndex++ {
			distance := pointToSegmentMeters(points[pointIndex], points[current.first], points[current.last])
			if distance > maximum {
				index, maximum = pointIndex, distance
			}
		}
		if index >= 0 && maximum > toleranceMeters {
			keep[index] = true
			stack = append(stack, interval{current.first, index}, interval{index, current.last})
		}
	}
	result := make([]domain.GeoPoint, 0, len(points))
	for index, point := range points {
		if keep[index] {
			result = append(result, point)
		}
	}
	return result
}

func decimate(points []domain.GeoPoint, maximum int) []domain.GeoPoint {
	if len(points) <= maximum || maximum < 2 {
		return append([]domain.GeoPoint(nil), points...)
	}
	result := make([]domain.GeoPoint, maximum)
	for index := range result {
		source := int(math.Round(float64(index) * float64(len(points)-1) / float64(maximum-1)))
		result[index] = points[source]
	}
	return result
}

func boundingBoxesSeparated(a, b []domain.GeoPoint, toleranceMeters float64) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	minLatA, maxLatA, minLonA, maxLonA := bounds(a)
	minLatB, maxLatB, minLonB, maxLonB := bounds(b)
	latitudeSlack := toleranceMeters / 111_320
	meanLatitude := (minLatA + maxLatA + minLatB + maxLatB) / 4
	longitudeScale := math.Max(.05, math.Cos(radians(meanLatitude)))
	longitudeSlack := toleranceMeters / (111_320 * longitudeScale)
	return maxLatA+latitudeSlack < minLatB || maxLatB+latitudeSlack < minLatA ||
		maxLonA+longitudeSlack < minLonB || maxLonB+longitudeSlack < minLonA
}

func bounds(points []domain.GeoPoint) (minLatitude, maxLatitude, minLongitude, maxLongitude float64) {
	minLatitude, maxLatitude = points[0].Latitude, points[0].Latitude
	minLongitude, maxLongitude = points[0].Longitude, points[0].Longitude
	for _, point := range points[1:] {
		minLatitude, maxLatitude = math.Min(minLatitude, point.Latitude), math.Max(maxLatitude, point.Latitude)
		minLongitude, maxLongitude = math.Min(minLongitude, point.Longitude), math.Max(maxLongitude, point.Longitude)
	}
	return
}

func pointToSegmentMeters(point, start, end domain.GeoPoint) float64 {
	// Equirectangular projection is sufficiently accurate at route-segment scale.
	meanLat := radians((start.Latitude + end.Latitude + point.Latitude) / 3)
	x1, y1 := radians(start.Longitude)*math.Cos(meanLat)*earthRadiusMeters, radians(start.Latitude)*earthRadiusMeters
	x2, y2 := radians(end.Longitude)*math.Cos(meanLat)*earthRadiusMeters, radians(end.Latitude)*earthRadiusMeters
	x, y := radians(point.Longitude)*math.Cos(meanLat)*earthRadiusMeters, radians(point.Latitude)*earthRadiusMeters
	dx, dy := x2-x1, y2-y1
	if dx == 0 && dy == 0 {
		return math.Hypot(x-x1, y-y1)
	}
	t := clamp01(((x-x1)*dx + (y-y1)*dy) / (dx*dx + dy*dy))
	return math.Hypot(x-(x1+t*dx), y-(y1+t*dy))
}

func interpolate(a, b domain.GeoPoint, t float64) domain.GeoPoint {
	return domain.GeoPoint{Latitude: a.Latitude + (b.Latitude-a.Latitude)*t, Longitude: a.Longitude + (b.Longitude-a.Longitude)*t}
}

func radians(value float64) float64 { return value * math.Pi / 180 }

func clamp01(value float64) float64 { return math.Max(0, math.Min(1, value)) }
