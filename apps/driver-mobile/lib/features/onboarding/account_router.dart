import 'package:flutter/material.dart';

import '../../state/api_providers.dart';
import '../auth/phone_entry_screen.dart';
import '../home/driver_home_screen.dart';
import '../registration/driver_registration_screen.dart';
import '../status/account_status_screen.dart';

/// The screen a driver belongs on, given where their account stands.
Widget screenForAccount(DriverAccountState state) {
  switch (state) {
    case DriverAccountState.ready:
      return const DriverHomeScreen();
    case DriverAccountState.needsRegistration:
      return const DriverRegistrationScreen();
    case DriverAccountState.pending:
    case DriverAccountState.rejected:
    case DriverAccountState.suspended:
      return AccountStatusScreen(state: state);
    case DriverAccountState.signedOut:
      return const PhoneEntryScreen();
  }
}
