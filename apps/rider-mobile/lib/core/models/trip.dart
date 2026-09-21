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

/// One trip, as the backend keeps it.
class Trip {
  final String id;
  final String riderId;

  /// Empty until a driver has accepted the trip.
  final String driverId;
  final TripStatus status;
  final GeoPoint pickup;
  final GeoPoint dropoff;
  final String cancellationReason;
  final String vehicleClass;

  /// 'cash' or 'wallet': fixed when the trip is requested.
  final String paymentMethod;

  final DateTime? requestedAt;
  final DateTime? completedAt;
  final DateTime? cancelledAt;

  const Trip({
    required this.id,
    required this.riderId,
    required this.driverId,
    required this.status,
    required this.pickup,
    required this.dropoff,
    required this.cancellationReason,
    required this.vehicleClass,
    required this.paymentMethod,
    this.requestedAt,
    this.completedAt,
    this.cancelledAt,
  });

  factory Trip.fromJson(Map<String, dynamic> json) {
    return Trip(
      id: json['id'] as String? ?? '',
      riderId: json['riderId'] as String? ?? '',
      driverId: json['driverId'] as String? ?? '',
      status: tripStatusFromJson(json['status']),
      pickup: GeoPoint.fromJson(json['pickup']),
      dropoff: GeoPoint.fromJson(json['dropoff']),
      cancellationReason: json['cancellationReason'] as String? ?? '',
      vehicleClass: json['vehicleClass'] as String? ?? '',
      paymentMethod: json['paymentMethod'] as String? ?? '',
      requestedAt: _time(json['requestedAt']),
      completedAt: _time(json['completedAt']),
      cancelledAt: _time(json['cancelledAt']),
    );
  }

  bool get hasDriver => driverId.isNotEmpty;

  /// When the trip ended (or, if it is still going, when it was requested), in the
  /// phone's time zone.
  DateTime? get when => (completedAt ?? cancelledAt ?? requestedAt)?.toLocal();

  static DateTime? _time(Object? value) {
    if (value is! String || value.isEmpty) {
      return null;
    }

    return DateTime.tryParse(value);
  }
}

/// One page of the rider's trip history, newest first.
class TripPage {
  final List<Trip> trips;

  /// Empty on the last page.
  final String nextPageToken;

  const TripPage({required this.trips, required this.nextPageToken});
}

/// What a rider may know about the driver of their trip.
class TripDriverInfo {
  final String name;
  final String vehicle;
  final String plateNumber;
  final double ratingAverage;
  final int ratingCount;

  const TripDriverInfo({
    required this.name,
    required this.vehicle,
    required this.plateNumber,
    required this.ratingAverage,
    required this.ratingCount,
  });

  factory TripDriverInfo.fromJson(Map<String, dynamic> json) {
    final vehicleJson = json['vehicle'];
    var vehicle = '';
    var plate = '';

    if (vehicleJson is Map) {
      final parts = <String>[
        for (final key in ['make', 'model', 'color'])
          if (vehicleJson[key] is String && (vehicleJson[key] as String).isNotEmpty)
            vehicleJson[key] as String,
      ];
      vehicle = parts.join(' · ');
      plate = vehicleJson['plateNumber'] as String? ?? '';
    }

    return TripDriverInfo(
      name: json['displayName'] as String? ?? '',
      vehicle: vehicle,
      plateNumber: plate,
      ratingAverage: (json['ratingAverage'] as num?)?.toDouble() ?? 5.0,
      ratingCount: (json['ratingCount'] as num?)?.toInt() ?? 0,
    );
  }

  String get initial {
    final trimmed = name.trim();
    if (trimmed.isEmpty) {
      return '';
    }

    return String.fromCharCode(trimmed.runes.first);
  }

  /// "Toyota · Corolla · White · 33452" for the driver card.
  String get carInfo {
    final parts = <String>[
      if (vehicle.isNotEmpty) vehicle,
      if (plateNumber.isNotEmpty) plateNumber,
    ];

    return parts.join(' · ');
  }
}
