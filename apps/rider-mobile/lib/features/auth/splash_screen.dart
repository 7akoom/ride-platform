import 'package:flutter/material.dart';

import '../../state/session_storage.dart';
import '../../theme/app_theme.dart';
import '../rider/request_ride_screen.dart';
import 'phone_entry_screen.dart';

class SplashScreen extends StatefulWidget {
  const SplashScreen({super.key});

  @override
  State<SplashScreen> createState() => _SplashScreenState();
}

class _SplashScreenState extends State<SplashScreen> {
  @override
  void initState() {
    super.initState();
    _checkSession();
  }

  Future<void> _checkSession() async {
    final token = await SessionStorage.readToken();
    if (!mounted) return;

    // TODO(robert): a stored token existing doesn't mean it's still valid —
    // once Identity's verify-token/refresh RPC is reachable through the
    // Gateway, check it here before deciding to skip login, and fall back
    // to PhoneEntryScreen on any failure instead of trusting local storage.
    Navigator.of(context).pushReplacement(
      MaterialPageRoute(
        builder: (_) =>
            token != null ? const RequestRideScreen() : const PhoneEntryScreen(),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Scaffold(
      backgroundColor: colors.brand500,
      body: const Center(
        child: Icon(
          Icons.directions_car_filled,
          color: Colors.white,
          size: 56,
        ),
      ),
    );
  }
}
