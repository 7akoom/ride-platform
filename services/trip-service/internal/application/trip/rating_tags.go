package trip

import (
	"errors"
	"strings"
)

// A rating's tags: what it was for, beside the stars. Each side has its own.
const (
	// The rider about the captain.
	TagSafeDriving = "safe_driving"
	TagCleanCar    = "clean_car"
	TagFriendly    = "friendly"
	TagKnowsTheWay = "knows_the_way"

	// The captain about the rider.
	TagOnTime = "on_time"
	TagPolite = "polite"
)

var ErrInvalidRatingTag = errors.New("a tag is not one this side may give")

var ratingTags = map[RatedBy][]string{
	RatedByRider:  {TagSafeDriving, TagCleanCar, TagFriendly, TagKnowsTheWay},
	RatedByDriver: {TagOnTime, TagPolite},
}

// normalizeRatingTags keeps each tag once, in the order of the side's list, and
// refuses one the side may not give.
func normalizeRatingTags(by RatedBy, tags []string) ([]string, error) {
	allowed := ratingTags[by]
	picked := make(map[string]bool, len(tags))

	for _, tag := range tags {
		tag = strings.TrimSpace(tag)

		ok := false
		for _, a := range allowed {
			ok = ok || a == tag
		}

		if !ok {
			return nil, ErrInvalidRatingTag
		}

		picked[tag] = true
	}

	out := make([]string, 0, len(picked))
	for _, a := range allowed {
		if picked[a] {
			out = append(out, a)
		}
	}

	return out, nil
}
