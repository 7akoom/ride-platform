package main

// tripOffersDurable is the durable consumer that turns trip.offered into a push for the driver.
// It is separate from tripEventsDurable on purpose: an offer lasts seconds, so it must never
// wait behind the other trip events, and adding it does not change the existing consumer.
// Its name must stay stable across restarts so redelivery resumes where it left off.
const tripOffersDurable = "notification-trip-offers"
