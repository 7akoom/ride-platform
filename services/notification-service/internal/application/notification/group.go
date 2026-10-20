package notification

import "strings"

// Group is one tab of the inbox: the notifications whose event key starts with
// one of its families ("trip" for "trip.started").
type Group string

const (
	// GroupAll is no filter.
	GroupAll     Group = ""
	GroupTrips   Group = "trips"
	GroupWallet  Group = "wallet"
	GroupOffers  Group = "offers"
	GroupSupport Group = "support"
	// GroupOther is every event key of no other group.
	GroupOther Group = "other"
)

var groupFamilies = map[Group][]string{
	GroupTrips:   {"trip"},
	GroupWallet:  {"wallet"},
	GroupOffers:  {"promo", "offer"},
	GroupSupport: {"support"},
}

// Families are the first parts of the event keys the group lists, or, when
// exclude is true (GroupOther), those it leaves out. None for GroupAll.
func (g Group) Families() (families []string, exclude bool) {
	if g == GroupOther {
		for _, group := range []Group{GroupTrips, GroupWallet, GroupOffers, GroupSupport} {
			families = append(families, groupFamilies[group]...)
		}

		return families, true
	}

	return groupFamilies[g], false
}

// GroupOf is the group of an event key.
func GroupOf(eventKey string) Group {
	family, _, _ := strings.Cut(eventKey, ".")

	for group, families := range groupFamilies {
		for _, f := range families {
			if f == family {
				return group
			}
		}
	}

	return GroupOther
}
