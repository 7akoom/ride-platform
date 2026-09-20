package grpc

import (
	"testing"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
)

func TestEveryRPCIsClassified(t *testing.T) {
	desc := tripv1.TripService_ServiceDesc
	prefix := "/" + desc.ServiceName + "/"

	registered := map[string]struct{}{}

	for _, method := range desc.Methods {
		full := prefix + method.MethodName
		registered[full] = struct{}{}

		if _, exempt := exemptMethods[full]; exempt {
			continue
		}

		if _, classified := methodAccess[full]; !classified {
			t.Errorf("%s has no access level in methodAccess", full)
		}
	}

	for full := range methodAccess {
		if _, ok := registered[full]; !ok {
			t.Errorf("methodAccess lists %s, which is not an RPC of %s", full, desc.ServiceName)
		}
	}
}

func TestEveryOwnerMethodHasAnOwnerCheck(t *testing.T) {
	for full, level := range methodAccess {
		_, hasCheck := ownerChecks[full]

		if level == accessOwner && !hasCheck {
			t.Errorf("%s is accessOwner but has no owner check", full)
		}

		if level != accessOwner && hasCheck {
			t.Errorf("%s has an owner check but is not accessOwner", full)
		}
	}
}
