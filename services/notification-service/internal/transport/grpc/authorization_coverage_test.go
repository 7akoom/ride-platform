package grpc

import (
	"testing"

	notificationv1 "github.com/7akoom/ride-platform/gen/go/ride/notification/v1"
)

func TestEveryRPCIsClassified(t *testing.T) {
	desc := notificationv1.NotificationService_ServiceDesc
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

	for full := range exemptMethods {
		if _, ok := registered[full]; !ok && full != "/grpc.health.v1.Health/Check" {
			t.Errorf("exemptMethods lists %s, which is not an RPC of %s", full, desc.ServiceName)
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

	for full := range ownerChecks {
		if _, classified := methodAccess[full]; !classified {
			t.Errorf("ownerChecks lists %s, which is not in methodAccess", full)
		}
	}
}
