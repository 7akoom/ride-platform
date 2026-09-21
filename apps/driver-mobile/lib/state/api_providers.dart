import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api/api_client.dart';
import '../core/api/api_exception.dart';
import '../core/api/auth_api.dart';
import '../core/api/driver_api.dart';
import '../core/api/maps_api.dart';
import '../core/api/money_api.dart';
import '../core/api/trip_api.dart';
import '../core/app_config.dart';
import '../core/models/driver_profile.dart';
import '../core/navigation.dart';
import '../features/auth/phone_entry_screen.dart';
import 'locale_provider.dart';
import 'session_storage.dart';

// These providers refer to each other (a refused session clears the profile), so their
// types are written out: Dart cannot work them out from a cycle.

/// The one connection to the backend.
final Provider<ApiClient> apiClientProvider = Provider<ApiClient>((ref) {
  return ApiClient(
    baseUrl: AppConfig.apiBaseUrl,
    languageCode: () => ref.read(localeProvider).languageCode,
    // The refresh token was refused: the session is over, wherever the driver is in the app.
    onSessionExpired: () async {
      ref.invalidate(driverProfileProvider);
      ref.invalidate(myPhoneProvider);
      await SessionStorage.clear();

      rootNavigatorKey.currentState?.pushAndRemoveUntil(
        MaterialPageRoute(builder: (_) => const PhoneEntryScreen()),
        (route) => false,
      );
    },
  );
});

final Provider<AuthApi> authApiProvider = Provider<AuthApi>((ref) {
  return AuthApi(ref.watch(apiClientProvider));
});

final Provider<DriverApi> driverApiProvider = Provider<DriverApi>((ref) {
  return DriverApi(ref.watch(apiClientProvider));
});

final Provider<TripApi> tripApiProvider = Provider<TripApi>((ref) {
  return TripApi(ref.watch(apiClientProvider));
});

final Provider<MapsApi> mapsApiProvider = Provider<MapsApi>((ref) {
  return MapsApi(ref.watch(apiClientProvider));
});

final Provider<MoneyApi> moneyApiProvider = Provider<MoneyApi>((ref) {
  return MoneyApi(ref.watch(apiClientProvider));
});

/// The signed-in driver's profile (null if there is none yet).
final FutureProvider<DriverProfile?> driverProfileProvider = FutureProvider<DriverProfile?>((ref) async {
  final identityId = await SessionStorage.readIdentityId();
  if (identityId == null) {
    return null;
  }

  return ref.watch(driverApiProvider).getByIdentity(identityId);
});

/// The signed-in driver's verified phone number, in international form.
final FutureProvider<String?> myPhoneProvider = FutureProvider<String?>((ref) async {
  final me = await ref.watch(authApiProvider).getMyIdentity();

  return me.phone;
});

/// Whether the driver may take trips right now (null if the backend cannot say).
final FutureProvider<DriverStanding?> standingProvider = FutureProvider<DriverStanding?>((ref) async {
  final driverId = await SessionStorage.readDriverId();
  if (driverId == null) {
    return null;
  }

  try {
    return await ref.watch(driverApiProvider).standing(driverId);
  } on ApiException {
    return null;
  }
});

/// Where a person stands once they have a session.
enum DriverAccountState {
  /// No session, or the backend refused it.
  signedOut,

  /// Signed in, but has not registered as a driver yet.
  needsRegistration,

  /// Registered; waiting for an operator to approve the account.
  pending,

  /// An operator refused the account.
  rejected,

  /// An operator suspended the account.
  suspended,

  /// Approved: straight to the app.
  ready,
}

final Provider<AccountService> accountServiceProvider = Provider<AccountService>((ref) {
  return AccountService(ref.watch(driverApiProvider));
});

class AccountService {
  AccountService(this._drivers);

  final DriverApi _drivers;

  /// Finds out whether the stored session still works and where the driver's account stands.
  ///
  /// It throws [ApiException] (with isNetwork) when the backend cannot be reached: that
  /// is not a reason to sign anyone out.
  Future<DriverAccountState> resolve() async {
    final identityId = await SessionStorage.readIdentityId();
    final refreshToken = await SessionStorage.readRefreshToken();

    if (identityId == null || refreshToken == null) {
      return DriverAccountState.signedOut;
    }

    try {
      final profile = await _drivers.getByIdentity(
        identityId,
        signOutOnExpiry: false,
      );

      if (profile == null) {
        return DriverAccountState.needsRegistration;
      }

      await SessionStorage.saveDriverId(profile.id);

      switch (profile.status) {
        case DriverStatus.active:
          return DriverAccountState.ready;
        case DriverStatus.rejected:
          return DriverAccountState.rejected;
        case DriverStatus.suspended:
          return DriverAccountState.suspended;
        case DriverStatus.pending:
        case DriverStatus.unknown:
          return DriverAccountState.pending;
      }
    } on ApiException catch (error) {
      // The token and its refresh were both refused: the login is over.
      if (error.isUnauthorized) {
        await SessionStorage.clear();

        return DriverAccountState.signedOut;
      }

      rethrow;
    }
  }
}

/// Signs out: tells the backend (best effort), forgets the session on the phone, and
/// returns to the phone number screen.
Future<void> signOut(WidgetRef ref) async {
  final refreshToken = await SessionStorage.readRefreshToken();

  if (refreshToken != null) {
    try {
      await ref.read(authApiProvider).logout(refreshToken);
    } catch (_) {
      // Signing out must work with no connection too.
    }
  }

  ref.invalidate(driverProfileProvider);
  ref.invalidate(myPhoneProvider);
  ref.invalidate(standingProvider);
  await SessionStorage.clear();

  rootNavigatorKey.currentState?.pushAndRemoveUntil(
    MaterialPageRoute(builder: (_) => const PhoneEntryScreen()),
    (route) => false,
  );
}
