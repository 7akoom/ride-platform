/// Where the app talks to, and who it says it is.
class AppConfig {
  /// The API gateway.
  ///
  /// 10.0.2.2 is how the Android emulator reaches the computer it runs on. On a real phone
  /// use `adb reverse tcp:8080 tcp:8080` and localhost, or the computer's address on the Wi-Fi:
  ///   flutter run --dart-define=API_BASE_URL=http://localhost:8080
  static const String apiBaseUrl = String.fromEnvironment(
    'API_BASE_URL',
    defaultValue: 'http://10.0.2.2:8080',
  );

  /// Sent with the login so the backend can tell which app a session belongs to.
  static const String clientId = 'driver-app';

  /// How this app is named in the list of a person's sessions.
  static const String appLabel = 'Driver app';

  /// Keep in step with `version:` in pubspec.yaml.
  static const String appVersion = '0.1.0';

  /// The number the emergency button dials. Check it for the country of each deployment
  /// before real drivers use the app: a wrong number here is a safety problem.
  static const String emergencyNumber = '104';
}
