/// A rider's profile, as the backend keeps it.
class RiderProfile {
  final String id;
  final String identityId;
  final String displayName;
  final double ratingAverage;
  final int ratingCount;

  const RiderProfile({
    required this.id,
    required this.identityId,
    required this.displayName,
    required this.ratingAverage,
    required this.ratingCount,
  });

  factory RiderProfile.fromJson(Map<String, dynamic> json) {
    return RiderProfile(
      id: json['id'] as String? ?? '',
      identityId: json['identityId'] as String? ?? '',
      displayName: json['displayName'] as String? ?? '',
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
