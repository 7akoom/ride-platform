import 'geo_point.dart';

enum TripStatus { requested, accepted, inProgress, completed, cancelled, unknown }

TripStatus tripStatusFromJson(Object? value) {
  switch (value) {
    case 'TRIP_STATUS_REQUESTED':
      return TripStatus.requested;
    case 'TRIP_STATUS_ACCEPTED':
      return TripStatus.accepted;
    case 'TRIP_STATUS_IN_PROGRESS':
      return TripStatus.inProgress;
    case 'TRIP_STATUS_COMPLETED':
      return TripStatus.completed;
    case 'TRIP_STATUS_CANCELLED':
      return TripStatus.cancelled;
    default:
      return TripStatus.unknown;
  }
}

/// One trip, as the backend keeps it. The driver sees where it starts and ends, not who the
/// rider is.
class Trip {
  final String id;
  final String driverId;
  final TripStatus status;
  final GeoPoint pickup;
  final GeoPoint dropoff;
  final String cancellationReason;

  /// 'cash' or 'wallet'.
  final String paymentMethod;

  const Trip({
    required this.id,
    required this.driverId,
    required this.status,
    required this.pickup,
    required this.dropoff,
    required this.cancellationReason,
    required this.paymentMethod,
  });

  factory Trip.fromJson(Map<String, dynamic> json) {
    return Trip(
      id: json['id'] as String? ?? '',
      driverId: json['driverId'] as String? ?? '',
      status: tripStatusFromJson(json['status']),
      pickup: GeoPoint.fromJson(json['pickup']),
      dropoff: GeoPoint.fromJson(json['dropoff']),
      cancellationReason: json['cancellationReason'] as String? ?? '',
      paymentMethod: json['paymentMethod'] as String? ?? '',
    );
  }
}

/// A trip put to this driver, who has until [expiresAt] to accept or reject it.
class TripOffer {
  final String tripId;
  final GeoPoint pickup;
  final GeoPoint dropoff;
  final String paymentMethod;
  final DateTime? offeredAt;
  final DateTime? expiresAt;

  const TripOffer({
    required this.tripId,
    required this.pickup,
    required this.dropoff,
    required this.paymentMethod,
    required this.offeredAt,
    required this.expiresAt,
  });

  factory TripOffer.fromJson(Map<String, dynamic> json) {
    DateTime? time(Object? value) => value is String ? DateTime.tryParse(value) : null;

    return TripOffer(
      tripId: json['tripId'] as String? ?? '',
      pickup: GeoPoint.fromJson(json['pickup']),
      dropoff: GeoPoint.fromJson(json['dropoff']),
      paymentMethod: json['paymentMethod'] as String? ?? '',
      offeredAt: time(json['offeredAt']),
      expiresAt: time(json['expiresAt']),
    );
  }
}
