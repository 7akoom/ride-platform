# Ride Platform Architecture Overview

## Architecture Style

Ride Platform is a white-label mobility platform built using independently deployable Go microservices.

The repository uses a monorepo structure, while each backend service remains logically and operationally independent.

## Core Principles

- Each microservice owns its business logic and data.
- Services must not read or write another service's database directly.
- Synchronous service-to-service communication uses gRPC with Protobuf contracts.
- Asynchronous domain events use NATS JetStream.
- PostgreSQL is the primary persistent data store.
- PostGIS is used for persistent geospatial data.
- Valkey is used for fast ephemeral state, caching, locks, and live geospatial data.
- Public clients communicate through an API Gateway.
- Services must be independently deployable and horizontally scalable.
- Important operations must support idempotency.
- Cross-service workflows must not depend on distributed database transactions.

## Deployment Model

Ride Platform is single-tenant per deployment, not a shared multi-tenant SaaS: the platform is built once and each investor or client licenses their own independently deployed copy, run as its own set of Docker containers under their own branding and configuration. There is no tenant-isolation layer inside a running deployment, and no Tenant Service — a single deployment serves exactly one operator.

A single deployment can still operate across more than one city or service area at once (see Service Zones below); that is about one deployment's own reach, not about serving multiple tenants from shared infrastructure.

## Initial Microservices

- Identity Service
- Rider Service
- Driver Service
- Trip Service
- Dispatch Service
- Pricing Service
- Location Service
- Wallet Service
- Notification Service

## Client Applications

- Rider Mobile App
- Driver Mobile App
- Admin Web Application

## Service Communication

### External Communication

Mobile and web applications communicate with the platform through the API Gateway.

### Synchronous Internal Communication

gRPC is used when one service requires an immediate response from another service.

Example:

Trip Service -> Pricing Service -> Fare Quote

### Asynchronous Communication

NATS JetStream is used for domain events that do not require an immediate response.

Examples:

- TripRequested
- DriverAssigned
- TripStarted
- TripCompleted
- TripCancelled
- FareCalculated
- PaymentRecorded
- RatingSubmitted

## Data Ownership

Each microservice owns its own data model.

A service may reference identifiers owned by another service, but it must never directly modify another service's database.

## Service Zones

A deployment's operator defines the geographic areas it actually serves as explicit, admin-editable service zones — geofenced polygons within a city, not just a city name, so a deployment can cover as little as one street or as much as an entire metro area. A trip request outside every active service zone is rejected. A single deployment may have zones across more than one city.

## Mobility Services

The architecture must support multiple service and vehicle types from the beginning.

Passenger mobility belongs to the core platform.

Cargo and moving capabilities are designed as expandable paid modules.

## Future Dynamic Pricing

Per-zone pricing (a distinct rate card per service zone, rather than one global rate card for the whole deployment) is a planned near-term addition.

Beyond that, dynamic pricing will be implemented in a later phase. The initial platform must still collect the operational data required for future pricing models, including:

- Demand
- Available driver supply
- Estimated wait time
- Driver acceptance rate
- Cancellation rate
- Service type
- Pickup zone
- Destination zone
- Estimated distance
- Estimated duration
- Quoted price
- Time of day

## Future Customer Behavior Analytics

Customer behavior analytics will be implemented in a later phase, consuming the domain events already published over NATS JetStream.

The event architecture must support analysis of:

- Trip request behavior
- Quote acceptance
- Cancellations
- Retention
- Repeat usage
- Service preferences
- Geographic demand
- Promotion effectiveness
- Customer cohorts

Analytics processing must remain outside the critical trip execution path.

A failure in analytics must never prevent a trip from being requested, dispatched, started, or completed.
