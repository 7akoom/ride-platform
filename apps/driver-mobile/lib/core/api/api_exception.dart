/// A failed call to the backend.
class ApiException implements Exception {
  /// The HTTP status, or null when there was no answer at all.
  final int? statusCode;

  /// The gRPC code the backend answered with, when it said one.
  final int? code;

  /// The backend's own message (English, for developers; do not show it to riders).
  final String message;

  /// True when the request never got an answer: no connection, or it timed out.
  final bool isNetwork;

  const ApiException({
    this.statusCode,
    this.code,
    required this.message,
    this.isNetwork = false,
  });

  bool get isUnauthorized => statusCode == 401;
  bool get isForbidden => statusCode == 403;
  bool get isNotFound => statusCode == 404;
  bool get isConflict => statusCode == 409;
  bool get isTooManyRequests => statusCode == 429;
  bool get isServerError => statusCode != null && statusCode! >= 500;

  @override
  String toString() => 'ApiException(${statusCode ?? 'no answer'}: $message)';
}
