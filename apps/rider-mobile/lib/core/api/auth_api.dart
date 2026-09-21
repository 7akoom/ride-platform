import 'package:flutter/foundation.dart';

import '../../state/session_storage.dart';
import '../app_config.dart';
import 'api_client.dart';

/// The answer to "send me a login code": which code to check the answer against.
class OtpChallenge {
  final String challengeId;
  final int expiresInSeconds;

  const OtpChallenge({required this.challengeId, required this.expiresInSeconds});
}

/// The tokens a successful login returns.
class AuthTokens {
  final String identityId;
  final String accessToken;
  final String refreshToken;

  const AuthTokens({
    required this.identityId,
    required this.accessToken,
    required this.refreshToken,
  });
}

/// Who the backend says is signed in.
class MyIdentity {
  final String identityId;

  /// The verified phone number, or null if there is none.
  final String? phone;

  const MyIdentity({required this.identityId, required this.phone});
}

/// Login, logout and "who am I": the identity routes of the gateway.
class AuthApi {
  AuthApi(this._client);

  final ApiClient _client;

  /// Asks for a login code to be sent to [e164Phone] (for example +9647701234567).
  Future<OtpChallenge> requestOtp(String e164Phone) async {
    final json = await _client.post(
      '/v1/auth/otp:request',
      auth: false,
      body: <String, dynamic>{
        'identifier': <String, dynamic>{
          'type': 'IDENTIFIER_TYPE_PHONE',
          'value': e164Phone,
        },
      },
    );

    return OtpChallenge(
      challengeId: json['challengeId'] as String? ?? '',
      expiresInSeconds: (json['expiresInSeconds'] as num?)?.toInt() ?? 0,
    );
  }

  /// Checks the code the person typed. On success the backend returns the tokens; the
  /// caller stores them.
  Future<AuthTokens> verifyOtp({
    required String challengeId,
    required String code,
  }) async {
    final platform = _platformName();

    final json = await _client.post(
      '/v1/auth/otp:verify',
      auth: false,
      body: <String, dynamic>{
        'challengeId': challengeId,
        'code': code,
        'clientId': AppConfig.clientId,
        'deviceId': await SessionStorage.deviceId(),
        'deviceName': 'Rider app ($platform)',
        'platform': platform,
        'appVersion': AppConfig.appVersion,
      },
    );

    return AuthTokens(
      identityId: json['identityId'] as String? ?? '',
      accessToken: json['accessToken'] as String? ?? '',
      refreshToken: json['refreshToken'] as String? ?? '',
    );
  }

  Future<MyIdentity> getMyIdentity() async {
    final json = await _client.get('/v1/me');

    String? phone;
    final identifiers = json['identifiers'];

    if (identifiers is List) {
      for (final item in identifiers) {
        if (item is Map && item['type'] == 'IDENTIFIER_TYPE_PHONE') {
          final value = item['value'];
          if (value is String && value.isNotEmpty) {
            phone = value;
            break;
          }
        }
      }
    }

    return MyIdentity(
      identityId: json['identityId'] as String? ?? '',
      phone: phone,
    );
  }

  /// Ends this session on the backend. The refresh token is the credential, so no access
  /// token is sent.
  Future<void> logout(String refreshToken) async {
    await _client.post(
      '/v1/auth/logout',
      auth: false,
      body: <String, dynamic>{'refreshToken': refreshToken},
    );
  }

  String _platformName() {
    switch (defaultTargetPlatform) {
      case TargetPlatform.android:
        return 'android';
      case TargetPlatform.iOS:
        return 'ios';
      default:
        return 'other';
    }
  }
}
