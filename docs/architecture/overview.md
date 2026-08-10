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

## Initial Microservices

- Identity Service
- Tenant Service
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
- PaymentRecorded
- RatingSubmitted

## Data Ownership

Each microservice owns its own data model.

A service may reference identifiers owned by another service, but it must never directly modify another service's database.

## Multi-Tenancy

The platform is designed as a white-label multi-tenant system.

Tenant-specific configuration may include:

- Branding
- Languages
- Cities
- Service types
- Vehicle classes
- Pricing policies
- Dispatch policies
- Enabled modules
- Domains
- Operational settings

## Mobility Services

The architecture must support multiple service and vehicle types from the beginning.

Passenger mobility belongs to the core platform.

Cargo and moving capabilities are designed as expandable paid modules.

## Future Dynamic Pricing

Dynamic pricing will be implemented in a later phase.

The initial platform must still collect the operational data required for future pricing models, including:

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

Customer behavior analytics will be implemented in a later phase.

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