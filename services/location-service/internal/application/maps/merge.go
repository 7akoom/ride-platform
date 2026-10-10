package maps

import "strings"

// dedupeMeters: two results this close are the same place when one is curated,
// or when their names are the same or one holds the other.
const dedupeMeters = 75

// mergeResults puts the sources in order of trust: curated places, then
// imported places, then the map's own results (streets, addresses, places only
// it knows). A result the more trusted sources already have is left out.
// Imported places may not fill every slot while the map has other results: a
// third of the list stays the map's, and goes back to imported places when the
// map has nothing more. Never more than limit.
func mergeResults(curated, imported, mapped []Place, limit int) []Place {
	merged := make([]Place, 0, limit)
	merged = append(merged, curated[:min(len(curated), limit)]...)

	freshImported := without(imported, curated)
	freshMap := without(without(mapped, curated), freshImported)

	mapShare := min(limit/3, len(freshMap))
	firstImported := min(len(freshImported), max(0, limit-len(merged)-mapShare))

	merged = append(merged, freshImported[:firstImported]...)
	merged = append(merged, freshMap[:min(len(freshMap), max(0, limit-len(merged)))]...)
	merged = append(merged, freshImported[firstImported:][:min(len(freshImported)-firstImported, max(0, limit-len(merged)))]...)

	return merged
}

// without keeps the places that are not already among listed.
func without(places, listed []Place) []Place {
	kept := make([]Place, 0, len(places))

	for _, place := range places {
		if !duplicates(place, listed) {
			kept = append(kept, place)
		}
	}

	return kept
}

// duplicates reports whether place is one of those already listed.
func duplicates(place Place, listed []Place) bool {
	for _, other := range listed {
		if DistanceMeters(place.Coordinates, other.Coordinates) > dedupeMeters {
			continue
		}

		if place.CuratedPlaceID != "" || other.CuratedPlaceID != "" || sameName(place.Name, other.Name) {
			return true
		}
	}

	return false
}

func sameName(a, b string) bool {
	a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))

	if a == "" || b == "" {
		return false
	}

	return strings.Contains(a, b) || strings.Contains(b, a)
}
