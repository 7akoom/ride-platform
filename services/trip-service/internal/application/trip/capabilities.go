package trip

// Optional capabilities of a Service.
//
// The Service interface stays as it is; features such as the rider's live view
// of their driver (WithDriverTracking) or the trip history (WithTripHistory) are
// added by decorators that embed the Service they wrap. A decorator only exposes
// the methods of the Service interface plus its own, so once a second decorator
// wraps the first, the first one's extra methods would be hidden from anything
// that asks for them with a type assertion. Each decorator therefore also says
// which Service it wraps (Unwrap), and As looks for a capability through the
// whole stack.

// maxDecoratorDepth stops a loop if a decorator ever returns itself from Unwrap.
const maxDecoratorDepth = 16

// Unwrapper is implemented by decorators to expose the Service they wrap.
type Unwrapper interface {
	Unwrap() Service
}

// As returns s, or the first Service it wraps, that implements T.
func As[T any](s Service) (T, bool) {
	for depth := 0; s != nil && depth < maxDecoratorDepth; depth++ {
		if found, ok := any(s).(T); ok {
			return found, true
		}

		next, ok := s.(Unwrapper)
		if !ok {
			break
		}

		s = next.Unwrap()
	}

	var none T

	return none, false
}

// Unwrap returns the Service the tracking decorator wraps.
func (s *trackingService) Unwrap() Service { return s.Service }
