package grpc

import (
	"testing"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
)

func TestEveryRPCIsClassified(t *testing.T) {
	desc := pricingv1.PricingService_ServiceDesc
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
