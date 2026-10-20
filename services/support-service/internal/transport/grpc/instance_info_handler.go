package grpc

import (
	"context"

	supportv1 "github.com/7akoom/ride-platform/gen/go/ride/support/v1"
)

// InstanceInfo is what GetInstanceInfo answers; set once at start from the
// instance's settings.
type InstanceInfo struct {
	TermsURL   string
	PrivacyURL string
	// Emergency: kinds "general", "police", "ambulance", "fire", in order.
	Emergency []EmergencyNumber
}

type EmergencyNumber struct {
	Kind   string
	Number string
}

// WithInstanceInfo sets what GetInstanceInfo answers; without it the answer is
// empty (no pages, no numbers).
func (h *SupportHandler) WithInstanceInfo(info InstanceInfo) *SupportHandler {
	h.instance = info

	return h
}

// GetInstanceInfo is open to anyone signed in.
func (h *SupportHandler) GetInstanceInfo(ctx context.Context, _ *supportv1.GetInstanceInfoRequest) (*supportv1.GetInstanceInfoResponse, error) {
	if _, err := caller(ctx); err != nil {
		return nil, err
	}

	numbers := make([]*supportv1.EmergencyNumber, 0, len(h.instance.Emergency))
	for _, n := range h.instance.Emergency {
		numbers = append(numbers, &supportv1.EmergencyNumber{Kind: emergencyKindToProto(n.Kind), Number: n.Number})
	}

	return &supportv1.GetInstanceInfoResponse{
		TermsUrl:         h.instance.TermsURL,
		PrivacyUrl:       h.instance.PrivacyURL,
		EmergencyNumbers: numbers,
	}, nil
}

func emergencyKindToProto(kind string) supportv1.EmergencyKind {
	switch kind {
	case "general":
		return supportv1.EmergencyKind_EMERGENCY_KIND_GENERAL
	case "police":
		return supportv1.EmergencyKind_EMERGENCY_KIND_POLICE
	case "ambulance":
		return supportv1.EmergencyKind_EMERGENCY_KIND_AMBULANCE
	case "fire":
		return supportv1.EmergencyKind_EMERGENCY_KIND_FIRE
	default:
		return supportv1.EmergencyKind_EMERGENCY_KIND_UNSPECIFIED
	}
}
