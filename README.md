# Ride Platform

White-label mobility platform built with Go microservices.

## Product Scope

The platform is designed to support:

- Passenger mobility
- Multiple vehicle and service types
- White-label tenants
- Real-time driver dispatch
- Live location tracking
- Pricing and commissions
- Driver wallets
- Notifications
- Cargo and moving services as expandable modules
- Future dynamic pricing
- Future customer behavior analytics

## Repository Structure

- `services/` - Go microservices
- Apps live in their own repos: [ride-rider-app](https://github.com/7akoom/ride-rider-app), [ride-driver-app](https://github.com/7akoom/ride-driver-app), [ride-admin-web](https://github.com/7akoom/ride-admin-web)
- `proto/` - Protobuf and gRPC contracts
- `infrastructure/` - Docker, gateway, and deployment configuration
- `docs/` - Architecture decisions and documentation
- `scripts/` - Development and automation scripts