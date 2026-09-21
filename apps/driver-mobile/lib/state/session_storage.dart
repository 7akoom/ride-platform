import 'dart:math';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// What the app remembers about who is signed in, in the phone's secure storage.
///
/// The access token lives 15 minutes; the refresh token is what keeps the person signed
/// in (ApiClient trades it for a new pair when the access token runs out). The device id
/// is made once per install and survives a sign-out, so the backend sees the same device
/// on the next login.
class SessionStorage {
  static const _storage = FlutterSecureStorage();

  static const _accessKey = 'access_token';
  static const _refreshKey = 'refresh_token';
  static const _identityKey = 'identity_id';
  static const _driverKey = 'driver_id';
  static const _deviceKey = 'device_id';

  static Future<void> saveTokens({
    required String accessToken,
    required String refreshToken,
    required String identityId,
  }) async {
    await _storage.write(key: _accessKey, value: accessToken);
    await _storage.write(key: _refreshKey, value: refreshToken);

    if (identityId.isNotEmpty) {
      await _storage.write(key: _identityKey, value: identityId);
    }
  }

  static Future<String?> readToken() => _storage.read(key: _accessKey);

  static Future<String?> readRefreshToken() => _storage.read(key: _refreshKey);

  static Future<String?> readIdentityId() => _storage.read(key: _identityKey);

  static Future<String?> readDriverId() => _storage.read(key: _driverKey);

  static Future<void> saveDriverId(String driverId) =>
      _storage.write(key: _driverKey, value: driverId);

  /// A random id for this install, made on first use.
  static Future<String> deviceId() async {
    final existing = await _storage.read(key: _deviceKey);
    if (existing != null && existing.isNotEmpty) {
      return existing;
    }

    final random = Random.secure();
    final bytes = List<int>.generate(16, (_) => random.nextInt(256));
    final id = bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

    await _storage.write(key: _deviceKey, value: id);

    return id;
  }

  /// Forgets the signed-in person. The device id stays.
  static Future<void> clear() async {
    await _storage.delete(key: _accessKey);
    await _storage.delete(key: _refreshKey);
    await _storage.delete(key: _identityKey);
    await _storage.delete(key: _driverKey);
  }
}
