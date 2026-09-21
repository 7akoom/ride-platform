/// Where a driver's account stands. Only an operator moves a driver from pending to active
/// (or rejected): a new driver cannot work until then.
enum DriverStatus { active, suspended, pending, rejected, unknown }

DriverStatus driverStatusFromJson(Object? value) {
  switch (value) {
    case 'DRIVER_STATUS_ACTIVE':
      return DriverStatus.active;
    case 'DRIVER_STATUS_SUSPENDED':
      return DriverStatus.suspended;
    case 'DRIVER_STATUS_PENDING':
      return DriverStatus.pending;
    case 'DRIVER_STATUS_REJECTED':
      return DriverStatus.rejected;
    default:
      return DriverStatus.unknown;
  }
}

enum DriverAvailability { offline, available, busy, unknown }

DriverAvailability driverAvailabilityFromJson(Object? value) {
  switch (value) {
    case 'AVAILABILITY_STATUS_OFFLINE':
      return DriverAvailability.offline;
    case 'AVAILABILITY_STATUS_AVAILABLE':
      return DriverAvailability.available;
    case 'AVAILABILITY_STATUS_BUSY':
      return DriverAvailability.busy;
    default:
      return DriverAvailability.unknown;
  }
}

/// The driver's car.
class Vehicle {
  final String make;
  final String model;
  final String color;
  final String plateNumber;

  /// 'economy' or 'comfort'.
  final String vehicleClass;

  const Vehicle({
    required this.make,
    required this.model,
    required this.color,
    required this.plateNumber,
    required this.vehicleClass,
  });

  factory Vehicle.fromJson(Object? json) {
    if (json is Map) {
      return Vehicle(
        make: json['make'] as String? ?? '',
        model: json['model'] as String? ?? '',
        color: json['color'] as String? ?? '',
        plateNumber: json['plateNumber'] as String? ?? '',
        vehicleClass: json['vehicleClass'] as String? ?? '',
      );
    }

    return const Vehicle(make: '', model: '', color: '', plateNumber: '', vehicleClass: '');
  }

  Map<String, dynamic> toJson() => <String, dynamic>{
        'make': make,
        'model': model,
        'color': color,
        'plateNumber': plateNumber,
        'vehicleClass': vehicleClass,
      };

  /// "Toyota Corolla · أبيض · 33452"
  String get summary {
    final parts = <String>[
      if (make.isNotEmpty || model.isNotEmpty) '$make $model'.trim(),
      if (color.isNotEmpty) color,
      if (plateNumber.isNotEmpty) plateNumber,
    ];

    return parts.join(' · ');
  }
}

/// A driver's profile, as the backend keeps it.
class DriverProfile {
  final String id;
  final String identityId;
  final String displayName;
  final DriverStatus status;
  final DriverAvailability availability;
  final Vehicle vehicle;
  final double ratingAverage;
  final int ratingCount;

  const DriverProfile({
    required this.id,
    required this.identityId,
    required this.displayName,
    required this.status,
    required this.availability,
    required this.vehicle,
    required this.ratingAverage,
    required this.ratingCount,
  });

  factory DriverProfile.fromJson(Map<String, dynamic> json) {
    return DriverProfile(
      id: json['id'] as String? ?? '',
      identityId: json['identityId'] as String? ?? '',
      displayName: json['displayName'] as String? ?? '',
      status: driverStatusFromJson(json['status']),
      availability: driverAvailabilityFromJson(json['availabilityStatus']),
      vehicle: Vehicle.fromJson(json['vehicle']),
      ratingAverage: (json['ratingAverage'] as num?)?.toDouble() ?? 5.0,
      ratingCount: (json['ratingCount'] as num?)?.toInt() ?? 0,
    );
  }

  /// The first letter of the name, for the round avatar.
  String get initial {
    final trimmed = displayName.trim();
    if (trimmed.isEmpty) {
      return '';
    }

    return String.fromCharCode(trimmed.runes.first);
  }
}

/// Whether the driver may take trips right now. A driver whose commission debt passed the
/// limit is suspended until they pay [amountDue].
class DriverStanding {
  final bool canTakeTrips;
  final bool suspended;

  /// A decimal string.
  final String amountDue;
  final String reason;

  const DriverStanding({
    required this.canTakeTrips,
    required this.suspended,
    required this.amountDue,
    required this.reason,
  });

  factory DriverStanding.fromJson(Map<String, dynamic> json) {
    return DriverStanding(
      canTakeTrips: json['canTakeTrips'] as bool? ?? true,
      suspended: json['suspended'] as bool? ?? false,
      amountDue: json['amountDue'] as String? ?? '0',
      reason: json['reason'] as String? ?? '',
    );
  }
}
