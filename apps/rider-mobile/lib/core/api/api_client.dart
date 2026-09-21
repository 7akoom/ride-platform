import 'dart:async';

import 'package:dio/dio.dart';

import '../../state/session_storage.dart';
import 'api_exception.dart';

typedef JsonMap = Map<String, dynamic>;

enum _RefreshResult { refreshed, rejected, unavailable }

/// Talks to the API gateway as the signed-in rider.
///
/// Every call carries the access token. When the backend answers 401 (the token ran out:
/// it lives 15 minutes), the client trades the refresh token for a new pair once and
/// repeats the call, so the rest of the app never has to think about it. If the refresh
/// token is refused too, the session is over: [onSessionExpired] is called and the caller
/// gets the 401.
class ApiClient {
  ApiClient({
    required String baseUrl,
    required this.languageCode,
    this.onSessionExpired,
  })  : _dio = Dio(_options(baseUrl)),
        _refreshDio = Dio(_options(baseUrl));

  final Dio _dio;

  /// Used only to renew the tokens, so it can never end up waiting on itself.
  final Dio _refreshDio;

  /// The language the person picked ('ar' or 'en'): the backend writes its messages
  /// (for example the login code) in it.
  final String Function() languageCode;

  final Future<void> Function()? onSessionExpired;

  Future<_RefreshResult>? _refreshing;

  static BaseOptions _options(String baseUrl) => BaseOptions(
        baseUrl: baseUrl,
        connectTimeout: const Duration(seconds: 10),
        sendTimeout: const Duration(seconds: 15),
        receiveTimeout: const Duration(seconds: 20),
        contentType: Headers.jsonContentType,
      );

  /// [auth] false is for the login routes, which take no token.
  ///
  /// [signOutOnExpiry] false makes a refused refresh token throw instead of ending the
  /// session, for the check made when the app opens (it decides where to go itself).
  Future<JsonMap> get(
    String path, {
    Map<String, dynamic>? query,
    bool auth = true,
    bool signOutOnExpiry = true,
  }) =>
      _send('GET', path,
          query: query, auth: auth, signOutOnExpiry: signOutOnExpiry);

  Future<JsonMap> post(String path, {Object? body, bool auth = true}) => _send(
        'POST',
        path,
        body: body ?? const <String, dynamic>{},
        auth: auth,
      );

  Future<JsonMap> patch(String path, {Object? body}) =>
      _send('PATCH', path, body: body ?? const <String, dynamic>{});

  Future<JsonMap> put(String path, {Object? body}) =>
      _send('PUT', path, body: body ?? const <String, dynamic>{});

  Future<JsonMap> delete(String path) => _send('DELETE', path);

  Future<JsonMap> _send(
    String method,
    String path, {
    Map<String, dynamic>? query,
    Object? body,
    bool auth = true,
    bool signOutOnExpiry = true,
  }) async {
    var alreadyRefreshed = false;

    while (true) {
      final headers = <String, dynamic>{'Accept-Language': languageCode()};

      if (auth) {
        final token = await SessionStorage.readToken();
        if (token != null) {
          headers['Authorization'] = 'Bearer $token';
        }
      }

      try {
        final response = await _dio.request<dynamic>(
          path,
          data: body,
          queryParameters: query,
          options: Options(method: method, headers: headers),
        );

        return _decode(response.data);
      } on DioException catch (error) {
        final failure = _toApiException(error);

        if (!auth || !failure.isUnauthorized || alreadyRefreshed) {
          throw failure;
        }

        alreadyRefreshed = true;

        switch (await _refreshSession()) {
          case _RefreshResult.refreshed:
            continue;

          case _RefreshResult.rejected:
            if (signOutOnExpiry) {
              await onSessionExpired?.call();
            }
            throw failure;

          case _RefreshResult.unavailable:
            // The token may still be fine: there was just no way to find out. Do not
            // sign anyone out because their connection dropped.
            throw const ApiException(
              message: 'the session could not be renewed right now',
              isNetwork: true,
            );
        }
      }
    }
  }

  /// Several calls can find the token expired at the same moment; they all wait for one
  /// refresh instead of each spending the (single-use) refresh token.
  Future<_RefreshResult> _refreshSession() {
    final running = _refreshing;
    if (running != null) {
      return running;
    }

    final attempt = _renewTokens().whenComplete(() => _refreshing = null);
    _refreshing = attempt;

    return attempt;
  }

  Future<_RefreshResult> _renewTokens() async {
    final refreshToken = await SessionStorage.readRefreshToken();
    if (refreshToken == null) {
      return _RefreshResult.rejected;
    }

    try {
      final response = await _refreshDio.post<dynamic>(
        '/v1/auth/token:refresh',
        data: <String, dynamic>{'refreshToken': refreshToken},
        options: Options(headers: <String, dynamic>{'Accept-Language': languageCode()}),
      );

      final json = _decode(response.data);
      final access = json['accessToken'];
      final refresh = json['refreshToken'];

      if (access is! String || refresh is! String) {
        return _RefreshResult.rejected;
      }

      final identity = json['identityId'];

      await SessionStorage.saveTokens(
        accessToken: access,
        refreshToken: refresh,
        identityId: identity is String && identity.isNotEmpty
            ? identity
            : (await SessionStorage.readIdentityId()) ?? '',
      );

      return _RefreshResult.refreshed;
    } on DioException catch (error) {
      final failure = _toApiException(error);

      if (failure.isNetwork || failure.isServerError) {
        return _RefreshResult.unavailable;
      }

      return _RefreshResult.rejected;
    }
  }

  JsonMap _decode(dynamic data) {
    if (data is Map<String, dynamic>) {
      return data;
    }

    if (data is Map) {
      return Map<String, dynamic>.from(data);
    }

    return <String, dynamic>{};
  }

  ApiException _toApiException(DioException error) {
    final response = error.response;

    if (response == null) {
      return ApiException(
        message: error.message ?? 'no answer from the server',
        isNetwork: true,
      );
    }

    var message = '';
    int? code;
    final data = response.data;

    if (data is Map) {
      final rawMessage = data['message'];
      if (rawMessage is String) {
        message = rawMessage;
      }

      final rawCode = data['code'];
      if (rawCode is num) {
        code = rawCode.toInt();
      }
    }

    return ApiException(
      statusCode: response.statusCode,
      code: code,
      message: message.isEmpty ? 'HTTP ${response.statusCode}' : message,
    );
  }
}
